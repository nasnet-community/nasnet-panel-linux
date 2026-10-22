package usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
)

type memSettings struct {
	mu   sync.Mutex
	rows map[string]*settingDomain.Setting
}

func newMemSettings(kv ...string) *memSettings {
	m := &memSettings{rows: map[string]*settingDomain.Setting{}}
	for i := 0; i+1 < len(kv); i += 2 {
		m.rows[kv[i]] = &settingDomain.Setting{Key: kv[i], Value: kv[i+1]}
	}
	return m
}

func (m *memSettings) GetAll(context.Context) ([]*settingDomain.Setting, error) { return nil, nil }

func (m *memSettings) GetByKey(_ context.Context, key string) (*settingDomain.Setting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.rows[key]; ok {
		c := *s
		return &c, nil
	}
	return nil, errors.New("record not found")
}

func (m *memSettings) Update(_ context.Context, s *settingDomain.Setting) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *s
	m.rows[s.Key] = &c
	return nil
}

func (m *memSettings) UpdateMany(ctx context.Context, rows []*settingDomain.Setting) error {
	for _, s := range rows {
		_ = m.Update(ctx, s)
	}
	return nil
}

func (m *memSettings) value(key string) string {
	s, err := m.GetByKey(context.Background(), key)
	if err != nil {
		return ""
	}
	return s.Value
}

func testUsecase(t *testing.T, version string, f Fetcher, settings *memSettings) (*Usecase, events.Subscriber) {
	t.Helper()
	bus := events.NewEventBus()
	u := New(Deps{Paths: Paths{Root: t.TempDir()}, Version: version, Settings: settings, Events: bus})
	u.checker = NewChecker(f, "amd64")
	return u, bus.Subscribe("test")
}

func TestKinds(t *testing.T) {
	root := t.TempDir()
	exe := root + "/bin/nasnet-panel"
	ok := host{version: "v1.0.0", euid: 0, invocation: "abc", exe: exe, root: root}
	cases := []struct {
		name string
		edit func(*host)
		want string
	}{
		{"systemd release", func(*host) {}, KindSystemd},
		{"docker", func(h *host) { h.docker = true }, KindDocker},
		{"dev build", func(h *host) { h.version = "dev" }, KindSource},
		{"git describe", func(h *host) { h.version = "v1.0.0-3-gabcdef1" }, KindSource},
		{"not root", func(h *host) { h.euid = 1000 }, KindNotRoot},
		{"run by hand", func(h *host) { h.invocation = "" }, KindSource},
		{"another binary", func(h *host) { h.exe = "/tmp/nasnet-panel" }, KindSource},
	}
	for _, c := range cases {
		h := ok
		c.edit(&h)
		k := kindOf(h)
		if k.Name != c.want {
			t.Errorf("%s: kind %s, want %s", c.name, k.Name, c.want)
		}
		if k.Name != KindSystemd && k.Command == "" {
			t.Errorf("%s: no command to run instead", c.name)
		}
	}
}

func TestCheckNowStoresTheResultAndAnnouncesAVersionOnce(t *testing.T) {
	key := testKey(t)
	f := &fakeFetcher{files: map[string][]byte{}}
	f.publish(t, key, "", release.Manifest{Version: "v1.1.0", Notes: "Fixes."})
	settings := newMemSettings()
	u, sub := testUsecase(t, "v1.0.0", f, settings)

	for i := 0; i < 2; i++ {
		if _, err := u.CheckNow(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if v, ok := u.Available(context.Background()); !ok || v != "v1.1.0" {
		t.Fatalf("available = %s %v", v, ok)
	}
	if settings.value(keyNotified) != "v1.1.0" {
		t.Fatal("the announced version wasn't kept")
	}
	if got := drain(sub); len(got) != 1 || got[0].Type != events.EventPanelUpdateAvailable {
		t.Fatalf("events = %+v", got)
	}
	if row, _ := settings.GetByKey(context.Background(), keyLastCheck); row.Category != "admin" {
		t.Fatalf("the check result is in category %q", row.Category)
	}
}

func TestAFailedCheckIsKeptButNotAnnounced(t *testing.T) {
	u, sub := testUsecase(t, "v1.0.0", &fakeFetcher{down: true}, newMemSettings())
	c, err := u.CheckNow(context.Background())
	if err != nil || c.Error == "" {
		t.Fatalf("check = %+v, %v", c, err)
	}
	st, err := u.Status(context.Background())
	if err != nil || st.Check == nil || st.Check.Error == "" || st.Available != nil {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if got := drain(sub); len(got) != 0 {
		t.Fatalf("events = %+v", got)
	}
}

func TestADevBuildDoesNotCheck(t *testing.T) {
	u, _ := testUsecase(t, "dev", &fakeFetcher{down: true}, newMemSettings())
	if _, err := u.CheckNow(context.Background()); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheTimerChecksTwoMinutesAfterBootThenEverySixHours(t *testing.T) {
	key := testKey(t)
	f := &fakeFetcher{files: map[string][]byte{}}
	f.publish(t, key, "", release.Manifest{Version: "v1.1.0"})
	settings := newMemSettings()
	u, _ := testUsecase(t, "v1.0.0", f, settings)
	boot := time.Now()
	clock := boot
	u.now = func() time.Time { return clock }
	u.nextCheck = boot.Add(firstCheck)

	u.MaybeRun(context.Background())
	if settings.value(keyLastCheck) != "" {
		t.Fatal("checked right at boot")
	}
	clock = boot.Add(firstCheck)
	u.MaybeRun(context.Background())
	if settings.value(keyLastCheck) == "" {
		t.Fatal("no check two minutes after boot")
	}
	if next := u.nextCheck.Sub(clock); next < checkEvery || next > checkEvery+checkJitter {
		t.Fatalf("next check in %s", next)
	}

	// turned off: the tick does nothing, Check now still works
	settings.rows[keyCheckEnabled] = &settingDomain.Setting{Key: keyCheckEnabled, Value: "false"}
	delete(settings.rows, keyLastCheck)
	clock = u.nextCheck
	u.MaybeRun(context.Background())
	if settings.value(keyLastCheck) != "" {
		t.Fatal("checked with the timer off")
	}
	if _, err := u.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func drain(sub events.Subscriber) []events.Event {
	var got []events.Event
	for {
		select {
		case e := <-sub:
			got = append(got, e)
		case <-time.After(50 * time.Millisecond):
			return got
		}
	}
}
