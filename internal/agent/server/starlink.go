package server

import (
	"context"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/starlink"
	slpb "github.com/nasnet-community/nasnet-panel-linux/pkg/starlink/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// dishAddr resolves a request's dish address with the package default.
func dishAddr(req *pb.StarlinkRequest) string {
	if req != nil && req.DishAddress != "" {
		return req.DishAddress
	}
	return starlink.DefaultDishAddress
}

// GetStarlinkStatus queries the local Starlink dish and returns its status.
// On any failure (channel build, dial, RPC) we return Available=false instead
// of an RPC error so the panel can render a unified "Dish Unreachable" state
// without distinguishing transport vs application failure.
func (s *Server) GetStarlinkStatus(ctx context.Context, req *pb.StarlinkRequest) (*pb.StarlinkStatusResponse, error) {
	addr := dishAddr(req)

	client, err := starlink.SharedClient(addr)
	if err != nil {
		return &pb.StarlinkStatusResponse{Available: false}, nil
	}

	status, err := client.GetStatus(ctx)
	if err != nil {
		// Wedged channel — drop it so the next poll redials.
		starlink.EvictPooled(addr)
		return &pb.StarlinkStatusResponse{Available: false}, nil
	}

	return &pb.StarlinkStatusResponse{
		Available:                        status.Available,
		DownlinkThroughputBps:            float32(status.DownlinkThroughputBps),
		UplinkThroughputBps:              float32(status.UplinkThroughputBps),
		PopPingLatencyMs:                 float32(status.PopPingLatencyMs),
		PopPingDropRate:                  float32(status.PopPingDropRate),
		EthSpeedMbps:                     status.EthSpeedMbps,
		ObstructionFraction:              float32(status.ObstructionFraction),
		CurrentlyObstructed:              status.CurrentlyObstructed,
		AvgProlongedObstructionDurationS: float32(status.AvgProlongedObstructionDurationS),
		AvgProlongedObstructionIntervalS: float32(status.AvgProlongedObstructionIntervalS),
		AlertThermalThrottle:             status.AlertThermalThrottle,
		AlertThermalShutdown:             status.AlertThermalShutdown,
		AlertIsHeating:                   status.AlertIsHeating,
		AlertSlowEthernet:                status.AlertSlowEthernet,
		AlertPowerSaveIdle:               status.AlertPowerSaveIdle,
		AlertMotorsStuck:                 status.AlertMotorsStuck,
		AlertNoEthernetLink:              status.AlertNoEthernetLink,
		AlertUnexpectedLocation:          status.AlertUnexpectedLocation,
		AlertRoaming:                     status.AlertRoaming,
		AlertMastNotNearVertical:         status.AlertMastNotNearVert,
		AlertInstallPending:              status.AlertInstallPending,
		HardwareVersion:                  status.HardwareVersion,
		SoftwareVersion:                  status.SoftwareVersion,
		CountryCode:                      status.CountryCode,
		UptimeS:                          status.UptimeS,
		BootCount:                        status.BootCount,
		GpsValid:                         status.GpsValid,
		GpsSats:                          status.GpsSats,
		TiltAngleDeg:                     float32(status.TiltAngleDeg),
		BoresightAzimuthDeg:              float32(status.BoresightAzimuthDeg),
		BoresightElevationDeg:            float32(status.BoresightElevationDeg),
		SoftwareUpdateState:              status.SoftwareUpdateState,
		SoftwareUpdateProgress:           float32(status.SoftwareUpdateProgress),
		OutageCause:                      status.OutageCause,
		OutageDurationNs:                 status.OutageDurationNs,
		DisablementCode:                  status.DisablementCode,
		MobilityClass:                    status.MobilityClass,
		ClassOfService:                   status.ClassOfService,
		CellId:                           status.CellID,
		SatelliteId:                      status.SatelliteID,
		GatewayId:                        status.GatewayID,
		OnBackupBeam:                     status.OnBackupBeam,
		Latitude:                         status.Latitude,
		Longitude:                        status.Longitude,
		Altitude:                         status.Altitude,
		IsSnrAboveNoiseFloor:             status.IsSnrAboveNoiseFloor,
		IsSnrPersistentlyLow:             status.IsSnrPersistentlyLow,
		AttitudeUncertaintyDeg:           float32(status.AttitudeUncertaintyDeg),
		AttitudeEstimationState:          status.AttitudeEstimationState,
		DesiredBoresightAzimuthDeg:       float32(status.DesiredBoresightAzimuthDeg),
		DesiredBoresightElevationDeg:     float32(status.DesiredBoresightElevationDeg),
		ActuatorState:                    status.ActuatorState,
		HasActuators:                     status.HasActuators,
	}, nil
}

// GetStarlinkObstructionMap queries the local Starlink dish for its obstruction map.
// Mirrors the status path: on dial / RPC failure return an empty map (NumRows=0)
// so the panel can treat it identically to "dish unreachable" without a 500.
func (s *Server) GetStarlinkObstructionMap(ctx context.Context, req *pb.StarlinkRequest) (*pb.StarlinkObstructionMapResponse, error) {
	addr := dishAddr(req)

	client, err := starlink.SharedClient(addr)
	if err != nil {
		return &pb.StarlinkObstructionMapResponse{}, nil
	}

	m, err := client.GetObstructionMap(ctx)
	if err != nil {
		starlink.EvictPooled(addr)
		return &pb.StarlinkObstructionMapResponse{}, nil
	}

	return &pb.StarlinkObstructionMapResponse{
		NumRows:        m.NumRows,
		NumCols:        m.NumCols,
		Snr:            m.SNR,
		MaxThetaDeg:    m.MaxThetaDeg,
		ReferenceFrame: m.ReferenceFrame,
	}, nil
}

// StarlinkHandle forwards an allowlisted dish request. Dish failures travel
// in the response code, not as an RPC error, so the hub can tell a refusing
// dish from an agent that lacks this RPC.
func (s *Server) StarlinkHandle(ctx context.Context, req *pb.StarlinkHandleRequest) (*pb.StarlinkHandleResponse, error) {
	var dishReq slpb.Request
	if err := proto.Unmarshal(req.GetRequest(), &dishReq); err != nil {
		return &pb.StarlinkHandleResponse{Code: int32(codes.InvalidArgument), Message: "malformed dish request"}, nil
	}
	// Forward only the oneof. Unknown fields (a request type this build does
	// not know) and routing fields would otherwise ride through re-marshal
	// past the allowlist.
	clean := &slpb.Request{Request: dishReq.Request}
	if !starlink.PassthroughAllowed(clean) {
		return &pb.StarlinkHandleResponse{Code: int32(codes.PermissionDenied), Message: "request type not allowed by agent"}, nil
	}

	// Give up on the dish before the caller gives up on us, so a dish that
	// goes silent (mid-reboot) comes back as a relayed DeadlineExceeded
	// instead of the hub's own timeout.
	if dl, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl.Add(-2*time.Second))
		defer cancel()
	}

	addr := req.GetDishAddress()
	if addr == "" {
		addr = starlink.DefaultDishAddress
	}
	client, err := starlink.SharedClient(addr)
	if err != nil {
		return &pb.StarlinkHandleResponse{Code: int32(codes.Unavailable), Message: err.Error()}, nil
	}

	resp, err := client.Handle(ctx, clean)
	if err != nil {
		st := status.Convert(err)
		if st.Code() == codes.Unavailable || st.Code() == codes.DeadlineExceeded {
			starlink.EvictPooled(addr)
		}
		return &pb.StarlinkHandleResponse{Code: int32(st.Code()), Message: st.Message()}, nil
	}

	raw, err := proto.Marshal(resp)
	if err != nil {
		return &pb.StarlinkHandleResponse{Code: int32(codes.Internal), Message: "marshal dish response: " + err.Error()}, nil
	}
	return &pb.StarlinkHandleResponse{Response: raw}, nil
}
