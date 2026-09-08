package xray

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ss "github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/status"
)

type countingListener struct {
	net.Listener
	accepted atomic.Int32
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.accepted.Add(1)
	}
	return c, err
}

type onlineStatsServer struct {
	ss.UnimplementedStatsServiceServer
	users               int
	ip                  string
	entered             chan struct{}
	release             <-chan struct{}
	fail                atomic.Bool
	active, peak, calls atomic.Int32
}

func (s *onlineStatsServer) QueryStats(context.Context, *ss.QueryStatsRequest) (*ss.QueryStatsResponse, error) {
	resp := &ss.QueryStatsResponse{}
	for i := range s.users {
		resp.Stat = append(resp.Stat, &ss.Stat{Name: fmt.Sprintf("user>>>user%d>>>online", i), Value: 1})
	}
	return resp, nil
}

func (s *onlineStatsServer) GetStatsOnlineIpList(ctx context.Context, req *ss.GetStatsRequest) (*ss.GetStatsOnlineIpListResponse, error) {
	s.calls.Add(1)
	n := s.active.Add(1)
	defer s.active.Add(-1)
	for old := s.peak.Load(); n > old && !s.peak.CompareAndSwap(old, n); old = s.peak.Load() {
	}
	if s.entered != nil {
		s.entered <- struct{}{}
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.fail.Load() && strings.Contains(req.Name, ">>>user0>>>") {
		return nil, status.Error(codes.Unavailable, "restart")
	}
	ips := map[string]int64{s.ip: 123}
	if strings.Contains(req.Name, fmt.Sprintf(">>>user%d>>>", s.users-1)) {
		ips = nil
	}
	return &ss.GetStatsOnlineIpListResponse{Ips: ips}, nil
}

func startStatsServer(t *testing.T, addr string, service *onlineStatsServer) (*grpc.Server, *countingListener) {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	listener := &countingListener{Listener: l}
	server := grpc.NewServer()
	ss.RegisterStatsServiceServer(server, service)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	return server, listener
}

func TestOnlineSweepReusesOneConnectionAndBoundsConcurrency(t *testing.T) {
	release := make(chan struct{})
	service := &onlineStatsServer{users: 1000, ip: "192.0.2.1", entered: make(chan struct{}, 1000), release: release}
	_, listener := startStatsServer(t, "127.0.0.1:0", service)
	c := NewLocalClient(listener.Addr().String(), time.Second)
	defer c.Close()
	result := make(chan map[string]map[string]int64, 1)
	errs := make(chan error, 1)
	go func() { users, err := c.CollectOnlineIPs(context.Background()); result <- users; errs <- err }()
	for range onlineIPConcurrency {
		select {
		case <-service.entered:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("workers did not run concurrently")
		}
	}
	if service.active.Load() != onlineIPConcurrency {
		t.Errorf("active workers: %d", service.active.Load())
	}
	close(release)
	users := <-result
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if len(users) != 1000 || users["user0"]["192.0.2.1"] != 123 {
		t.Fatalf("incomplete sweep: %d", len(users))
	}
	if users["user999"] == nil || len(users["user999"]) != 0 {
		t.Fatal("known empty user became missing")
	}
	if service.peak.Load() > onlineIPConcurrency || service.peak.Load() < 2 {
		t.Fatalf("peak concurrency %d", service.peak.Load())
	}
	if listener.accepted.Load() != 1 {
		t.Fatalf("1000 users created %d connections", listener.accepted.Load())
	}
	if _, err := c.GetAllOnlineUsers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if listener.accepted.Load() != 1 {
		t.Fatal("subsequent call did not reuse connection")
	}
}

func TestOnlineSweepNeverReturnsPartialDataOnFailure(t *testing.T) {
	service := &onlineStatsServer{users: 30, ip: "192.0.2.1"}
	service.fail.Store(true)
	_, listener := startStatsServer(t, "127.0.0.1:0", service)
	c := NewLocalClient(listener.Addr().String(), time.Second)
	defer c.Close()
	if users, err := c.CollectOnlineIPs(context.Background()); err == nil || users != nil {
		t.Fatal("partial failure published a snapshot")
	}
	service.fail.Store(false)
	if users, err := c.CollectOnlineIPs(context.Background()); err != nil || len(users) != 30 {
		t.Fatalf("recovery: %d %v", len(users), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if users, err := c.CollectOnlineIPs(ctx); !errors.Is(err, context.Canceled) || users != nil {
		t.Fatalf("cancelled sweep: %v", err)
	}
}

func TestSharedConnectionReconnectsAfterRestartAndAddressChange(t *testing.T) {
	service := &onlineStatsServer{users: 2, ip: "192.0.2.1"}
	server, listener := startStatsServer(t, "127.0.0.1:0", service)
	addr := listener.Addr().String()
	c := NewLocalClient(addr, 500*time.Millisecond)
	defer c.Close()
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn := c.conn
	server.Stop()
	_, _ = startStatsServer(t, addr, service)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		ips, err := c.GetUserOnlineIPs(ctx, "user0")
		if err == nil && ips["192.0.2.1"] == 123 {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("reconnect: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c.conn != conn {
		t.Fatal("restart replaced shared client connection")
	}
	_, next := startStatsServer(t, "127.0.0.1:0", &onlineStatsServer{users: 2, ip: "192.0.2.2"})
	c.SetAddress(next.Addr().String())
	if conn.GetState() != connectivity.Shutdown {
		t.Fatal("old address connection leaked")
	}
	ips, err := c.GetUserOnlineIPs(context.Background(), "user0")
	if err != nil || ips["192.0.2.2"] != 123 {
		t.Fatalf("new endpoint: %v %v", ips, err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("closed client reopened")
	}
}

func TestConcurrentClientUseAndAddressChanges(t *testing.T) {
	_, a := startStatsServer(t, "127.0.0.1:0", &onlineStatsServer{users: 2})
	_, b := startStatsServer(t, "127.0.0.1:0", &onlineStatsServer{users: 2})
	c := NewLocalClient(a.Addr().String(), 50*time.Millisecond)
	defer c.Close()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 20 {
				_, _ = c.GetAllOnlineUsers(context.Background())
			}
		})
	}
	wg.Go(func() {
		for range 20 {
			c.SetAddress(b.Addr().String())
			c.SetAddress(a.Addr().String())
		}
	})
	wg.Wait()
}
