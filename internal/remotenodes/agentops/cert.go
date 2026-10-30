// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"crypto/x509"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// CertInfo is a node certificate, for the manager to keep.
type CertInfo struct {
	Serial    string
	NotBefore time.Time
	NotAfter  time.Time
}

// RenewCert gives a node a new certificate from the CA, for the same id and
// address, through the agent's own InstallCertificate, which keeps the node id
// and refuses a certificate that isn't for this node or this CA. The key is
// made here, sent once and dropped.
//
// It then connects again on a new link and checks that the agent presents the
// new certificate, so a renewal that the agent took but doesn't use is not
// taken for one that worked.
func (e *Env) RenewCert(ctx context.Context, c *agent.Client, node model.Node) (_ CertInfo, err error) {
	defer func() { err = ctxErr(ctx, err) }()
	if e.CA == nil {
		return CertInfo{}, ErrNoCA
	}
	host, err := model.CleanHost(node.Host)
	if err != nil {
		return CertInfo{}, err
	}
	is, err := e.CA.IssueNodeCert(node.ID, node.Name, host, e.now())
	if err != nil {
		return CertInfo{}, plainErr(err)
	}
	defer clear(is.KeyPEM)

	resp, err := c.InstallCertificate(ctx, &pb.InstallCertificateRequest{CertPem: is.CertPEM, KeyPem: is.KeyPEM})
	if err != nil {
		return CertInfo{}, certFailure(err, c)
	}
	if resp.Serial != is.Serial || resp.NotAfterUnix != is.NotAfter.Unix() {
		return CertInfo{}, &opError{msg: "The agent says it installed another certificate than the one that was sent.", cause: nil}
	}

	var mu sync.Mutex
	var seen string
	nc, err := e.dialAgent(ctx, node, e.dialTimeout(), func(leaf *x509.Certificate) {
		mu.Lock()
		seen = leaf.SerialNumber.String()
		mu.Unlock()
	})
	if err != nil {
		return CertInfo{}, err
	}
	defer nc.Close()
	if _, err := nc.GetVersion(ctx); err != nil {
		return CertInfo{}, callFailure(err, node)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen != is.Serial {
		return CertInfo{}, &opError{msg: "The agent took the new certificate, but still shows the old one.", cause: nil}
	}
	e.log().Info("renewed a node certificate", "node", node.ID, "host", node.Host, "until", is.NotAfter)
	return CertInfo{Serial: is.Serial, NotBefore: is.NotBefore, NotAfter: is.NotAfter}, nil
}

// certFailure says why the agent didn't take a certificate, in its words.
func certFailure(err error, c *agent.Client) error {
	if ctxDone(err) {
		return err
	}
	if down := linkDown(err, c); down != nil {
		return down
	}
	switch status.Code(err) {
	case codes.Unimplemented:
		return newErr(ErrAgentOutdated, err, "This node's agent is too old to take a new certificate. Update the agent first.")
	case codes.InvalidArgument, codes.FailedPrecondition:
		return &opError{msg: sentence("The agent refused the new certificate: " + statusMessage(err)), cause: err}
	}
	return &opError{msg: sentence("The agent couldn't take the new certificate: " + statusMessage(err)), cause: err}
}
