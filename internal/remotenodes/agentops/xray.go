// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

const (
	xrayVersionWait = 30 * time.Second // a push restarts Xray, which takes a few seconds
	xrayRunningWait = 20 * time.Second
)

// PushXray puts the Xray build the plugin pins on a node. The agent unpacks
// it, checks its hash and that it is built for the node's CPU, and swaps it
// in, rolling back if the new build doesn't run; the hub then waits until the
// node reports the pinned version. A node already on it gets nothing, and the
// steps say so. The architecture is the one the agent reports for itself.
//
// restart asks the agent to start Xray afterwards even if it was stopped. A
// new node has no config for it to start with yet, so it is false there; an
// update is true.
//
// An agent that predates the call answers Unimplemented, which is
// ErrAgentOutdated: update the agent first.
func (e *Env) PushXray(ctx context.Context, c *agent.Client, node model.Node, restart bool, progress Progress) (err error) {
	defer func() { err = ctxErr(ctx, err) }()
	return e.pushXray(ctx, c, node, restart, false, newStepper(progress))
}

// pushXray is PushXray's work. kept says the build on the node is known to be
// the pinned one (checked by its checksum over SSH, which the agent's version
// line can't show of a patched build), so it is neither fetched nor sent, and
// the node is only asked what runs.
func (e *Env) pushXray(ctx context.Context, c *agent.Client, node model.Node, restart, kept bool, st *stepper) error {
	if kept {
		st.skip(StepDownloadXray, "")
		st.skip(StepUploadXray, "")
		done := st.begin(StepCheckXray)
		detail, err := e.waitXray(ctx, c, node, pin.Version, restart)
		return done(detail, err)
	}
	done := st.begin(StepDownloadXray)
	info, err := c.GetVersion(ctx)
	if err != nil {
		return done("", callFailure(err, node))
	}
	if ParseXrayVersion(info.XrayVersion) == pin.Version {
		done("Xray "+pin.Version+" is already on the server", nil)
		st.skip(StepUploadXray, "")
		st.skip(StepCheckXray, "")
		return nil
	}
	arch := info.Arch
	if arch == "" {
		arch = node.Arch
	}
	if e.Files == nil {
		return done("", ErrNoFiles)
	}
	x, err := e.Files.Xray(ctx, arch)
	if err != nil {
		return done("", plainErr(err))
	}
	done(fmt.Sprintf("%s · %s", x.Version, byteSize(int64(len(x.Gz)))), nil)

	done = st.begin(StepUploadXray)
	m := newMeter(st, StepUploadXray, int64(len(x.Gz)), e.now)
	if err := c.PushXrayBinary(ctx, x.Version, x.PlainSHA256, m.reader(bytes.NewReader(x.Gz)), restart, true); err != nil {
		return done("", pushFailure(err))
	}
	done(m.summary()+" sent", nil)

	done = st.begin(StepCheckXray)
	detail, err := e.waitXray(ctx, c, node, x.Version, restart)
	return done(detail, err)
}

// waitXray waits for the node to report the version that was sent, and, when
// Xray was meant to be started, to run it.
func (e *Env) waitXray(ctx context.Context, c *agent.Client, node model.Node, want string, running bool) (string, error) {
	err := poll(ctx, e.settleTime(xrayVersionWait), 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		info, err := c.GetVersion(ctx)
		if err != nil {
			return false, callFailure(err, node)
		}
		if got := ParseXrayVersion(info.XrayVersion); got != want {
			return false, &VersionMismatchError{What: "Xray", Want: want, Got: got}
		}
		if running {
			st, err := c.GetStatus(ctx)
			if err != nil {
				return false, callFailure(err, node)
			}
			if !st.XrayRunning {
				return false, newErr(ErrXrayNotRunning, nil, "Xray is on the node, but it isn't running.")
			}
		}
		return true, nil
	})
	if err != nil {
		return "", err
	}
	return "Xray " + want, nil
}

// pushFailure says why an Xray push didn't take, in the agent's words where it has some.
func pushFailure(err error) error {
	switch {
	case ctxDone(err):
		return err
	case agent.IsUnimplemented(err):
		return newErr(ErrAgentOutdated, err, "This node's agent is too old to take an Xray build. Update the agent first.")
	}
	var se interface{ GRPCStatus() *status.Status }
	if errors.As(err, &se) {
		switch se.GRPCStatus().Code() {
		case codes.Unavailable, codes.DeadlineExceeded:
			return newErr(ErrUnreachable, err, "The connection to the node broke while the Xray build was going over.")
		}
		return &opError{msg: sentence("The node refused the Xray build: " + statusMessage(err)), cause: err}
	}
	// the agent took the push and couldn't finish it, and rolled back
	return &opError{msg: sentence("The node didn't take the Xray build: " + trimPrefixes(err.Error(), "xray push failed: ")), cause: err}
}

// UpdateXray puts the pinned Xray on a node that already runs one and
// verifies it, restarting Xray as it goes. The manager restarts the bridge
// itself, since both ends of the tunnel have to run the same build.
func (e *Env) UpdateXray(ctx context.Context, c *agent.Client, node model.Node, progress Progress) (_ Versions, err error) {
	defer func() { err = ctxErr(ctx, err) }()
	if err := e.pushXray(ctx, c, node, true, false, newStepper(progress)); err != nil {
		return Versions{}, err
	}
	return e.versions(ctx, c, node)
}

// versions is what a node reports it runs.
func (e *Env) versions(ctx context.Context, c *agent.Client, node model.Node) (Versions, error) {
	info, err := c.GetVersion(ctx)
	if err != nil {
		return Versions{}, callFailure(err, node)
	}
	return Versions{Agent: info.AgentVersion, Xray: ParseXrayVersion(info.XrayVersion)}, nil
}
