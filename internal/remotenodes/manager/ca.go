// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

// clockFloor is the earliest date a router's clock may show for a CA to be
// made. A router that has only just booted can think it is 1970, and a CA
// made then would be over before the clock was set right: nodes would trust
// it, and the hub could not use it.
var clockFloor = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// CA is the plugin's certificate authority, which signs every node's
// certificate and the hub's own. It is nil while the CA can't be used; see
// CAError. The returned value is shared and safe for concurrent use.
//
// It is the same value from one look to the next and another when the CA is
// replaced, as a restored backup does. Whatever is made from it, such as the
// hub's own certificate, is good for as long as CA returns what it was made
// from, and has to be made again when it doesn't.
func (m *Manager) CA() *pki.CA {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ca
}

// CAError is why the CA can't be used, or nil when it can: the state has none
// and the clock is too far off to make one, it won't load, or it has run out
// (or isn't valid yet). While it is set no node is contacted, and every Sync
// looks again, so fixing the clock or restoring a backup is enough.
func (m *Manager) CAError() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.caErr
}

// checkCA makes sure the state has a CA that can be used, making one the first
// time, and reports whether another CA has taken the place of the one in use.
func (m *Manager) checkCA() (replaced bool) {
	st := m.cfg.Store.Read()
	made := false
	if st.CACert == "" && st.CAKey == "" {
		if err := m.makeCA(); err != nil {
			m.setCA(nil, "", err, false)
			return false
		}
		made = true
		st = m.cfg.Store.Read()
	}
	ca, err := pki.Load([]byte(st.CACert), []byte(st.CAKey))
	if err == nil {
		err = validAt(ca, m.now())
	}
	if err != nil {
		ca = nil
	}
	return m.setCA(ca, st.CACert, err, made)
}

// makeCA puts a new CA into the state, unless the state got one in the
// meantime (a backup restored, say), which stays.
func (m *Manager) makeCA() error {
	now := m.now()
	if now.Before(clockFloor) {
		return fmt.Errorf("the clock reads %s, which can't be right, so no CA is made yet", now.UTC().Format(time.DateOnly))
	}
	ca, err := pki.NewCA(now)
	if err != nil {
		return fmt.Errorf("making the CA: %w", err)
	}
	return m.cfg.Store.Update(func(s *model.State) error {
		if s.CACert != "" || s.CAKey != "" {
			return nil
		}
		s.CACert, s.CAKey = string(ca.CertPEM()), string(ca.KeyPEM())
		return nil
	})
}

// validAt is whether the CA may sign at the given time. pki.Load leaves that
// to the issuing calls, which refuse it too; this finds out early, before the
// manager has a node to refuse.
func validAt(ca *pki.CA, now time.Time) error {
	c := ca.Certificate()
	switch {
	case now.Before(c.NotBefore):
		return fmt.Errorf("%w (it starts %s, the time is %s)", pki.ErrCANotYetValid, c.NotBefore.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	case now.After(c.NotAfter):
		return fmt.Errorf("%w (it ended %s)", pki.ErrCAExpired, c.NotAfter.UTC().Format(time.RFC3339))
	}
	return nil
}

// setCA records what checkCA found and says what changed, once: a problem is
// logged when it first appears or changes, not on every look.
func (m *Manager) setCA(ca *pki.CA, text string, err error, made bool) (replaced bool) {
	m.mu.Lock()
	replaced = ca != nil && m.ca != nil && text != m.caText
	was := m.ca
	if ca != nil && was != nil && !replaced {
		// the same CA read again: keep the value callers hold, so that a new one
		// means the CA changed
		ca = was
	}
	m.ca, m.caErr = ca, err
	if ca != nil {
		m.caText = text
	}
	said := m.caSaid
	if err != nil {
		m.caSaid = err.Error()
	} else {
		m.caSaid = ""
	}
	m.mu.Unlock()

	switch {
	case err != nil && err.Error() != said:
		m.log.Error("the CA can't be used, so no node is contacted until it is fixed", "err", err)
	case err == nil && made:
		m.log.Info("made the CA that signs the nodes' certificates", "expires", ca.Certificate().NotAfter.UTC().Format(time.DateOnly))
	case err == nil && (was == nil || replaced):
		m.log.Info("the CA is in use", "expires", ca.Certificate().NotAfter.UTC().Format(time.DateOnly), "replaced", replaced)
	}
	return replaced
}
