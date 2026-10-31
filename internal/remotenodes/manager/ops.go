// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// The node operations are what a person asks of the nodes: add one, change
// its settings, update it, renew its certificate, rotate its keys, take it
// out. Each is a method of Manager, in a file of its own (install.go,
// settings.go, updates.go, certs.go, remove.go). The ones that take more than
// a moment are jobs of the job engine, so that the page can follow their
// steps, close the dialog and come back.
//
// What reaches a node goes through Ops, the few calls of agentops that the
// operations make. In the hub it is an agentops.Env; a test puts a fake there
// that records the calls and can fail on any step.

// The API changes nodes through the manager.
var _ api.NodeOps = (*Manager)(nil)

// Ops is what the operations ask of agentops. EnvOps makes one from an
// agentops.Env, and a test has its own.
//
// The calls that take a client are given the node's own connection, the one
// its loop keeps (or, while a node is being installed, one just made by
// Connect), and they never close it.
type Ops interface {
	Probe(ctx context.Context, host string, port int) (fingerprint string, err error)
	InstallAgent(ctx context.Context, node model.Node, login agentops.Login, progress agentops.Progress) (agentops.InstallResult, error)
	Uninstall(ctx context.Context, node model.Node, login agentops.Login, progress agentops.Progress) error
	// UndoInstall takes off the server what an install that failed put there.
	UndoInstall(ctx context.Context, node model.Node, login agentops.Login, progress agentops.Progress) error
	// Connect opens a link to the node's agent, which the caller closes.
	Connect(ctx context.Context, node model.Node) (Client, error)
	Provision(ctx context.Context, c Client, in agentops.ProvisionInput, progress agentops.Progress) (agentops.ProvisionResult, error)
	PushConfig(ctx context.Context, c Client, in nodecfg.Input) error
	CheckTarget(ctx context.Context, c Client, target string) (agentops.HandshakeCheck, error)
	UpdateAgent(ctx context.Context, c Client, node model.Node, progress agentops.Progress) (agentops.Versions, error)
	UpdateXray(ctx context.Context, c Client, node model.Node, progress agentops.Progress) (agentops.Versions, error)
	RenewCert(ctx context.Context, c Client, node model.Node) (agentops.CertInfo, error)
}

// Downloads is the project's releases as the manager meets them: it says
// which agent the latest one carries, and lets go of what an install or an
// update downloaded once the jobs that used it are over, since a router has
// little memory to spare. *artifacts.Holder is one.
type Downloads interface {
	LatestAgent(ctx context.Context, arch string) (version string, err error)
	Release()
}

// EnvOps runs the operations with an agentops.Env. The Env's calls take the
// agent's own client type, which is what a connector built on the Env makes,
// so the adapter asks each client it is handed for it, and a connection that
// was made by anything else is refused in a sentence.
func EnvOps(env *agentops.Env) Ops { return envOps{env} }

type envOps struct{ env *agentops.Env }

var errForeignClient = errors.New("the connection to the node was not made by the agent client, so the operations can't use it")

func agentOf(c Client) (*agent.Client, error) {
	if ac, ok := c.(*agent.Client); ok && ac != nil {
		return ac, nil
	}
	return nil, errForeignClient
}

func (o envOps) Probe(ctx context.Context, host string, port int) (string, error) {
	return o.env.Probe(ctx, host, port)
}

func (o envOps) InstallAgent(ctx context.Context, node model.Node, login agentops.Login, progress agentops.Progress) (agentops.InstallResult, error) {
	return o.env.InstallAgent(ctx, node, login, progress)
}

func (o envOps) Uninstall(ctx context.Context, node model.Node, login agentops.Login, progress agentops.Progress) error {
	return o.env.Uninstall(ctx, node, login, progress)
}

func (o envOps) UndoInstall(ctx context.Context, node model.Node, login agentops.Login, progress agentops.Progress) error {
	return o.env.UndoInstall(ctx, node, login, progress)
}

func (o envOps) Connect(ctx context.Context, node model.Node) (Client, error) {
	c, err := o.env.Connect(ctx, node)
	if err != nil {
		return nil, err // not c: a nil *agent.Client in a Client is not nil
	}
	return c, nil
}

func (o envOps) Provision(ctx context.Context, c Client, in agentops.ProvisionInput, progress agentops.Progress) (agentops.ProvisionResult, error) {
	ac, err := agentOf(c)
	if err != nil {
		return agentops.ProvisionResult{}, err
	}
	return o.env.Provision(ctx, ac, in, progress)
}

func (o envOps) PushConfig(ctx context.Context, c Client, in nodecfg.Input) error {
	ac, err := agentOf(c)
	if err != nil {
		return err
	}
	return o.env.PushConfig(ctx, ac, in)
}

func (o envOps) CheckTarget(ctx context.Context, c Client, target string) (agentops.HandshakeCheck, error) {
	ac, err := agentOf(c)
	if err != nil {
		return agentops.HandshakeCheck{}, err
	}
	return o.env.CheckTarget(ctx, ac, target)
}

func (o envOps) UpdateAgent(ctx context.Context, c Client, node model.Node, progress agentops.Progress) (agentops.Versions, error) {
	ac, err := agentOf(c)
	if err != nil {
		return agentops.Versions{}, err
	}
	return o.env.UpdateAgent(ctx, ac, node, progress)
}

func (o envOps) UpdateXray(ctx context.Context, c Client, node model.Node, progress agentops.Progress) (agentops.Versions, error) {
	ac, err := agentOf(c)
	if err != nil {
		return agentops.Versions{}, err
	}
	return o.env.UpdateXray(ctx, ac, node, progress)
}

func (o envOps) RenewCert(ctx context.Context, c Client, node model.Node) (agentops.CertInfo, error) {
	ac, err := agentOf(c)
	if err != nil {
		return agentops.CertInfo{}, err
	}
	return o.env.RenewCert(ctx, ac, node)
}

// ErrNoOps is why an operation can't run when the manager was not given what
// the operations run on.
var ErrNoOps = errors.New("manager: the node operations are not set up")

// Ops are the node operations over the CA now in use. They are made when the
// CA is first usable and again whenever another CA takes its place (a backup
// restored), since an agentops.Env holds the CA it signs with and has to be
// made anew for the new one; between those the same value is handed out.
//
// The error says why there are none: the manager has no NewOps, or the CA
// can't be used (see CAError), in which case nothing could be signed or dialled
// anyway.
func (m *Manager) Ops() (Ops, error) {
	if m.cfg.NewOps == nil {
		return nil, ErrNoOps
	}
	m.mu.RLock()
	ca, text, caErr := m.ca, m.caText, m.caErr
	m.mu.RUnlock()
	if ca == nil {
		if caErr == nil {
			caErr = errors.New("there is none yet")
		}
		return nil, fmt.Errorf("the hub can't use its certificate authority: %w", caErr)
	}
	m.opsMu.Lock()
	defer m.opsMu.Unlock()
	if m.ops == nil || m.opsFor != text {
		m.ops, m.opsFor = m.cfg.NewOps(ca), text
	}
	return m.ops, nil
}

// operate is Ops with the failure worded for the page, for an operation that
// starts from a request.
func (m *Manager) operate() (Ops, error) {
	ops, err := m.Ops()
	switch {
	case err == nil:
		return ops, nil
	case errors.Is(err, ErrNoOps):
		return nil, &api.Error{Code: api.CodeInternal, Msg: "This hub isn't set up to manage nodes."}
	}
	return nil, &api.Error{Code: api.CodeInternal, Msg: sentence(err.Error())}
}

// Failures. An operation's error is either one the page has words for, which
// is an *api.Error (or an error of the model's or the job engine's that the
// API words itself), or the hub's own, which the API logs and hides.

func unreachable(what string) error {
	return &api.Error{Code: api.CodeNodeUnreachable, Msg: "The router can't reach " + what + " right now."}
}

func badRequest(msg string) error { return &api.Error{Code: api.CodeBadRequest, Msg: msg} }

func neverInstalled() error {
	return badRequest("This install didn't finish. It can only be cleaned up or forgotten.")
}

// opFailure words an error that an Ops call returned. agentops writes the
// text of its errors for the page, with any login taken out, and what the page
// keys on is the code: a node that can't be reached, an agent that is too old
// or runs another Xray than the router's. Anything else of agentops's is
// shown as the sentence it is, under the hub's own code.
//
// The typed errors come before the context's own. A dial that times out is
// both a node that can't be reached and, to errors.Is, a context deadline, and
// the first is what it is: an address the router can't reach (one that isn't
// on the domestic list) gives exactly this.
func opFailure(err error) error {
	var apiErr *api.Error
	var invalid *model.InvalidError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &apiErr), errors.As(err, &invalid), errors.Is(err, model.ErrNotFound), errors.Is(err, jobs.ErrBusy):
		return err
	case errors.Is(err, agentops.ErrHostKeyChanged):
		return &api.Error{Code: api.CodeHostKeyChanged, Msg: clipText(err.Error())}
	case errors.Is(err, agentops.ErrUnreachable), errors.Is(err, agentops.ErrAgentCert):
		return &api.Error{Code: api.CodeNodeUnreachable, Msg: clipText(err.Error())}
	case errors.Is(err, agentops.ErrVersionMismatch):
		return &api.Error{Code: api.CodeVersionMismatch, Msg: clipText(err.Error())}
	case errors.Is(err, agentops.ErrAgentOutdated):
		return &api.Error{Code: api.CodeAgentTooOld, Msg: clipText(err.Error())}
	case errors.Is(err, agentops.ErrServerHasXray):
		return &api.Error{Code: api.CodeServerHasXray, Msg: clipText(err.Error())}
	case errors.Is(err, agentops.ErrServerIsNode):
		return &api.Error{Code: api.CodeServerIsNode, Msg: clipText(err.Error())}
	case errors.Is(err, agentops.ErrServerHasAgent):
		return &api.Error{Code: api.CodeServerHasAgent, Msg: clipText(err.Error())}
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err // the request's own context ended, which the API leaves unanswered
	case errors.Is(err, nodecfg.ErrInvalid):
		return badRequest(sentence(strings.TrimPrefix(err.Error(), nodecfg.ErrInvalid.Error()+": ")))
	}
	return &api.Error{Code: api.CodeInternal, Msg: clipText(err.Error())}
}

// JobErrorCode is the code of a job's error, which the job engine is given
// as Config.ErrorCode: the code an answer with that error would have, as
// opFailure words it, and "" for the hub's own errors, which the page has no
// words for.
func JobErrorCode(err error) string {
	var e *api.Error
	if errors.As(opFailure(err), &e) && e.Code != api.CodeInternal {
		return e.Code
	}
	return ""
}

// maxMsg is the longest message that goes to the page, in characters.
const maxMsg = 500

func clipText(s string) string {
	s = strings.ToValidUTF8(strings.TrimSpace(s), "�")
	if utf8.RuneCountInString(s) > maxMsg {
		s = string([]rune(s)[:maxMsg-1]) + "…"
	}
	return s
}

// sentence makes a message start with a capital and end with a full stop.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	if r >= 'a' && r <= 'z' {
		s = string(r-'a'+'A') + s[n:]
	}
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "?") {
		s += "."
	}
	return s
}
