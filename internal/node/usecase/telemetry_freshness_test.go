package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

type legacyOnlineSnapshotClient struct {
	agent.NodeClient
	users map[string]map[string]int64
}

func (c legacyOnlineSnapshotClient) GetAllUsersOnlineIPs(context.Context) (map[string]map[string]int64, error) {
	return c.users, nil
}

type timestampedOnlineSnapshotClient struct {
	legacyOnlineSnapshotClient
	snapshot *agent.OnlineIPSnapshot
}

func (c timestampedOnlineSnapshotClient) GetOnlineIPSnapshot(context.Context) (*agent.OnlineIPSnapshot, error) {
	return c.snapshot, nil
}

func TestOnlineSnapshotClientCompatibility(t *testing.T) {
	users := map[string]map[string]int64{"a": {"ip": 1}}
	legacy := legacyOnlineSnapshotClient{users: users}
	snapshot, err := getOnlineIPSnapshot(context.Background(), legacy)
	if err != nil || snapshot.CollectedAtUnixMs != 0 || snapshot.Users["a"]["ip"] != 1 {
		t.Fatalf("legacy snapshot: %+v %v", snapshot, err)
	}
	want := &agent.OnlineIPSnapshot{Users: users, CollectedAtUnixMs: 1234}
	snapshot, err = getOnlineIPSnapshot(context.Background(), timestampedOnlineSnapshotClient{snapshot: want})
	if err != nil || snapshot != want {
		t.Fatal("timestamped snapshot was discarded")
	}
	snapshot, err = getOnlineIPSnapshot(context.Background(), legacyOnlineSnapshotClient{})
	if err != nil || snapshot.Users != nil {
		t.Fatal("missing legacy data became a completed empty map")
	}
}

func TestTelemetrySampleTimeUsesSourceWithLegacyAndClockFallback(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	at := now.Add(-5 * time.Second)
	if got := telemetrySampleTime(at.UnixMilli(), now); !got.Equal(at) {
		t.Fatal("source time lost")
	}
	for _, stamp := range []int64{0, -1, now.Add(time.Hour).UnixMilli()} {
		if got := telemetrySampleTime(stamp, now); !got.Equal(now) {
			t.Fatalf("timestamp %d not clamped/fallback: %v", stamp, got)
		}
	}
}
