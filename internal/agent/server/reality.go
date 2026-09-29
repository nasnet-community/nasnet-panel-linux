package server

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/realitydest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxRealityDestNames bounds one probe so a hub request cannot fan out into
// hundreds of handshakes from the node.
const maxRealityDestNames = 16

// ProbeRealityDest runs the REALITY dest check from the node's own network,
// which is the vantage point that matters: the dest is dialled from here when
// the inbound serves clients.
func (s *Server) ProbeRealityDest(ctx context.Context, req *pb.RealityDestProbeRequest) (*pb.RealityDestProbeResponse, error) {
	if len(req.GetServerNames()) > maxRealityDestNames {
		return nil, status.Errorf(codes.InvalidArgument, "at most %d server names per check", maxRealityDestNames)
	}
	report, err := realitydest.Probe(ctx, realitydest.FromProtoRequest(req))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return realitydest.ReportToProto(report), nil
}
