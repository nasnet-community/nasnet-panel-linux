// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"fmt"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// InstallOptions are what an install is asked for beside the server and the
// login.
type InstallOptions struct {
	// TakeOverXray lets the install stop and remove an Xray that runs on the
	// server, which it otherwise refuses (ErrServerHasXray). It is for a person
	// who has said so: nothing of that Xray is kept, and nothing puts it back.
	TakeOverXray bool
}

// foreignXray is the Xray a server runs of its own, as the check found it.
type foreignXray struct {
	ServerHasXrayError // what was found, and which build the binary is

	// what a takeover acts on, read only when one is asked for
	unitFile  string   // where systemd has the unit; empty when there is none
	dropIns   []string // names in the drop-in folder
	confFiles []string // names in Xray's config folder
	active    bool     // the service runs now, which is what a refused takeover puts back
}

// lookForXray looks for an Xray the server runs of its own: the binary at the
// standard path, a unit named xray.service, a process named xray. It gives nil
// for a server with none. For one with a binary it reads the version and the
// checksum, which say whether it is the pinned build. It changes nothing, and it
// reads as root, as everything about a server's own Xray does: its folders may be
// closed to a login that isn't root, and /proc may be mounted so that a login
// sees only its own processes.
func (in *install) lookForXray(ctx context.Context) (*foreignXray, error) {
	out, err := in.s.root(ctx, "Couldn't look for Xray on the server", cmdXraySigns, nil)
	if err != nil {
		return nil, err
	}
	unit, process := xraySigns(out)
	fx := &foreignXray{ServerHasXrayError: ServerHasXrayError{Binary: in.state.present[xrayBin], Unit: unit, Process: process}}
	if !fx.Binary && !fx.Unit && !fx.Process {
		return nil, nil
	}
	if fx.Binary {
		// as root: the file may not be readable by the login, and the unit runs as root
		if out, err = in.s.root(ctx, "Couldn't read the Xray on the server", cmdXrayVersion, nil); err != nil {
			return nil, err
		}
		fx.Version = ParseXrayVersion(out)
		if out, err = in.s.root(ctx, "Couldn't read the Xray on the server", cmdXrayHash, nil); err != nil {
			return nil, err
		}
		// the version says which release it is, and the checksum which build: a patched
		// one keeps the number of the release it was made from
		want, sha := in.e.pinnedXray(in.res.Arch)
		fx.SameBuild = fx.Version == want && sha != "" && sha256Of(out) == sha
	}
	return fx, nil
}

// readTakeover reads what a takeover is about to remove, so that it is known
// before anything is touched.
func (in *install) readTakeover(ctx context.Context, fx *foreignXray) error {
	read := func(cmd string) (string, error) {
		return in.s.root(ctx, "Couldn't read the Xray on the server", cmd, nil)
	}
	if fx.Unit {
		out, err := read(cmdXrayUnitFile)
		if err != nil {
			return err
		}
		fx.unitFile = unitFileOf(out)
		if out, err = read(cmdXrayActive); err != nil {
			return err
		}
		fx.active = strings.TrimSpace(out) == "active"
	}
	out, err := read(cmdXrayDropIns)
	if err != nil {
		return err
	}
	fx.dropIns = namesIn(out)
	if out, err = read(cmdXrayConfFiles); err != nil {
		return err
	}
	fx.confFiles = namesIn(out)
	return nil
}

// takeOver is the step that takes over the server's Xray, for an install that
// was asked to. A server with nothing of the kind has nothing to take, which
// is no failure.
//
// It stops xray.service and then looks at the processes: one that still runs
// (in a container, by hand, from another unit) ends it there, with the service
// started again if it ran, since nothing else has been touched. Only when no
// Xray runs does it write the marker, so that a failure after this point can be
// cleaned away, and remove the drop-ins, the unit and the config; the binary
// goes too unless it is the pinned build, which the install then uses as it is.
func (in *install) takeOver(ctx context.Context) error {
	if !in.opts.TakeOverXray {
		return nil
	}
	if in.foreign == nil {
		in.st.skip(StepTakeOverXray, "There is no Xray of the server's own")
		return nil
	}
	done := in.st.begin(StepTakeOverXray)
	detail, err := in.removeForeign(ctx)
	return done(detail, err)
}

func (in *install) removeForeign(ctx context.Context) (string, error) {
	s, fx := in.s, in.foreign
	if fx.Unit {
		if _, err := s.root(ctx, "Couldn't stop Xray's service", cmdTakeStop, nil); err != nil {
			return "", err
		}
	}
	out, err := s.root(ctx, "Couldn't look for Xray processes", cmdXrayRunning, nil)
	if err != nil {
		return "", err
	}
	if _, still := xraySigns(out); still {
		return "", in.refuseTakeover(ctx)
	}

	// from here on the server's own Xray is gone, whatever happens next
	in.res.Changed, in.res.TookOver = true, true
	if _, err := s.root(ctx, "Couldn't create "+dataDir, cmdDirs(dataDir), nil); err != nil {
		return "", err
	}
	if err := in.writeMarker(ctx); err != nil {
		return "", err
	}
	steps := []struct{ what, cmd string }{
		{"Couldn't switch Xray's service off", cmdTakeDisable},
		{"Couldn't remove what changes Xray's service", cmdTakeRmDropIns},
		{"Couldn't remove Xray's service", cmdTakeRmUnit},
		{"Couldn't remove Xray's config", cmdTakeRmConfig},
	}
	if !fx.SameBuild {
		steps = append(steps, struct{ what, cmd string }{"Couldn't remove Xray", cmdTakeRmBinary})
	}
	steps = append(steps, struct{ what, cmd string }{"Couldn't reload systemd", cmdReload})
	for _, step := range steps {
		if _, err := s.root(ctx, step.what, step.cmd, nil); err != nil {
			return "", err
		}
	}
	in.res.XrayKept = fx.SameBuild
	return fx.removed(), nil
}

// refuseTakeover is the end of a takeover that found an Xray still running
// after its service was stopped. A service that ran is started again, so that
// nothing has changed; if that fails the server is changed after all, and the
// error says so.
func (in *install) refuseTakeover(ctx context.Context) error {
	msg := "An Xray is still running on this server after its service was stopped, for example in a container. Stop it there, or use another server."
	if fx := in.foreign; fx.Unit && fx.active {
		if _, err := in.s.root(ctx, "", cmdTakeStart, nil); err != nil {
			in.res.Changed = true // the service stays stopped
			msg += " Its service was stopped and could not be started again."
		} else {
			msg += " Its service was started again."
		}
	}
	return newErr(ErrXrayNotStopped, nil, "%s", msg)
}

// removed is what the takeover step says once it is done, in a line (a step's
// text is cut at 200 characters): which Xray it was, what went, and whether the
// binary stayed. A patched build that has the pinned version number is the same
// by name only, and is said to differ by its checksum.
func (fx *foreignXray) removed() string {
	was := "The Xray"
	if fx.Version != "" {
		was = "Xray " + fx.Version
	}
	var gone []string
	if fx.Unit {
		if fx.unitIsPackaged() {
			gone = append(gone, "its service (a package's, which this plugin's unit overrides)")
		} else {
			gone = append(gone, "its service")
		}
	}
	if n := len(fx.dropIns); n > 0 {
		gone = append(gone, plural(n, "drop-in"))
	}
	if n := len(fx.confFiles); n > 0 {
		gone = append(gone, plural(n, "config file"))
	}
	if !fx.SameBuild && fx.Binary {
		gone = append(gone, "its binary")
	}
	said := was + " was stopped"
	if len(gone) > 0 {
		said += ", and " + strings.Join(gone, ", ") + " removed"
	}
	switch {
	case fx.SameBuild:
		return said + ". It is the pinned build, so its binary stays."
	case fx.Binary && fx.Version == pin.Version:
		return said + ". It has the pinned version but not the pinned build (its checksum differs), so it is replaced."
	}
	return said + ". Xray " + pin.Version + " goes on later."
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// unitIsPackaged says the unit lives in a package's folder, which a takeover
// leaves alone: the unit it writes to /etc/systemd/system comes first.
func (fx *foreignXray) unitIsPackaged() bool {
	return fx.unitFile != "" && !strings.HasPrefix(fx.unitFile, "/etc/")
}

// planned is what the check step says of an Xray it is going to take over.
func (fx *foreignXray) planned() string {
	switch {
	case fx.SameBuild:
		return "takes over Xray " + fx.Version + " (the pinned build)"
	case fx.Version != "":
		return "takes over Xray " + fx.Version
	}
	return "takes over an Xray"
}
