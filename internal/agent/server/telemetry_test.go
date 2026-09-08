package server

import (
	"context"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/process"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/stats"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/telemetry"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/traffic"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

func TestBufferedTrafficDoesNotWaitForSlowTelemetry(t *testing.T) {
	sysStarted, ipsStarted := make(chan struct{}), make(chan struct{})
	sys := telemetry.NewSampler(func(ctx context.Context) (*stats.SystemStats, error) {
		close(sysStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}, time.Hour, time.Minute, time.Minute)
	ips := telemetry.NewSampler(func(ctx context.Context) (map[string]map[string]int64, error) {
		close(ipsStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}, time.Hour, time.Minute, time.Minute)
	sys.Start()
	ips.Start()
	defer sys.Stop()
	defer ips.Stop()
	<-sysStarted
	<-ipsStarted
	store := traffic.NewMemoryStore(time.Hour, 24*time.Hour)
	store.Accumulate(&traffic.XrayStatsSnapshot{UserUplink: map[string]int64{"alice": 123}, TotalUplink: 123}, time.Now())
	s := &Server{
		statsCollect: sys, onlineIPs: ips, trafficStore: store,
		xrayVersion: "test", xrayMgr: process.NewXrayManager(process.Config{ConfigPath: t.TempDir() + "/missing.json"}),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := s.GetSystemStats(context.Background(), &pb.Empty{}); err == nil {
			t.Error("unavailable stats became empty data")
		}
		if _, err := s.GetAllUsersOnlineIPs(context.Background(), &pb.Empty{}); err == nil {
			t.Error("unavailable online sample became empty data")
		}
		resp, err := s.GetBufferedTraffic(context.Background(), &pb.Empty{})
		if err != nil {
			t.Error(err)
			return
		}
		if resp == nil || len(resp.Records) != 1 || resp.Records[0].TotalUplink != 123 {
			t.Error("traffic not returned independently")
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("traffic bundle blocked on telemetry")
	}
}

func TestCachedTelemetryResponsesHaveTimestampAndIsolatedMaps(t *testing.T) {
	sys := telemetry.NewSampler(func(context.Context) (*stats.SystemStats, error) {
		return &stats.SystemStats{CPUUsagePercent: 10, CPUPerCore: []float64{11}}, nil
	}, time.Hour, time.Second, time.Minute)
	ips := telemetry.NewSampler(func(context.Context) (map[string]map[string]int64, error) {
		return map[string]map[string]int64{"alice": {"192.0.2.1": 123}, "empty": {}}, nil
	}, time.Hour, time.Second, time.Minute)
	sys.Start()
	ips.Start()
	defer sys.Stop()
	defer ips.Stop()
	s := &Server{statsCollect: sys, onlineIPs: ips}
	deadline := time.After(time.Second)
	for {
		_, a := s.GetSystemStats(context.Background(), &pb.Empty{})
		_, b := s.GetAllUsersOnlineIPs(context.Background(), &pb.Empty{})
		if a == nil && b == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("snapshots not ready")
		case <-time.After(time.Millisecond):
		}
	}
	a, _ := s.GetSystemStats(context.Background(), &pb.Empty{})
	b, _ := s.GetAllUsersOnlineIPs(context.Background(), &pb.Empty{})
	if a.CollectedAtUnixMs <= 0 || b.CollectedAtUnixMs <= 0 {
		t.Fatal("snapshot age not exposed")
	}
	a.CpuPerCore[0] = 99
	b.Users["alice"].Ips["192.0.2.1"] = 99
	c, _ := s.GetSystemStats(context.Background(), &pb.Empty{})
	d, _ := s.GetAllUsersOnlineIPs(context.Background(), &pb.Empty{})
	if c.CpuPerCore[0] != 11 || d.Users["alice"].Ips["192.0.2.1"] != 123 {
		t.Fatal("response mutation changed shared snapshot")
	}
	if c.CollectedAtUnixMs != a.CollectedAtUnixMs || d.CollectedAtUnixMs != b.CollectedAtUnixMs {
		t.Fatal("cached sample restamped on read")
	}
	if d.Users["empty"] == nil {
		t.Fatal("known empty user disappeared")
	}
}

// StartLocal is the production lifecycle in NASNET: no remote agent listener
// is started, but collectors must still start and be released by Stop.
func TestEmbeddedLifecycleStartsAndStopsTelemetry(t *testing.T) {
	systemStarted, onlineStarted := make(chan struct{}), make(chan struct{})
	sys := telemetry.NewSampler(func(ctx context.Context) (*stats.SystemStats, error) {
		close(systemStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}, time.Hour, time.Minute, time.Minute)
	ips := telemetry.NewSampler(func(ctx context.Context) (map[string]map[string]int64, error) {
		close(onlineStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}, time.Hour, time.Minute, time.Minute)
	s := &Server{statsCollect: sys, onlineIPs: ips,
		xrayMgr: process.NewXrayManager(process.Config{ConfigPath: t.TempDir() + "/missing.json"}),
	}
	t.Cleanup(func() { sys.Stop(); ips.Stop() })
	if err := s.StartLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, started := range []<-chan struct{}{systemStarted, onlineStarted} {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("embedded server did not start telemetry")
		}
	}
	// This fixture did not start an Xray process; exclude process shutdown.
	s.xrayMgr = nil
	stopped := make(chan struct{})
	go func() { s.Stop(context.Background()); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("embedded shutdown did not cancel telemetry")
	}
	if _, err := s.GetSystemStats(context.Background(), &pb.Empty{}); err == nil {
		t.Fatal("stopped system sampler returned data")
	}
	if _, err := s.GetAllUsersOnlineIPs(context.Background(), &pb.Empty{}); err == nil {
		t.Fatal("stopped online sampler returned data")
	}
}
