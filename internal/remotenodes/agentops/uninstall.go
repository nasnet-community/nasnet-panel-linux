// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// Uninstall takes the agent off a server over SSH and puts it back as it
// found it: the unit, the config and certificates, the data and log folders,
// the binary, the firewall rules the install opened, the kernel setting the
// install made (its file always, and the live value if it is still the
// install's), and Xray's binary, service and config only where the marker says
// the install put them there.
// It never touches a server's own Xray, and it leaves a server's other agent
// alone: an agent that trusts another hub's CA is ErrOtherHub.
//
// It takes whatever an install got as far as, and finds nothing wrong with a
// server that has less on it than an install leaves, or nothing at all: the
// marker is written before anything else and removed last, so a removal that
// stopped half way is finished by the next one. The login is used for this
// call and not kept.
func (e *Env) Uninstall(ctx context.Context, node model.Node, login Login, progress Progress) error {
	if e.CA == nil {
		return ErrNoCA
	}
	st := newStepper(progress)
	u := &uninstall{e: e, node: node, st: st}
	err := u.run(ctx, login)
	err = scrub(err, login.secrets())
	if err != nil {
		e.log().Warn("removing the agent failed", "node", node.ID, "host", node.Host, "error", err)
	} else {
		e.log().Info("removed the agent", "node", node.ID, "host", node.Host)
	}
	return err
}

type uninstall struct {
	e    *Env
	node model.Node
	st   *stepper
	s    *session

	// what the marker says the install added
	mk       marker
	xrayLeft bool // a foreign Xray stays
}

func (u *uninstall) run(ctx context.Context, login Login) error {
	var err error
	if u.s, err = u.e.openSession(ctx, u.node, login); err != nil {
		u.st.begin(StepCheckServer)("", err)
		return err
	}
	defer u.s.close()

	found, err := u.check(ctx)
	if err != nil {
		return err
	}
	if !found {
		return u.tidy(ctx)
	}
	if err := u.stop(ctx); err != nil {
		return err
	}
	return u.remove(ctx)
}

// tidy is all there is to do for a server with nothing of the install on it,
// but for the empty folders an install that stopped at its first step leaves.
func (u *uninstall) tidy(ctx context.Context) error {
	u.st.skip(StepStopAgent, "")
	done := u.st.begin(StepRemoveAgent)
	_, err := u.s.root(ctx, "Couldn't tidy the server", cmdRmEmpty, nil)
	return done("Nothing of the agent was left", err)
}

// check looks at the server and decides whose it is. It reports false when
// there is nothing of the install on it.
func (u *uninstall) check(ctx context.Context) (bool, error) {
	done := u.st.begin(StepCheckServer)
	found, detail, err := u.inspect(ctx)
	return found, done(detail, err)
}

func (u *uninstall) inspect(ctx context.Context) (bool, string, error) {
	state, err := u.s.readState(ctx)
	if err != nil {
		return false, "", err
	}
	mine, other := state.ownership(u.e.CA.Certificate())
	hasAgent := state.present[agentBin] || state.present[agentUnitPath]
	switch {
	case other:
		return false, "", newErr(ErrOtherHub, nil, "This server's agent trusts another hub, so it isn't taken off from here. Remove it from the hub that installed it, or by hand.")
	case !mine && hasAgent:
		return false, "", newErr(ErrOtherHub, nil, "There is an agent on this server, and nothing on it says it was installed by this hub, so it is left alone.")
	case !mine:
		return false, "Nothing of the agent is on this server", nil
	}
	if state.marker != nil {
		u.mk = *state.marker
	} else {
		// the CA is ours but the marker is gone: the agent goes, and Xray, which the
		// install may or may not have put there, stays
		u.mk = marker{Agent: true}
	}
	u.xrayLeft = (state.present[xrayBin] && !u.mk.XrayBinary) || (state.present[xrayUnitPath] && !u.mk.XrayUnit) ||
		(state.present[xrayConfPath] && !u.mk.XrayConfig)
	return true, "", nil
}

// stop is step two: the agent, and the service Xray runs in when the
// install made it. A server's own Xray keeps running.
func (u *uninstall) stop(ctx context.Context) error {
	done := u.st.begin(StepStopAgent)
	if _, err := u.s.root(ctx, "Couldn't stop the agent", cmdStopAgent, nil); err != nil {
		return done("", err)
	}
	if u.mk.XrayUnit {
		if _, err := u.s.root(ctx, "Couldn't stop Xray", cmdStopXray, nil); err != nil {
			return done("", err)
		}
	}
	return done("", nil)
}

// remove is step three. The marker goes last, with the data folder, so that a
// removal cut short still knows what it was doing.
func (u *uninstall) remove(ctx context.Context) error {
	done := u.st.begin(StepRemoveAgent)
	return done(u.removeAll(ctx))
}

func (u *uninstall) removeAll(ctx context.Context) (string, error) {
	s := u.s
	// what the kernel setting was before the install, and the kernel's default
	// for an install that never read it
	was := retries2Value(u.mk.TCPRetries2Was)
	if was == "" {
		was = retries2Default
	}
	steps := []struct{ what, cmd string }{
		{"Couldn't remove the agent", cmdRmAgent},
		{"Couldn't remove the agent's folders", cmdRmDirs},
		{"Couldn't remove the agent's kernel setting", cmdRmSysctl},
		{"Couldn't put net.ipv4.tcp_retries2 back", cmdRestoreRetries2(was)},
	}
	if u.mk.XrayBinary {
		steps = append(steps, struct{ what, cmd string }{"Couldn't remove Xray", cmdRmXray})
	}
	if u.mk.XrayConfig {
		steps = append(steps, struct{ what, cmd string }{"Couldn't remove Xray's config", cmdRmXrayCfg})
	}
	if u.mk.XrayUnit {
		steps = append(steps, struct{ what, cmd string }{"Couldn't remove Xray's service", cmdRmXrayDef})
	}
	// the agent made this one itself, whoever's Xray it points at
	steps = append(steps, struct{ what, cmd string }{"Couldn't remove what the agent wrote for Xray's service", cmdRmDropIn})
	for _, step := range steps {
		if _, err := s.root(ctx, step.what, step.cmd, nil); err != nil {
			return "", err
		}
	}
	if len(u.mk.Firewall) > 0 && s.has(ctx, cmdUFW) {
		for _, p := range u.mk.Firewall {
			if _, err := s.root(ctx, fmt.Sprintf("Couldn't close port %d in the firewall", p), cmdUFWDelete(p), nil); err != nil {
				return "", err
			}
		}
	}
	for _, step := range []struct{ what, cmd string }{
		{"Couldn't reload systemd", cmdReload},
		{"Couldn't remove the agent's data", cmdRmData},
	} {
		if _, err := s.root(ctx, step.what, step.cmd, nil); err != nil {
			return "", err
		}
	}
	if u.xrayLeft {
		return "Xray was on the server before, and is left as it is", nil
	}
	return "", nil
}
