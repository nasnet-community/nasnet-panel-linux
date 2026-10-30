// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// Versions is what a node reports it runs.
type Versions struct {
	Agent string // as the agent says, such as "v1.3.0"
	Xray  string // such as "26.10.21", empty while there is no Xray
}

const (
	// defaultDialTimeout is how long a connection to an agent may take to come up.
	defaultDialTimeout = 15 * time.Second
	// verifyWait is how long a freshly started agent has to answer.
	verifyWait = 30 * time.Second
)

// dialAgent opens the hub's mutual TLS link to a node's agent, from the bound
// address and with the node's own address as the name its certificate must
// carry. seen, when set, is told the certificate the agent showed.
//
// agent.NewClient ignores a context, and gives up after wait on its own, so a
// cancelled ctx is answered at once and the client that comes up late is
// closed.
func (e *Env) dialAgent(ctx context.Context, node model.Node, wait time.Duration, seen func(*x509.Certificate)) (*agent.Client, error) {
	host, err := model.CleanHost(node.Host)
	if err != nil {
		return nil, err
	}
	if err := model.ValidPort(node.AgentPort); err != nil {
		return nil, err
	}
	certPEM, keyPEM, err := e.hubClientCert()
	if err != nil {
		return nil, err
	}
	cfg := &agent.TLSConfig{CACert: string(e.CA.CertPEM()), ClientCert: certPEM, ClientKey: keyPEM, ServerName: host}
	if seen != nil {
		cfg.VerifyLeaf = func(leaf *x509.Certificate) error { seen(leaf); return nil }
	}
	d := e.dialer()
	type result struct {
		c   *agent.Client
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := agent.NewClient(net.JoinHostPort(host, strconv.Itoa(node.AgentPort)), cfg, wait,
			// without this a failed connection is only "deadline exceeded"
			grpc.WithReturnConnectionError(),
			// a closed port or an address that can't be used fails at once,
			// where the default is to keep trying until the time is up. A
			// filtered port that only times out still gets all of it.
			grpc.FailOnNonTempDialError(true),
			grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
				return d.DialContext(ctx, "tcp", addr)
			}))
		done <- result{c, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return nil, dialFailure(r.err, host, node.AgentPort)
		}
		return r.c, nil
	case <-ctx.Done():
		go func() {
			if r := <-done; r.c != nil {
				r.c.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

// dialFailure says what a failed connection to an agent means: a certificate
// one side didn't accept, or a node that doesn't answer.
func dialFailure(err error, host string, port int) error {
	msg := err.Error()
	for _, sign := range []string{"x509:", "bad certificate", "unknown certificate", "certificate required", "expired certificate"} {
		if strings.Contains(msg, sign) {
			return newErr(ErrAgentCert, err, "The agent's certificate wasn't accepted (%s). The agent may trust another hub, or its certificate may have run out.", tidyCause(err))
		}
	}
	return &UnreachableError{What: "agent", Host: host, Port: port, Err: err}
}

// callFailure says what a failed call on a link that came up means.
func callFailure(err error, node model.Node) error {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
		return &UnreachableError{What: "agent", Host: node.Host, Port: node.AgentPort, Err: err}
	case codes.Canceled:
		return err
	}
	return &opError{msg: sentence("The agent answered with an error: " + grpcReason(err)), cause: err}
}

// linkDown says a call failed because the link is gone, in the words for a
// node that doesn't answer. It is nil for any other failure.
func linkDown(err error, c *agent.Client) error {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
	default:
		return nil
	}
	host, portText, splitErr := net.SplitHostPort(c.Target())
	port, _ := strconv.Atoi(portText)
	if splitErr != nil {
		host = c.Target()
	}
	return &UnreachableError{What: "agent", Host: host, Port: port, Err: err}
}

// proveManaged shows that an agent runs the managed profile. GetSSHStatus is
// read-only and the profile refuses it, so a managed agent answers
// PermissionDenied. An agent that predates the profile ignores the line in its
// config and runs everything, and so answers, or fails on its own: either way
// it is not managed, and nothing the hub would send it is safe to send.
func proveManaged(ctx context.Context, c *agent.Client, version string) error {
	_, err := c.GetSSHStatus(ctx)
	switch status.Code(err) {
	case codes.PermissionDenied:
		return nil
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return err // no answer, so no proof either way
	}
	return &NotManagedError{AgentVersion: version}
}

// Connect opens the hub's link to a node's agent: mutual TLS from the bound
// address, the agent's version, and the proof that it runs the managed
// profile. The manager keeps the client open to poll and to call the other
// operations, and closes it. An agent that doesn't prove managed is refused
// with ErrNotManaged, and one that can't be reached with ErrUnreachable.
func (e *Env) Connect(ctx context.Context, node model.Node) (_ *agent.Client, err error) {
	defer func() { err = ctxErr(ctx, err) }()
	c, err := e.dialAgent(ctx, node, e.dialTimeout(), nil)
	if err != nil {
		return nil, err
	}
	info, err := c.GetVersion(ctx)
	if err != nil {
		c.Close()
		return nil, callFailure(err, node)
	}
	if err := proveManaged(ctx, c, info.AgentVersion); err != nil {
		c.Close()
		var nm *NotManagedError
		if errors.As(err, &nm) {
			return nil, err
		}
		return nil, callFailure(err, node)
	}
	return c, nil
}

// verifyAgent is what an install finishes with: the agent it just started
// answers over the link the hub will use, is the version that was sent, and
// runs the managed profile. The agent has only just started, so it is given a
// little time, unless the trouble is a certificate, which time won't mend.
func (e *Env) verifyAgent(ctx context.Context, node model.Node, want *artifacts.Agent) (Versions, error) {
	var c *agent.Client
	err := poll(ctx, verifyWait, 2*time.Second, func(ctx context.Context) (bool, error) {
		var err error
		if c, err = e.dialAgent(ctx, node, min(e.dialTimeout(), 5*time.Second), nil); err != nil {
			if errors.Is(err, ErrAgentCert) {
				return false, stop(err)
			}
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return Versions{}, err
	}
	defer c.Close()
	info, err := c.GetVersion(ctx)
	if err != nil {
		return Versions{}, callFailure(err, node)
	}
	if !sameVersion(info.AgentVersion, want.Version) {
		return Versions{}, &VersionMismatchError{What: "agent", Want: want.Version, Got: info.AgentVersion}
	}
	if err := proveManaged(ctx, c, info.AgentVersion); err != nil {
		var nm *NotManagedError
		if errors.As(err, &nm) {
			return Versions{}, err
		}
		return Versions{}, callFailure(err, node)
	}
	return Versions{Agent: info.AgentVersion, Xray: ParseXrayVersion(info.XrayVersion)}, nil
}

// sameVersion compares release tags, which an agent reports with the leading v
// and a file may leave off.
func sameVersion(a, b string) bool {
	return strings.TrimPrefix(strings.TrimSpace(a), "v") == strings.TrimPrefix(strings.TrimSpace(b), "v")
}

// ParseXrayVersion is the version in what an agent reports for Xray, the first
// line of `xray version`: "Xray 26.10.21 (Xray, Penetrates Everything.) ...".
// It is empty for anything else, such as "unknown" when there is no Xray.
func ParseXrayVersion(line string) string {
	f := strings.Fields(line)
	if len(f) < 2 || !strings.EqualFold(f[0], "Xray") {
		return ""
	}
	v := strings.TrimPrefix(f[1], "v")
	if v == "" || v[0] < '0' || v[0] > '9' {
		return ""
	}
	return v
}

// grpcReason is a gRPC error without the machinery around it: the agent's own
// words, or what went wrong with the connection.
func grpcReason(err error) string {
	if st, ok := status.FromError(err); ok && st.Message() != "" {
		return reasonTail(st.Message())
	}
	return reasonTail(err.Error())
}

// reasonTail cuts a connection error down to its last cause:
//
//	... connection error: desc = "transport: Error while dialing: dial tcp 1.2.3.4:9090: connect: connection refused"
func reasonTail(msg string) string {
	if i := strings.LastIndex(msg, `desc = "`); i >= 0 {
		msg = strings.TrimSuffix(msg[i+len(`desc = "`):], `"`)
	}
	for _, p := range []string{"transport: Error while dialing: ", "transport: authentication handshake failed: ", "transport: "} {
		msg = strings.TrimPrefix(msg, p)
	}
	return msg
}

// statusMessage is what the agent said in a gRPC error, however many times it
// was wrapped on the way.
func statusMessage(err error) string {
	var se interface{ GRPCStatus() *status.Status }
	if errors.As(err, &se) {
		if m := se.GRPCStatus().Message(); m != "" {
			return m
		}
	}
	return tidyCause(err)
}

func trimPrefixes(s string, prefixes ...string) string {
	for _, p := range prefixes {
		s = strings.TrimPrefix(s, p)
	}
	return s
}
