// Package telemetry keeps slow agent collection outside RPC request lifetimes.
package telemetry

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrUnavailable = errors.New("telemetry snapshot not ready or expired")

// Sampler publishes immutable, completed samples. Readers never initiate or wait
// for collection. A failed or cancelled collection cannot publish partial data.
// Values returned by Snapshot must not be mutated by callers.
type Sampler[T any] struct {
	collect                   func(context.Context) (T, error)
	interval, timeout, maxAge time.Duration
	mu                        sync.RWMutex
	value                     T
	at                        time.Time
	generation                uint64
	cancel                    context.CancelFunc
	done                      chan struct{}
	wake                      chan struct{}
	stopped                   bool
}

func NewSampler[T any](collect func(context.Context) (T, error), interval, timeout, maxAge time.Duration) *Sampler[T] {
	if interval <= 0 || timeout <= 0 || maxAge <= 0 {
		panic("telemetry: durations must be positive")
	}
	return &Sampler[T]{collect: collect, interval: interval, timeout: timeout, maxAge: maxAge, wake: make(chan struct{}, 1)}
}

// Start is idempotent. The first sample runs immediately in the background.
func (s *Sampler[T]) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil || s.stopped {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	go s.run(ctx)
}

func (s *Sampler[T]) run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.mu.RLock()
		generation := s.generation
		s.mu.RUnlock()
		started := time.Now()
		sampleCtx, cancel := context.WithTimeout(ctx, s.timeout)
		value, err := s.collect(sampleCtx)
		valid := err == nil && sampleCtx.Err() == nil
		cancel()
		if valid {
			s.mu.Lock()
			if generation == s.generation && !s.stopped {
				s.value, s.at = value, started
			}
			s.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}

func (s *Sampler[T]) Snapshot(ctx context.Context) (T, time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, time.Time{}, err
	}
	if s.stopped || s.at.IsZero() || time.Since(s.at) > s.maxAge {
		return zero, time.Time{}, ErrUnavailable
	}
	return s.value, s.at, nil
}

// Invalidate discards the old endpoint's data and rejects its in-flight sample.
func (s *Sampler[T]) Invalidate() {
	s.mu.Lock()
	s.generation++
	var zero T
	s.value, s.at = zero, time.Time{}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Sampler[T]) Stop() {
	s.mu.Lock()
	s.stopped = true
	cancel, done := s.cancel, s.done
	var zero T
	s.value, s.at = zero, time.Time{}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
