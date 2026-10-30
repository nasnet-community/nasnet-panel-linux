// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

func TestTheFirstStartMakesTheCAAndKeepsItInTheState(t *testing.T) {
	cfg, log := baseConfig(t)
	m, err := New(cfg)
	must(t, err)

	ca := m.CA()
	if ca == nil || m.CAError() != nil {
		t.Fatalf("CA = %v, error %v", ca, m.CAError())
	}
	st := cfg.Store.Read()
	if st.CACert == "" || st.CAKey == "" || cfg.Store.Writes() != 1 {
		t.Fatalf("the state holds cert %d bytes, key %d bytes after %d writes", len(st.CACert), len(st.CAKey), cfg.Store.Writes())
	}
	// what the state holds is what the manager uses
	loaded, err := pki.Load([]byte(st.CACert), []byte(st.CAKey))
	must(t, err)
	if !loaded.Certificate().Equal(ca.Certificate()) {
		t.Fatal("the manager's CA is not the one in the state")
	}
	if log.count("made the CA") != 1 {
		t.Errorf("the log never said it made one:\n%s", log)
	}
	// it signs, for the hub's own certificate and a node's
	if _, err := ca.IssueHubClientCert(cfg.Now()); err != nil {
		t.Errorf("the new CA can't sign: %v", err)
	}
	if strings.Contains(log.String(), "BEGIN") {
		t.Errorf("the log has PEM in it:\n%s", log)
	}
}

func TestALaterStartLoadsTheSameCAAndWritesNothing(t *testing.T) {
	cfg, log := baseConfig(t)
	first, err := New(cfg)
	must(t, err)
	writes := cfg.Store.Writes()

	second, err := New(cfg)
	must(t, err)
	if !second.CA().Certificate().Equal(first.CA().Certificate()) {
		t.Fatal("a second start made another CA")
	}
	if cfg.Store.Writes() != writes {
		t.Errorf("the state was written %d more times", cfg.Store.Writes()-writes)
	}
	if log.count("made the CA") != 1 {
		t.Errorf("the log says it made a CA more than once:\n%s", log)
	}
}

func TestACAThatHasRunOutIsReportedAndLeftAlone(t *testing.T) {
	cfg, log := baseConfig(t)
	// made eleven years ago, and good for ten
	old := newCA(t, epoch.AddDate(-11, 0, 0))
	setCA(t, cfg.Store, old)
	writes := cfg.Store.Writes()

	m, err := New(cfg)
	must(t, err)
	if m.CA() != nil || !errors.Is(m.CAError(), pki.ErrCAExpired) {
		t.Fatalf("CA = %v, error %v", m.CA(), m.CAError())
	}
	// no other CA is made in its place: every node trusts this one
	if st := cfg.Store.Read(); st.CACert != string(old.CertPEM()) || cfg.Store.Writes() != writes {
		t.Error("the expired CA was replaced or the state written")
	}
	if log.count("can't be used", "expired") != 1 {
		t.Errorf("the log doesn't report it:\n%s", log)
	}
	// said once, not at every look
	m.Sync()
	m.Sync()
	if log.count("can't be used") != 1 {
		t.Errorf("the problem was logged again:\n%s", log)
	}
}

func TestACAThatIsNotYetValidMeansTheClockIsWrong(t *testing.T) {
	cfg, _ := baseConfig(t)
	setCA(t, cfg.Store, newCA(t, epoch.AddDate(1, 0, 0)))
	m, err := New(cfg)
	must(t, err)
	if m.CA() != nil || !errors.Is(m.CAError(), pki.ErrCANotYetValid) {
		t.Fatalf("CA = %v, error %v", m.CA(), m.CAError())
	}
}

func TestACAThatWillNotLoadIsReported(t *testing.T) {
	cfg, log := baseConfig(t)
	ms := &memStore{st: model.NewState()}
	ms.st.CACert, ms.st.CAKey = "not a certificate", "not a key"
	cfg.Store = ms
	m, err := New(cfg)
	must(t, err)
	if m.CA() != nil || !errors.Is(m.CAError(), pki.ErrBadCA) {
		t.Fatalf("CA = %v, error %v", m.CA(), m.CAError())
	}
	if ms.Writes() != 0 {
		t.Error("a CA that wouldn't load was written over")
	}
	// the log gives the cause and never the text it was given
	if log.count("can't be used") != 1 || strings.Contains(log.String(), "not a key") {
		t.Errorf("log:\n%s", log)
	}
}

// A router that has just booted can think it is 1970. A CA made then would be
// over by the time the clock was right, so none is made until it is.
func TestNoCAIsMadeWhileTheClockIsWrongAndOneIsOnceItIsRight(t *testing.T) {
	cfg, log := baseConfig(t)
	clk := newClock(time.Unix(0, 0))
	cfg.Now = clk.Now
	m, err := New(cfg)
	must(t, err)
	if m.CA() != nil || m.CAError() == nil || !strings.Contains(m.CAError().Error(), "clock") {
		t.Fatalf("CA = %v, error %v", m.CA(), m.CAError())
	}
	if st := cfg.Store.Read(); st.CACert != "" || cfg.Store.Writes() != 0 {
		t.Fatal("a CA was made with the clock at 1970")
	}
	if log.count("1970-01-01") != 1 {
		t.Errorf("the log doesn't give the date it saw:\n%s", log)
	}

	clk.Set(epoch)
	m.Sync()
	if m.CA() == nil || m.CAError() != nil {
		t.Fatalf("after the clock was set: CA = %v, error %v", m.CA(), m.CAError())
	}
	// valid from the time it was made, not from 1970
	if nb := m.CA().Certificate().NotBefore; nb.Before(epoch.Add(-2 * time.Hour)) {
		t.Errorf("the CA starts %v", nb)
	}
}

// A backup restored onto an install with no nodes swaps the CA. The manager
// picks the other one up at its next look.
func TestACAThatIsReplacedInTheStateIsPickedUp(t *testing.T) {
	cfg, log := baseConfig(t)
	m, err := New(cfg)
	must(t, err)
	before := m.CA().Certificate()

	other := newCA(t, epoch)
	setCA(t, cfg.Store, other)
	m.Sync()
	if got := m.CA().Certificate(); got.Equal(before) || !got.Equal(other.Certificate()) {
		t.Fatal("the manager still uses the old CA")
	}
	if log.count("the CA is in use", "replaced=true") != 1 {
		t.Errorf("the log doesn't say it changed:\n%s", log)
	}
}

// A CA that gets fixed is used from then on: the problem is not remembered.
func TestAFixedCAIsUsedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, _ := baseConfig(t)
		// valid from two hours on
		setCA(t, cfg.Store, newCA(t, epoch.Add(3*time.Hour)))
		m, err := New(cfg)
		must(t, err)
		if m.CA() != nil {
			t.Fatal("a CA that is not valid yet is in use")
		}
		go m.Run(context.Background())
		time.Sleep(150 * time.Minute)
		synctest.Wait()
		if m.CA() == nil || m.CAError() != nil {
			t.Fatalf("CA = %v, error %v", m.CA(), m.CAError())
		}
		must(t, m.Close(context.Background()))
	})
}
