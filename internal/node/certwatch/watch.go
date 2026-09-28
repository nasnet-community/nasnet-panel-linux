// Package certwatch records which certificate each node presents to the hub
// and, in strict mode, refuses revoked or misplaced ones.
package certwatch

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// persistEvery bounds how often an unchanged sighting is rewritten.
const persistEvery = 10 * time.Minute

// ErrRevoked marks a refused revoked certificate.
var ErrRevoked = errors.New("certificate has been revoked")

type Store interface {
	ListSightings(ctx context.Context) ([]*domain.NodeCertSighting, error)
	SaveSighting(ctx context.Context, s *domain.NodeCertSighting) error
	ListRevokedSerialNumbers(ctx context.Context) ([]string, error)
}

type Watch struct {
	store  Store
	strict atomic.Bool

	mu       sync.RWMutex
	seen     map[uint]domain.NodeCertSighting
	savedAt  map[uint]time.Time
	revoked  map[string]struct{}
	onChange func(nodeID uint, serial string)
}

func New(store Store) *Watch {
	return &Watch{
		store:   store,
		seen:    make(map[uint]domain.NodeCertSighting),
		savedAt: make(map[uint]time.Time),
		revoked: make(map[string]struct{}),
	}
}

// Load restores persisted sightings and the revoked set.
func (w *Watch) Load(ctx context.Context) error {
	rows, err := w.store.ListSightings(ctx)
	if err != nil {
		return fmt.Errorf("load sightings: %w", err)
	}
	w.mu.Lock()
	for _, r := range rows {
		w.seen[r.NodeID] = *r
		w.savedAt[r.NodeID] = r.SeenAt
	}
	w.mu.Unlock()
	return w.RefreshRevoked(ctx)
}

func (w *Watch) RefreshRevoked(ctx context.Context) error {
	serials, err := w.store.ListRevokedSerialNumbers(ctx)
	if err != nil {
		return fmt.Errorf("load revoked serials: %w", err)
	}
	set := make(map[string]struct{}, len(serials))
	for _, s := range serials {
		set[s] = struct{}{}
	}
	w.mu.Lock()
	w.revoked = set
	w.mu.Unlock()
	return nil
}

func (w *Watch) SetStrict(on bool) { w.strict.Store(on) }
func (w *Watch) Strict() bool      { return w.strict.Load() }

// SetOnChange registers a hook fired (in its own goroutine) when a node
// presents a different serial than last seen. Set it before traffic flows.
func (w *Watch) SetOnChange(fn func(nodeID uint, serial string)) { w.onChange = fn }

// Observe records the certificate a node presented.
func (w *Watch) Observe(nodeID uint, mode string, leaf *x509.Certificate) {
	if leaf == nil || nodeID == 0 {
		return
	}
	now := time.Now()
	s := domain.NodeCertSighting{
		NodeID:     nodeID,
		Serial:     leaf.SerialNumber.String(),
		CommonName: leaf.Subject.CommonName,
		NotAfter:   leaf.NotAfter,
		Mode:       mode,
		SeenAt:     now,
	}
	w.mu.Lock()
	prev, had := w.seen[nodeID]
	changed := !had || prev.Serial != s.Serial
	persist := changed || prev.Mode != mode || now.Sub(w.savedAt[nodeID]) >= persistEvery
	w.seen[nodeID] = s
	if persist {
		w.savedAt[nodeID] = now
	}
	onChange := w.onChange
	w.mu.Unlock()

	if persist {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := w.store.SaveSighting(ctx, &s); err != nil {
				logger.GetLogger().WithError(err).Warn("certwatch: failed to save sighting")
			}
		}()
	}
	if changed && onChange != nil {
		go onChange(nodeID, s.Serial)
	}
}

// Check applies strict mode: a revoked certificate, or one whose node ID
// differs from the node it connects as, is refused.
func (w *Watch) Check(nodeID uint, leaf *x509.Certificate) error {
	if !w.Strict() || leaf == nil {
		return nil
	}
	if err := w.CheckRevoked(leaf); err != nil {
		return err
	}
	if id, ok := certs.NodeIDFromCommonName(leaf.Subject.CommonName); !ok || id != nodeID {
		return fmt.Errorf("certificate %q does not belong to node %d", leaf.Subject.CommonName, nodeID)
	}
	return nil
}

// CheckRevoked refuses a revoked serial when strict mode is on.
func (w *Watch) CheckRevoked(leaf *x509.Certificate) error {
	if !w.Strict() || leaf == nil {
		return nil
	}
	if w.IsRevoked(leaf.SerialNumber.String()) {
		return fmt.Errorf("%w: serial %s", ErrRevoked, leaf.SerialNumber)
	}
	return nil
}

// Gate observes, then checks. The TLS hooks call this.
func (w *Watch) Gate(nodeID uint, mode string, leaf *x509.Certificate) error {
	w.Observe(nodeID, mode, leaf)
	return w.Check(nodeID, leaf)
}

func (w *Watch) Sighting(nodeID uint) (domain.NodeCertSighting, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	s, ok := w.seen[nodeID]
	return s, ok
}

// RevokedSightings returns the nodes last seen presenting a revoked cert.
func (w *Watch) RevokedSightings() []domain.NodeCertSighting {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var out []domain.NodeCertSighting
	for _, s := range w.seen {
		if _, bad := w.revoked[s.Serial]; bad {
			out = append(out, s)
		}
	}
	return out
}

func (w *Watch) IsRevoked(serial string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, bad := w.revoked[serial]
	return bad
}
