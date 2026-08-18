package starlink

import (
	"context"
	"fmt"
	"math"
	"sync"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/starlink/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const DefaultDishAddress = "192.168.100.1:9200"

// Client wraps gRPC communication with a Starlink dish.
type Client struct {
	conn   *grpc.ClientConn
	device pb.DeviceClient
}

// NewClient opens a lazy gRPC channel to the Starlink dish (insecure LAN).
// Returned client is safe for concurrent use; the underlying connection is
// established on first RPC and reconnects automatically on failure.
func NewClient(addr string) (*Client, error) {
	if addr == "" {
		addr = DefaultDishAddress
	}

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Starlink client for %s: %w", addr, err)
	}

	return &Client{
		conn:   conn,
		device: pb.NewDeviceClient(conn),
	}, nil
}

// Close closes the gRPC connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// pool: one *Client per dish address. Long-lived agent + 5-10s poll →
// re-dial burns RTTs. Never evicts; addresses bounded by node count.
var (
	poolMu sync.Mutex
	pool   = map[string]*Client{}
)

// SharedClient returns a pooled client for the given dish address. Caller
// must NOT Close() the returned client — pool ownership stays with the pool.
func SharedClient(addr string) (*Client, error) {
	if addr == "" {
		addr = DefaultDishAddress
	}
	poolMu.Lock()
	defer poolMu.Unlock()
	if c, ok := pool[addr]; ok {
		return c, nil
	}
	c, err := NewClient(addr)
	if err != nil {
		return nil, err
	}
	pool[addr] = c
	return c, nil
}

// EvictPooled tears down and drops the pooled client for addr. Use when a
// caller decides the channel is wedged — next SharedClient redials.
func EvictPooled(addr string) {
	if addr == "" {
		addr = DefaultDishAddress
	}
	poolMu.Lock()
	c := pool[addr]
	delete(pool, addr)
	poolMu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// StarlinkStatus contains flattened dish status metrics.
type StarlinkStatus struct {
	Available bool `json:"available"`

	// Performance
	DownlinkThroughputBps float64 `json:"downlink_throughput_bps"`
	UplinkThroughputBps   float64 `json:"uplink_throughput_bps"`
	PopPingLatencyMs      float64 `json:"pop_ping_latency_ms"`
	PopPingDropRate       float64 `json:"pop_ping_drop_rate"`
	EthSpeedMbps          int32   `json:"eth_speed_mbps"`

	// Obstruction
	ObstructionFraction              float64 `json:"obstruction_fraction"`
	CurrentlyObstructed              bool    `json:"currently_obstructed"`
	AvgProlongedObstructionDurationS float64 `json:"avg_prolonged_obstruction_duration_s"`
	AvgProlongedObstructionIntervalS float64 `json:"avg_prolonged_obstruction_interval_s"`

	// Alerts
	AlertThermalThrottle    bool `json:"alert_thermal_throttle"`
	AlertThermalShutdown    bool `json:"alert_thermal_shutdown"`
	AlertIsHeating          bool `json:"alert_is_heating"`
	AlertSlowEthernet       bool `json:"alert_slow_ethernet"`
	AlertPowerSaveIdle      bool `json:"alert_power_save_idle"`
	AlertMotorsStuck        bool `json:"alert_motors_stuck"`
	AlertNoEthernetLink     bool `json:"alert_no_ethernet_link"`
	AlertUnexpectedLocation bool `json:"alert_unexpected_location"`
	AlertRoaming            bool `json:"alert_roaming"`
	AlertMastNotNearVert    bool `json:"alert_mast_not_near_vertical"`
	AlertInstallPending     bool `json:"alert_install_pending"`

	// Device
	HardwareVersion string `json:"hardware_version"`
	SoftwareVersion string `json:"software_version"`
	CountryCode     string `json:"country_code"`
	UptimeS         uint64 `json:"uptime_s"`
	BootCount       int32  `json:"boot_count"`

	// GPS
	GpsValid bool   `json:"gps_valid"`
	GpsSats  uint32 `json:"gps_sats"`

	// Alignment
	TiltAngleDeg          float64 `json:"tilt_angle_deg"`
	BoresightAzimuthDeg   float64 `json:"boresight_azimuth_deg"`
	BoresightElevationDeg float64 `json:"boresight_elevation_deg"`
	// AttitudeUncertaintyDeg is the filter's 1-sigma heading error. The
	// obstruction map only rotates FRAME_UT into compass coordinates when
	// the attitude filter has converged, so this rides along with it.
	AttitudeUncertaintyDeg       float64 `json:"attitude_uncertainty_deg"`
	AttitudeEstimationState      string  `json:"attitude_estimation_state"`
	DesiredBoresightAzimuthDeg   float64 `json:"desired_boresight_azimuth_deg"`
	DesiredBoresightElevationDeg float64 `json:"desired_boresight_elevation_deg"`
	ActuatorState                string  `json:"actuator_state"`
	HasActuators                 string  `json:"has_actuators"`

	// Software update
	SoftwareUpdateState    string  `json:"software_update_state"`
	SoftwareUpdateProgress float64 `json:"software_update_progress"`

	// Outage
	OutageCause      string `json:"outage_cause"`
	OutageDurationNs uint64 `json:"outage_duration_ns"`

	// Service
	DisablementCode string `json:"disablement_code"`
	MobilityClass   string `json:"mobility_class"`
	ClassOfService  string `json:"class_of_service"`

	// Satellite context
	CellID       uint32 `json:"cell_id"`
	SatelliteID  uint32 `json:"satellite_id"`
	GatewayID    uint32 `json:"gateway_id"`
	OnBackupBeam bool   `json:"on_backup_beam"`

	// Location
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Altitude  float64 `json:"altitude"`

	// Signal
	IsSnrAboveNoiseFloor bool `json:"is_snr_above_noise_floor"`
	IsSnrPersistentlyLow bool `json:"is_snr_persistently_low"`
}

// ObstructionMap holds the dish obstruction map data.
type ObstructionMap struct {
	NumRows        uint32    `json:"num_rows"`
	NumCols        uint32    `json:"num_cols"`
	SNR            []float32 `json:"snr"`
	MaxThetaDeg    float32   `json:"max_theta_deg"`
	ReferenceFrame string    `json:"reference_frame"`
}

// Location holds dish GPS location.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Altitude  float64 `json:"altitude"`
}

func (c *Client) handle(ctx context.Context, req *pb.Request) (*pb.Response, error) {
	return c.device.Handle(ctx, req)
}

// GetStatus retrieves the dish status, context, and location in a single flattened struct.
func (c *Client) GetStatus(ctx context.Context) (*StarlinkStatus, error) {
	// Get dish status
	statusResp, err := c.handle(ctx, &pb.Request{
		Request: &pb.Request_GetStatus{GetStatus: &pb.GetStatusRequest{}},
	})
	if err != nil {
		return nil, fmt.Errorf("get status: %w", err)
	}

	s := statusResp.GetDishGetStatus()
	if s == nil {
		return nil, fmt.Errorf("unexpected response type for get_status")
	}

	status := &StarlinkStatus{
		Available:             true,
		DownlinkThroughputBps: float64(s.DownlinkThroughputBps),
		UplinkThroughputBps:   float64(s.UplinkThroughputBps),
		PopPingLatencyMs:      float64(s.PopPingLatencyMs),
		PopPingDropRate:       float64(s.PopPingDropRate),
		EthSpeedMbps:          s.EthSpeedMbps,
		IsSnrAboveNoiseFloor:  s.IsSnrAboveNoiseFloor,
		IsSnrPersistentlyLow:  s.IsSnrPersistentlyLow,
		BoresightAzimuthDeg:   float64(s.BoresightAzimuthDeg),
		BoresightElevationDeg: float64(s.BoresightElevationDeg),
		MobilityClass:         s.MobilityClass.String(),
		ClassOfService:        s.ClassOfService.String(),
		SoftwareUpdateState:   s.SoftwareUpdateState.String(),
		DisablementCode:       s.DisablementCode.String(),
	}

	// Obstruction stats
	if obs := s.ObstructionStats; obs != nil {
		status.ObstructionFraction = float64(obs.FractionObstructed)
		status.CurrentlyObstructed = obs.CurrentlyObstructed
		status.AvgProlongedObstructionDurationS = float64(obs.AvgProlongedObstructionDurationS)
		status.AvgProlongedObstructionIntervalS = float64(obs.AvgProlongedObstructionIntervalS)
	}

	// Alerts
	if a := s.Alerts; a != nil {
		status.AlertThermalThrottle = a.ThermalThrottle
		status.AlertThermalShutdown = a.ThermalShutdown
		status.AlertIsHeating = a.IsHeating
		status.AlertSlowEthernet = a.SlowEthernetSpeeds
		status.AlertPowerSaveIdle = a.IsPowerSaveIdle
		status.AlertMotorsStuck = a.MotorsStuck
		status.AlertNoEthernetLink = a.NoEthernetLink
		status.AlertUnexpectedLocation = a.UnexpectedLocation
		status.AlertRoaming = a.Roaming
		status.AlertMastNotNearVert = a.MastNotNearVertical
		status.AlertInstallPending = a.InstallPending
	}

	// Device info
	if di := s.DeviceInfo; di != nil {
		status.HardwareVersion = di.HardwareVersion
		status.SoftwareVersion = di.SoftwareVersion
		status.CountryCode = di.CountryCode
		status.BootCount = di.Bootcount
	}

	// Device state
	if ds := s.DeviceState; ds != nil {
		status.UptimeS = ds.UptimeS
	}

	// GPS
	if gps := s.GpsStats; gps != nil {
		status.GpsValid = gps.GpsValid
		status.GpsSats = gps.GpsSats
	}

	// Alignment
	if align := s.AlignmentStats; align != nil {
		status.TiltAngleDeg = float64(align.TiltAngleDeg)
		// Prefer alignment stats over top-level fields
		status.BoresightAzimuthDeg = float64(align.BoresightAzimuthDeg)
		status.BoresightElevationDeg = float64(align.BoresightElevationDeg)
		status.AttitudeUncertaintyDeg = float64(align.AttitudeUncertaintyDeg)
		status.AttitudeEstimationState = align.AttitudeEstimationState.String()
		status.DesiredBoresightAzimuthDeg = float64(align.DesiredBoresightAzimuthDeg)
		status.DesiredBoresightElevationDeg = float64(align.DesiredBoresightElevationDeg)
		status.ActuatorState = align.ActuatorState.String()
		status.HasActuators = align.HasActuators.String()
	}

	// Software update
	if su := s.SoftwareUpdateStats; su != nil {
		status.SoftwareUpdateState = su.SoftwareUpdateState.String()
		status.SoftwareUpdateProgress = float64(su.SoftwareUpdateProgress)
	}

	// Outage
	if o := s.Outage; o != nil {
		status.OutageCause = o.Cause.String()
		status.OutageDurationNs = o.DurationNs
	}

	// Get context (satellite/cell info)
	ctxResp, err := c.handle(ctx, &pb.Request{
		Request: &pb.Request_DishGetContext{DishGetContext: &pb.DishGetContextRequest{}},
	})
	if err == nil {
		if dc := ctxResp.GetDishGetContext(); dc != nil {
			status.CellID = dc.CellId
			status.SatelliteID = dc.InitialSatelliteId
			status.GatewayID = dc.InitialGatewayId
			status.OnBackupBeam = dc.OnBackupBeam
		}
	}

	// Get location
	locResp, err := c.handle(ctx, &pb.Request{
		Request: &pb.Request_GetLocation{GetLocation: &pb.GetLocationRequest{}},
	})
	if err == nil {
		if loc := locResp.GetGetLocation(); loc != nil {
			if lla := loc.Lla; lla != nil {
				status.Latitude = lla.Lat
				status.Longitude = lla.Lon
				status.Altitude = lla.Alt
			}
		}
	}

	// Sanitize NaN/Inf values
	sanitizeStatus(status)

	return status, nil
}

// GetObstructionMap retrieves the obstruction map from the dish.
func (c *Client) GetObstructionMap(ctx context.Context) (*ObstructionMap, error) {
	resp, err := c.handle(ctx, &pb.Request{
		Request: &pb.Request_DishGetObstructionMap{DishGetObstructionMap: &pb.DishGetObstructionMapRequest{}},
	})
	if err != nil {
		return nil, fmt.Errorf("get obstruction map: %w", err)
	}

	m := resp.GetDishGetObstructionMap()
	if m == nil {
		return nil, fmt.Errorf("unexpected response type for obstruction map")
	}

	refFrame := m.MapReferenceFrame.String()

	// encoding/json refuses to marshal NaN/Inf, so a single junk cell would
	// fail the whole HTTP response instead of one pixel. Fold those into the
	// dish's own "no data" sentinel (-1).
	snr := make([]float32, len(m.Snr))
	for i, v := range m.Snr {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			snr[i] = -1
			continue
		}
		snr[i] = v
	}

	return &ObstructionMap{
		NumRows:        m.NumRows,
		NumCols:        m.NumCols,
		SNR:            snr,
		MaxThetaDeg:    float32(sanitizeFloat64(float64(m.MaxThetaDeg))),
		ReferenceFrame: refFrame,
	}, nil
}

func sanitizeFloat64(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// clampUnit forces a 0-1 ratio. Starlink emits -1 as "stats not yet
// valid" (early-boot obstruction, idle update progress).
func clampUnit(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// normalizeAzimuth wraps a heading into [0,360). The dish reports boresight
// azimuth signed (-180..180); the panel's compass dial and the FRAME_UT map
// rotation both want a plain compass bearing.
func normalizeAzimuth(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	v = math.Mod(v, 360)
	if v < 0 {
		v += 360
	}
	return v
}

// clampNonNeg keeps a metric at 0 or above. Drop rate and prolonged-outage
// counters can briefly emit small negative numbers from float jitter.
func clampNonNeg(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

func sanitizeStatus(s *StarlinkStatus) {
	s.DownlinkThroughputBps = clampNonNeg(s.DownlinkThroughputBps)
	s.UplinkThroughputBps = clampNonNeg(s.UplinkThroughputBps)
	s.PopPingLatencyMs = clampNonNeg(s.PopPingLatencyMs)
	s.PopPingDropRate = clampUnit(s.PopPingDropRate)
	s.ObstructionFraction = clampUnit(s.ObstructionFraction)
	s.AvgProlongedObstructionDurationS = clampNonNeg(s.AvgProlongedObstructionDurationS)
	s.AvgProlongedObstructionIntervalS = clampNonNeg(s.AvgProlongedObstructionIntervalS)
	s.TiltAngleDeg = sanitizeFloat64(s.TiltAngleDeg)
	s.BoresightAzimuthDeg = normalizeAzimuth(s.BoresightAzimuthDeg)
	s.BoresightElevationDeg = sanitizeFloat64(s.BoresightElevationDeg)
	s.AttitudeUncertaintyDeg = clampNonNeg(s.AttitudeUncertaintyDeg)
	s.DesiredBoresightAzimuthDeg = normalizeAzimuth(s.DesiredBoresightAzimuthDeg)
	s.DesiredBoresightElevationDeg = sanitizeFloat64(s.DesiredBoresightElevationDeg)
	s.SoftwareUpdateProgress = clampUnit(s.SoftwareUpdateProgress)
	s.Latitude = sanitizeFloat64(s.Latitude)
	s.Longitude = sanitizeFloat64(s.Longitude)
	s.Altitude = sanitizeFloat64(s.Altitude)
}
