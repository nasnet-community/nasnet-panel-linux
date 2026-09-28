package agent

import (
	"context"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// CertInstallClient rotates the agent's own TLS certificate and reads
// certificate files on the node.
type CertInstallClient interface {
	InstallCertificate(context.Context, *pb.InstallCertificateRequest) (*pb.InstallCertificateResponse, error)
	InspectCertFile(context.Context, *pb.InspectCertFileRequest) (*pb.InspectCertFileResponse, error)
}

func (c *Client) InstallCertificate(ctx context.Context, req *pb.InstallCertificateRequest) (*pb.InstallCertificateResponse, error) {
	return c.client.InstallCertificate(ctx, req)
}

func (c *Client) InspectCertFile(ctx context.Context, req *pb.InspectCertFileRequest) (*pb.InspectCertFileResponse, error) {
	return c.client.InspectCertFile(ctx, req)
}

func (c *ReverseClient) InstallCertificate(ctx context.Context, req *pb.InstallCertificateRequest) (*pb.InstallCertificateResponse, error) {
	var resp pb.InstallCertificateResponse
	if err := c.sendUnary(ctx, pb.NodeAgent_InstallCertificate_FullMethodName, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *ReverseClient) InspectCertFile(ctx context.Context, req *pb.InspectCertFileRequest) (*pb.InspectCertFileResponse, error) {
	var resp pb.InspectCertFileResponse
	if err := c.sendUnary(ctx, pb.NodeAgent_InspectCertFile_FullMethodName, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
