package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

func liteRecord(ts, up, down int64) *agent.TrafficRecord {
	return &agent.TrafficRecord{
		Timestamp:        ts,
		TotalUplink:      up,
		TotalDownlink:    down,
		UserUplink:       map[string]int64{},
		UserDownlink:     map[string]int64{},
		OutboundUplink:   map[string]int64{},
		OutboundDownlink: map[string]int64{},
		InboundUplink:    map[string]int64{},
		InboundDownlink:  map[string]int64{},
	}
}

func TestIngestTraffic_BooksNodeTotalsAndReturnsNewestStamp(t *testing.T) {
	ts := time.Now().Unix()
	node := &domain.Node{ID: 7, Name: "vps", Kind: domain.NodeKindLite, IsActive: true}
	repo := newFakeStatsNodeRepo(node)
	u := newSyncSingleNodeTestUsecase(repo, nil)

	through, err := u.IngestTraffic(context.Background(), node.ID, []*agent.TrafficRecord{liteRecord(ts, 500, 900)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if through != ts {
		t.Fatalf("through = %d, want %d", through, ts)
	}
	if repo.addNodeTrafficCalls != 1 || repo.addNodeDailyTrafficCalls != 1 {
		t.Fatalf("node totals booked %d/%d times, want 1/1", repo.addNodeTrafficCalls, repo.addNodeDailyTrafficCalls)
	}
}

func TestIngestTraffic_NothingToBookTouchesNothing(t *testing.T) {
	node := &domain.Node{ID: 7, Kind: domain.NodeKindLite}
	repo := newFakeStatsNodeRepo(node)
	u := newSyncSingleNodeTestUsecase(repo, nil)

	through, err := u.IngestTraffic(context.Background(), node.ID, nil, nil)
	if err != nil || through != 0 {
		t.Fatalf("through=%d err=%v, want 0 and no error", through, err)
	}
	if repo.addNodeTrafficCalls != 0 {
		t.Fatal("an empty batch must not write")
	}
}

func TestIngestTraffic_RejectsARecordWithoutATimestamp(t *testing.T) {
	node := &domain.Node{ID: 7, Kind: domain.NodeKindLite}
	repo := newFakeStatsNodeRepo(node)
	u := newSyncSingleNodeTestUsecase(repo, nil)

	if _, err := u.IngestTraffic(context.Background(), node.ID, []*agent.TrafficRecord{liteRecord(0, 1, 1)}, nil); err == nil {
		t.Fatal("a record with no timestamp must be refused, or the delta could never be acknowledged")
	}
	if repo.addNodeTrafficCalls != 0 {
		t.Fatal("a refused batch must not write")
	}
}

// A scheduled sweep and a manual ingestion of one node must queue, never overlap.
func TestIngestTraffic_WaitsForTheNodesStatsGate(t *testing.T) {
	node := &domain.Node{ID: 7, Kind: domain.NodeKindLite}
	repo := newFakeStatsNodeRepo(node)
	u := newSyncSingleNodeTestUsecase(repo, nil)

	release, err := u.acquireStatsSync(context.Background(), node.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := u.IngestTraffic(ctx, node.ID, []*agent.TrafficRecord{liteRecord(time.Now().Unix(), 1, 1)}, nil); err == nil {
		t.Fatal("ingestion ran while another reader held the gate")
	}
	if repo.addNodeTrafficCalls != 0 {
		t.Fatal("nothing may be booked when the gate was not obtained")
	}
}

func TestWithoutLiteKeepsOnlyAgentNodes(t *testing.T) {
	full := &domain.Node{ID: 1}
	lite := &domain.Node{ID: 2, Kind: domain.NodeKindLite}
	got := withoutLite([]*domain.Node{full, lite})
	if len(got) != 1 || got[0] != full {
		t.Fatalf("got %+v, want only the full node", got)
	}
}

// A caller's own bookkeeping (an MTProto node's usage baseline) lands with the
// booking or not at all, so a failed save can never count the bytes twice.
func TestIngestTraffic_HookFailureRollsTheBookingBack(t *testing.T) {
	db, u, node, _ := setupTrafficIntegration(t, 1)
	rec := liteRecord(time.Now().Unix(), 100, 200)
	rec.UserUplink["user_1"], rec.UserDownlink["user_1"] = 100, 200
	failed := errors.New("baseline save failed")
	if _, err := u.IngestTraffic(context.Background(), node.ID, []*agent.TrafficRecord{rec}, func(context.Context) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("err = %v, want the hook's error", err)
	}
	var used int64
	db.Table("subscriptions").Select("COALESCE(SUM(data_used),0)").Scan(&used)
	if used != 0 {
		t.Fatalf("a rolled-back batch left %d bytes booked", used)
	}
	ran := false
	if _, err := u.IngestTraffic(context.Background(), node.ID, []*agent.TrafficRecord{rec}, func(ctx context.Context) error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("err=%v ran=%v", err, ran)
	}
	db.Table("subscriptions").Select("COALESCE(SUM(data_used),0)").Scan(&used)
	if used != 300 {
		t.Fatalf("booked %d, want 300", used)
	}
}

// statRepo keeps the samples an MTProto node records.
type statRepo struct {
	repository.NodeRepository // nil: anything else panics
	got                       []*domain.NodeStat
}

func (r *statRepo) CreateNodeStat(_ context.Context, s *domain.NodeStat) error {
	r.got = append(r.got, s)
	return nil
}

// The series query bounds in UTC and SQLite compares times as text, so a sample must land in UTC.
func TestRecordNodeStat_SavesTheSampleInUTC(t *testing.T) {
	repo := &statRepo{}
	u := &nodeUsecase{nodeRepo: repo}
	local := time.Date(2026, 10, 2, 13, 30, 0, 0, time.FixedZone("IRST", 3*3600+1800))
	if err := u.RecordNodeStat(context.Background(), &domain.NodeStat{NodeID: 7, UpSpeed: 100, DownSpeed: 900, TcpCount: 12, OnlineUsers: 3, CreatedAt: local}); err != nil {
		t.Fatal(err)
	}
	if err := u.RecordNodeStat(context.Background(), &domain.NodeStat{NodeID: 7}); err != nil {
		t.Fatal(err)
	}
	if len(repo.got) != 2 {
		t.Fatalf("%d samples saved, want 2", len(repo.got))
	}
	s := repo.got[0]
	if s.CreatedAt.Location() != time.UTC || !s.CreatedAt.Equal(local) || s.UpSpeed != 100 || s.DownSpeed != 900 || s.TcpCount != 12 || s.OnlineUsers != 3 {
		t.Fatalf("saved %+v", s)
	}
	if at := repo.got[1].CreatedAt; at.IsZero() || at.Location() != time.UTC || time.Since(at) > time.Minute {
		t.Fatalf("an unstamped sample got %v, want now in UTC", at)
	}
}
