package telemetry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func awaitSample(t *testing.T, s *Sampler[int], want int) time.Time {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		value, at, err := s.Snapshot(context.Background())
		if err == nil && value == want {
			return at
		}
		select {
		case <-deadline:
			t.Fatalf("sample = %d, %v; want %d", value, err, want)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestSnapshotReadersNeverWaitForOrTriggerCollection(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s := NewSampler(func(ctx context.Context) (int, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return 42, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}, time.Hour, time.Second, time.Minute)
	s.Start()
	s.Start()
	defer s.Stop()
	<-started
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if _, _, err := s.Snapshot(context.Background()); !errors.Is(err, ErrUnavailable) {
				t.Errorf("startup: %v", err)
			}
		})
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readers waited for collection")
	}
	close(release)
	at := awaitSample(t, s, 42)
	for range 1000 {
		value, gotAt, err := s.Snapshot(context.Background())
		if err != nil || value != 42 || !gotAt.Equal(at) {
			t.Fatal("read did not reuse sample")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("readers initiated %d collections", calls.Load())
	}
}

func TestFailedRefreshRetainsOnlyFreshCompletedData(t *testing.T) {
	var calls atomic.Int32
	failed := make(chan struct{})
	s := NewSampler(func(context.Context) (int, error) {
		if calls.Add(1) == 1 {
			return 7, nil
		}
		close(failed)
		return 99, errors.New("partial collection")
	}, time.Hour, time.Second, time.Minute)
	s.Start()
	defer s.Stop()
	at := awaitSample(t, s, 7)
	s.wake <- struct{}{}
	<-failed
	if value, gotAt, err := s.Snapshot(context.Background()); err != nil || value != 7 || !at.Equal(gotAt) {
		t.Fatal("failure overwrote completed sample")
	}
	s.mu.Lock()
	s.at = time.Now().Add(-2 * time.Minute)
	s.mu.Unlock()
	if _, _, err := s.Snapshot(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("expired data returned as fresh")
	}
}

func TestInvalidateRejectsInflightOldEndpointSample(t *testing.T) {
	started, release, next := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s := NewSampler(func(ctx context.Context) (int, error) {
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
				return 1, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		close(next)
		<-ctx.Done()
		return 0, ctx.Err()
	}, time.Hour, time.Second, time.Minute)
	s.Start()
	defer s.Stop()
	<-started
	s.Invalidate()
	close(release)
	<-next
	if _, _, err := s.Snapshot(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("old endpoint sample survived invalidation")
	}
}

func TestCancelledSampleCannotPublishAndStopJoinsCollector(t *testing.T) {
	exited := make(chan struct{})
	s := NewSampler(func(ctx context.Context) (int, error) {
		<-ctx.Done()
		close(exited)
		return 5, nil // collector forgot to return context error
	}, time.Hour, 10*time.Millisecond, time.Minute)
	s.Start()
	<-exited
	s.Stop()
	s.Stop()
	s.Start()
	if _, _, err := s.Snapshot(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cancelled/stopped sample published")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.Snapshot(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("reader cancellation ignored")
	}
}

func BenchmarkSnapshot(b *testing.B) {
	s := NewSampler(func(context.Context) (int, error) { return 1, nil }, time.Hour, time.Second, time.Minute)
	s.value, s.at = 1, time.Now()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = s.Snapshot(ctx)
	}
}
