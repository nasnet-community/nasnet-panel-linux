// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

const (
	entryWait = 15 * time.Second
	// how much of what Xray says about a config it won't take goes in the message
	maxRejectedText = 700
)

// ConfigRejectedError is a config the node's Xray refused to load. Message is
// what its own check printed.
type ConfigRejectedError struct{ Message string }

func (e *ConfigRejectedError) Error() string {
	return sentence("The node's Xray refused the configuration: " + e.Message)
}
func (e *ConfigRejectedError) Is(target error) bool { return target == ErrConfigRejected }

// PushConfig builds a node's Xray config from in, has the node check it with
// Xray itself, pushes it, and makes sure it took: Xray is running, and the
// entry port answers from the bound address, which is where the bridge will
// dial from. A config the node refuses is ErrConfigRejected with Xray's words
// in it, and the config on the node stays as it was. An input that can't make
// a config is nodecfg.ErrInvalid, and the node is not called.
//
// A port that doesn't answer, with Xray running and the entry in it, is the
// server's firewall, or the provider's, and is ErrUnreachable and says to
// open TCP <port>.
func (e *Env) PushConfig(ctx context.Context, c *agent.Client, in nodecfg.Input) (err error) {
	defer func() { err = ctxErr(ctx, err) }()
	cfg, err := nodecfg.Build(in)
	if err != nil {
		return err
	}
	ok, problems, _, err := c.ValidateConfig(ctx, string(cfg))
	if err != nil {
		return configFailure(err, c)
	}
	if !ok {
		return &ConfigRejectedError{Message: xrayWords(strings.Join(problems, "\n"))}
	}
	if err := c.PushConfig(ctx, string(cfg)); err != nil {
		return configFailure(err, c)
	}
	if err := e.ensureRunning(ctx, c); err != nil {
		return err
	}
	return e.checkEntry(ctx, c, in.EntryPort)
}

// configFailure says why a config call failed: the node's Xray refusing the
// config, or not starting with it, in the agent's words.
func configFailure(err error, c *agent.Client) error {
	if ctxDone(err) {
		return err
	}
	if down := linkDown(err, c); down != nil {
		return down
	}
	if agent.IsUnimplemented(err) {
		return newErr(ErrAgentOutdated, err, "This node's agent is too old for this. Update the agent first.")
	}
	msg := statusMessage(err)
	switch {
	case strings.Contains(msg, "config validation failed"), strings.Contains(msg, "could not run"):
		_, after, _ := strings.Cut(msg, "config validation failed: ")
		if after == "" {
			after = msg
		}
		return &ConfigRejectedError{Message: xrayWords(after)}
	case strings.Contains(msg, "restart failed"):
		return newErr(ErrXrayNotRunning, err, "Xray didn't start with the new configuration: %s", xrayWords(msg))
	}
	return &opError{msg: sentence("The node couldn't take the configuration: " + trimPrefixes(msg, "failed to push config: ")), cause: err}
}

// xrayWords is what Xray printed, cut down to its last lines: the banner comes
// first and the reason last.
func xrayWords(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxRejectedText {
		s = tail(s, maxRejectedText)
	}
	return s
}

// ensureRunning makes sure Xray is up after a push: the agent restarts it as
// part of one, so normally it is, and when it isn't it is started, and the
// agent's answer, which carries Xray's last lines, says why it can't.
func (e *Env) ensureRunning(ctx context.Context, c *agent.Client) error {
	running := func(ctx context.Context) (bool, error) {
		st, err := c.GetStatus(ctx)
		if err != nil {
			return false, err
		}
		return st.XrayRunning, nil
	}
	if ok, err := running(ctx); err != nil {
		return configFailure(err, c)
	} else if ok {
		return nil
	}
	if err := c.StartXray(ctx); err != nil {
		return newErr(ErrXrayNotRunning, err, "Xray didn't start on the node: %s", xrayWords(trimPrefixes(statusMessage(err), "failed to start xray: ")))
	}
	if err := poll(ctx, e.settleTime(xrayRunningWait), 500*time.Millisecond, running); err != nil {
		if ctxDone(err) {
			return err
		}
		return newErr(ErrXrayNotRunning, err, "Xray is on the node, but it isn't running.")
	}
	return nil
}

// checkEntry connects to the entry port from the bound address. The node's
// address is the one the link to its agent is made to.
//
// If nothing answers, the agent is asked whether Xray knows the entry: if it
// does, the port is open on the node and shut on the way, and that is said as
// such; if it doesn't, Xray is what's wrong.
func (e *Env) checkEntry(ctx context.Context, c *agent.Client, port int) error {
	host, _, err := net.SplitHostPort(c.Target())
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	d := e.dialer()
	err = poll(ctx, e.settleTime(entryWait), 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		dctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		conn, err := d.DialContext(dctx, "tcp", addr)
		if err != nil {
			return false, err
		}
		conn.Close()
		return true, nil
	})
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, uerr := c.ListUsers(ctx, nodecfg.EntryTag); uerr != nil {
		return newErr(ErrXrayNotRunning, errors.Join(err, uerr), "Xray doesn't know the entry on port %d: it isn't answering on the node.", port)
	}
	return &UnreachableError{What: "entry", Host: host, Port: port, Err: err}
}
