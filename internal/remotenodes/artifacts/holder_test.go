// SPDX-License-Identifier: AGPL-3.0-or-later

package artifacts

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestTheHolderFetchesEachAgentOnce(t *testing.T) {
	f := newFixture(t)
	f.publish("v1.3.0", "amd64", "arm64")
	h := NewHolder(f.srv.Client(), f.source())

	a1, err := h.Agent(bg, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	a2, err := h.Agent(bg, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if a1 != a2 {
		t.Error("the second node was given another copy")
	}
	if n := f.count("/agent-linux-arm64.gz"); n != 1 {
		t.Errorf("the agent was downloaded %d times", n)
	}
	// the small manifest is read each time, to know which release is current
	if n := f.count("/release.json"); n != 2 {
		t.Errorf("release.json was read %d times, want 2", n)
	}

	b, err := h.Agent(bg, "amd64")
	if err != nil || b == a1 || b.Arch != "amd64" {
		t.Fatalf("another architecture: %v", err)
	}
	if f.count("/agent-linux-amd64.gz") != 1 || f.count("/agent-linux-arm64.gz") != 1 {
		t.Errorf("requests %v", f.requested())
	}
}

func TestTheHolderFetchesEachXrayOnce(t *testing.T) {
	bins := map[string][]byte{"amd64": fakeBinary(), "arm64": fakeBinary()}
	zips := map[string][]byte{"amd64": releaseZip(t, bins["amd64"]), "arm64": releaseZip(t, bins["arm64"])}
	f, fx := xrayRig(t, zips, bins)
	h := NewHolder(f.srv.Client(), Source{})
	h.f = fx

	x1, err := h.Xray(bg, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	x2, err := h.Xray(bg, "arm64")
	if err != nil || x1 != x2 {
		t.Fatalf("the second node: %v, same copy %v", err, x1 == x2)
	}
	if n := f.count("/dl/v" + testXray + "/Xray-linux-arm64-v8a.zip"); n != 1 {
		t.Errorf("the zip was downloaded %d times", n)
	}
	y, err := h.Xray(bg, "amd64")
	if err != nil || y == x1 || y.Arch != "amd64" {
		t.Fatalf("another architecture: %v", err)
	}
	if len(f.requested()) != 2 {
		t.Errorf("requests %v", f.requested())
	}
}

func TestANewReleaseIsFetchedBesideTheOldOne(t *testing.T) {
	f := newFixture(t)
	f.publish("v1.3.0", "arm64")
	h := NewHolder(f.srv.Client(), f.source())
	old, err := h.Agent(bg, "arm64")
	if err != nil {
		t.Fatal(err)
	}

	f.publish("v1.3.1", "arm64") // lands in the middle of the job
	next, err := h.Agent(bg, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if next.Version != "v1.3.1" || old.Version != "v1.3.0" || next == old {
		t.Fatalf("got %s then %s", old.Version, next.Version)
	}
	again, _ := h.Agent(bg, "arm64")
	if again != next {
		t.Error("the new release was not kept")
	}
	if n := f.count("/agent-linux-arm64.gz"); n != 2 {
		t.Errorf("downloads: %d, want one for each release", n)
	}
}

func TestAFailedFetchIsNotKept(t *testing.T) {
	f := newFixture(t)
	f.publish("v1.3.0", "arm64")
	h := NewHolder(f.srv.Client(), f.source())
	f.hook("/agent-linux-arm64.gz", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusBadGateway) })
	if a, err := h.Agent(bg, "arm64"); err == nil || a != nil {
		t.Fatalf("a bad gateway: %v", err)
	}
	f.unhook("/agent-linux-arm64.gz")
	a, err := h.Agent(bg, "arm64")
	if err != nil || a == nil {
		t.Fatalf("the retry: %v", err)
	}
	if n := f.count("/agent-linux-arm64.gz"); n != 2 {
		t.Errorf("asked for the file %d times, want the failed try and the good one", n)
	}
}

func TestCallersAtOnceShareOneDownload(t *testing.T) {
	f := newFixture(t)
	f.publish("v1.3.0", "arm64")
	h := NewHolder(f.srv.Client(), f.source())
	slow := f.file("/agent-linux-arm64.gz")
	f.hook("/agent-linux-arm64.gz", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond) // long enough for every caller to be waiting
		w.Write(slow)
	})

	const callers = 10
	got := make([]*Agent, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := h.Agent(bg, "arm64")
			if err != nil {
				t.Error(err)
			}
			got[i] = a
		}()
	}
	wg.Wait()
	for i, a := range got {
		if a == nil || a != got[0] {
			t.Fatalf("caller %d got another copy", i)
		}
	}
	if n := f.count("/agent-linux-arm64.gz"); n != 1 {
		t.Fatalf("the agent was downloaded %d times for %d callers", n, callers)
	}
}

func TestACallerWaitingForAnotherDownloadCanGiveUp(t *testing.T) {
	f := newFixture(t)
	f.publish("v1.3.0", "amd64", "arm64")
	h := NewHolder(f.srv.Client(), f.source())
	arm := f.file("/agent-linux-arm64.gz")
	inside, release := make(chan struct{}), make(chan struct{})
	f.hook("/agent-linux-arm64.gz", func(w http.ResponseWriter, r *http.Request) {
		close(inside)
		<-release
		w.Write(arm)
	})

	first := make(chan error, 1)
	go func() { _, err := h.Agent(bg, "arm64"); first <- err }()
	<-inside // the one download that may run is under way

	ctx, cancel := context.WithTimeout(bg, 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := h.Agent(ctx, "amd64"); !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 3*time.Second {
		t.Fatalf("a caller that waited out its deadline: %v after %v", err, time.Since(started))
	}
	if f.count("/agent-linux-amd64.gz") != 0 {
		t.Error("the second download started while the first was running")
	}

	close(release)
	if err := <-first; err != nil {
		t.Fatalf("the first download: %v", err)
	}
	if _, err := h.Agent(bg, "amd64"); err != nil {
		t.Fatalf("the one that gave up, afterwards: %v", err)
	}
}

func TestReleaseLetsGoOfWhatIsHeld(t *testing.T) {
	f := newFixture(t)
	f.publish("v1.3.0", "arm64")
	h := NewHolder(f.srv.Client(), f.source())
	h.Release() // with nothing held
	if _, err := h.Agent(bg, "arm64"); err != nil {
		t.Fatal(err)
	}
	if len(h.held) != 1 {
		t.Fatalf("held %d things", len(h.held))
	}
	h.Release()
	h.Release()
	if len(h.held) != 0 {
		t.Fatalf("still holding %d things after Release", len(h.held))
	}
	// the holder is still good, and fetches again
	if _, err := h.Agent(bg, "arm64"); err != nil {
		t.Fatal(err)
	}
	if n := f.count("/agent-linux-arm64.gz"); n != 2 {
		t.Errorf("downloads: %d, want one before the release and one after", n)
	}
}
