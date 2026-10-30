// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// restartWait is how long an updated agent has to come back: it waits two
// seconds before it restarts itself, and the restart takes a few more.
const restartWait = 2 * time.Minute

// UpdateAgent puts the release's agent on a node that has one, through the
// agent itself: the gzipped bytes with the plain agent's SHA-256, its version
// and its signature, which the agent checks after unpacking, and a restart.
// It never forces: an agent that would be going backwards refuses, and the
// refusal is ErrAgentRefused with the agent's reason.
//
// The old link goes down with the restart, so the hub then reaches the agent
// again on a new connection, waits for the version that was sent, and shows
// again that the new agent runs the managed profile. The client it was given
// is not touched, and reconnects by itself. A node already on the release
// is not sent anything, and the steps say so.
//
// The upload is one message, so it has no progress of its own to report.
func (e *Env) UpdateAgent(ctx context.Context, c *agent.Client, node model.Node, progress Progress) (_ Versions, err error) {
	defer func() { err = ctxErr(ctx, err) }()
	st := newStepper(progress)
	if e.Files == nil {
		return Versions{}, ErrNoFiles
	}
	if e.CA == nil {
		return Versions{}, ErrNoCA // the new agent is met on a link of its own
	}
	done := st.begin(StepDownloadAgent)
	info, err := c.GetVersion(ctx)
	if err != nil {
		return Versions{}, done("", callFailure(err, node))
	}
	arch := info.Arch
	if arch == "" {
		arch = node.Arch
	}
	ag, err := e.Files.Agent(ctx, arch)
	if err != nil {
		return Versions{}, done("", plainErr(err))
	}
	if sameVersion(info.AgentVersion, ag.Version) {
		done("The agent is already "+ag.Version, nil)
		for _, name := range []string{StepUploadAgent, StepRestartAgent, StepCheckAgent} {
			st.skip(name, "")
		}
		return Versions{Agent: info.AgentVersion, Xray: ParseXrayVersion(info.XrayVersion)}, nil
	}
	done(fmt.Sprintf("%s · %s", ag.Version, byteSize(int64(len(ag.Gz)))), nil)

	done = st.begin(StepUploadAgent)
	res, err := c.SelfUpdate(ctx, ag.Gz, ag.PlainSHA256, ag.Version, true, ag.Sig, false)
	if err != nil {
		return Versions{}, done("", updateFailure(err, c))
	}
	if !res.Success {
		return Versions{}, done("", newErr(ErrAgentRefused, nil, "The agent refused the update: %s", sentence(res.Message)))
	}
	done(byteSize(int64(len(ag.Gz)))+" sent", nil)

	done = st.begin(StepRestartAgent)
	nc, err := e.waitForAgent(ctx, node, ag.Version)
	if err != nil {
		return Versions{}, done("", err)
	}
	defer nc.Close()
	done("Back on "+ag.Version, nil)

	done = st.begin(StepCheckAgent)
	v, err := e.versions(ctx, nc, node)
	if err != nil {
		return Versions{}, done("", err)
	}
	if err := proveManaged(ctx, nc, v.Agent); err != nil {
		var nm *NotManagedError
		if errors.As(err, &nm) {
			return Versions{}, done("", err)
		}
		return Versions{}, done("", callFailure(err, node))
	}
	done(v.Agent+", managed profile", nil)
	return v, nil
}

// updateFailure says why SelfUpdate didn't go through.
func updateFailure(err error, c *agent.Client) error {
	if ctxDone(err) {
		return err
	}
	if down := linkDown(err, c); down != nil {
		return down
	}
	return &opError{msg: sentence("The agent couldn't take the update: " + trimPrefixes(statusMessage(err), "failed to call SelfUpdate: ")), cause: err}
}

// waitForAgent connects again until the agent that answers is the version that
// was sent. For the first seconds the old agent still does, then nothing
// does, then the new one. It gives back the connection it found it on.
func (e *Env) waitForAgent(ctx context.Context, node model.Node, want string) (*agent.Client, error) {
	var found *agent.Client
	var got string
	err := poll(ctx, e.settleOr(restartWait), 1500*time.Millisecond, func(ctx context.Context) (bool, error) {
		c, err := e.dialAgent(ctx, node, min(e.dialTimeout(), 5*time.Second), nil)
		if err != nil {
			if errors.Is(err, ErrAgentCert) {
				return false, stop(err)
			}
			return false, err
		}
		info, err := c.GetVersion(ctx)
		if err != nil {
			c.Close()
			return false, callFailure(err, node)
		}
		if !sameVersion(info.AgentVersion, want) {
			c.Close()
			got = info.AgentVersion
			return false, &VersionMismatchError{What: "agent", Want: want, Got: got}
		}
		found = c
		return true, nil
	})
	if err == nil {
		return found, nil
	}
	if ctxDone(err) {
		return nil, err
	}
	var u *UnreachableError
	if errors.As(err, &u) {
		return nil, &UnreachableError{What: "restart", Host: u.Host, Port: u.Port, Err: u.Err}
	}
	return nil, err
}

// settleOr is d, or what the tests give every wait to shorten them.
func (e *Env) settleOr(d time.Duration) time.Duration { return e.settleTime(d) }
