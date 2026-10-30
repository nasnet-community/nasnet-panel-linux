// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
)

// begin starts a job that does nothing until it is told to end, and gives the
// test its Run, so the test can report steps itself and read what the API
// says in between.
func (r *rig) begin(spec jobs.Spec) (job jobs.Job, run *jobs.Run, end func(error)) {
	r.t.Helper()
	runs := make(chan *jobs.Run, 1)
	stop := make(chan error, 1)
	job, err := r.jobs.Start(spec, func(ctx context.Context, jr *jobs.Run) error {
		runs <- jr
		select {
		case err := <-stop:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		r.t.Fatalf("Start: %v", err)
	}
	return job, <-runs, func(err error) { stop <- err }
}

// finished runs a job to its end and returns it.
func (r *rig) finished(spec jobs.Spec, fn func(*jobs.Run) error) jobs.Job {
	r.t.Helper()
	job, err := r.jobs.Start(spec, func(_ context.Context, jr *jobs.Run) error { return fn(jr) })
	if err != nil {
		r.t.Fatalf("Start: %v", err)
	}
	return r.settle(job.ID)
}

func (r *rig) settle(id string) jobs.Job {
	r.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if j, ok := r.jobs.Get(id); ok && j.State != jobs.StateRunning {
			return j
		}
	}
	r.t.Fatalf("job %s did not end", id)
	return jobs.Job{}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("never happened: %s", what)
}

// stream is a response read a line at a time, as an EventSource would.
type stream struct {
	t     *testing.T
	resp  *http.Response
	lines chan string
	done  chan struct{}
	once  sync.Once
}

// open starts reading the stream at path. The headers are exactly those given.
func (r *rig) open(path string, headers map[string]string) *stream {
	r.t.Helper()
	req, err := http.NewRequest("GET", r.srv.URL+path, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// a stream whose headers never come would otherwise hang the test
	client := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 5 * time.Second}}
	r.t.Cleanup(client.CloseIdleConnections)
	resp, err := client.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	s := &stream{t: r.t, resp: resp, lines: make(chan string), done: make(chan struct{})}
	go func() {
		defer close(s.lines)
		br := bufio.NewReader(resp.Body)
		for {
			line, err := br.ReadString('\n')
			if line != "" {
				select {
				case s.lines <- strings.TrimRight(line, "\r\n"):
				case <-s.done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	r.t.Cleanup(s.close)
	return s
}

// close is the browser going away.
func (s *stream) close() {
	s.once.Do(func() { close(s.done); s.resp.Body.Close() })
}

func (s *stream) line() (string, bool) {
	s.t.Helper()
	select {
	case l, ok := <-s.lines:
		return l, ok
	case <-time.After(5 * time.Second):
		s.t.Fatal("nothing came on the stream in 5 s")
		return "", false
	}
}

// event is the lines of the next event, up to the blank line that ends it.
func (s *stream) event() []string {
	s.t.Helper()
	var lines []string
	for {
		l, ok := s.line()
		if !ok {
			s.t.Fatalf("the stream ended inside an event: %q", lines)
		}
		if l == "" {
			return lines
		}
		lines = append(lines, l)
	}
}

func isPing(ev []string) bool { return len(ev) == 1 && ev[0] == ": ping" }

// job is the next event as a Job, past any pings in the way.
func (s *stream) job() jobs.Job {
	s.t.Helper()
	for {
		ev := s.event()
		if isPing(ev) {
			continue
		}
		if len(ev) != 1 || !strings.HasPrefix(ev[0], "data: ") {
			s.t.Fatalf("not an event with one data line: %q", ev)
		}
		var j jobs.Job
		if err := json.Unmarshal([]byte(strings.TrimPrefix(ev[0], "data: ")), &j); err != nil {
			s.t.Fatalf("event is not a job: %v: %q", err, ev[0])
		}
		return j
	}
}

// ended is true once the server has closed the stream, with nothing more on it.
func (s *stream) ended() bool {
	s.t.Helper()
	l, ok := s.line()
	if ok {
		s.t.Logf("a line before the end: %q", l)
	}
	return !ok
}

func eventsPath(b, id string) string { return b + "/api/jobs/" + id + "/events" }

func (r *rig) wantRefused(code int, hdr http.Header, body []byte, what string) {
	r.t.Helper()
	got := decode[map[string]any](r.t, body)
	if code != http.StatusForbidden || got["code"] != "forbidden" || got["error"] == "" || len(got) != 2 ||
		!strings.HasPrefix(hdr.Get("Content-Type"), "application/json") {
		r.t.Errorf("%s = %d %q %s, want 403 forbidden as JSON", what, code, hdr.Get("Content-Type"), body)
	}
}

func TestTheStreamFollowsAJobFromTheSnapshotToTheEnd(t *testing.T) {
	for _, b := range []string{"", base} {
		r := newRig(t, b)
		job, run, end := r.begin(jobs.Spec{Kind: jobs.KindInstall, NodeID: 1, Target: "Node 1", Steps: []string{"release", "verify"}})
		// no header: an EventSource can't send one
		s := r.open(eventsPath(b, job.ID), nil)
		hdr := s.resp.Header
		if s.resp.StatusCode != 200 || hdr.Get("Content-Type") != "text/event-stream" || hdr.Get("Cache-Control") != "no-store" ||
			hdr.Get("X-Accel-Buffering") != "no" || hdr.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("base %q: the stream answers %d with %v", b, s.resp.StatusCode, hdr)
		}

		step := func(what string, got jobs.Job, want ...jobs.Step) {
			t.Helper()
			if got.ID != job.ID || got.State != jobs.StateRunning || len(got.Steps) != len(want) {
				t.Fatalf("%s: %+v", what, got)
			}
			for i := range want {
				if got.Steps[i] != want[i] {
					t.Fatalf("%s: steps = %v, want %v", what, got.Steps, want)
				}
			}
		}
		first := s.job()
		step("the snapshot", first, jobs.Step{Name: "release", State: jobs.StepPending}, jobs.Step{Name: "verify", State: jobs.StepPending})
		if first.Kind != jobs.KindInstall || first.NodeID != 1 || first.Target != "Node 1" || !first.StartedAt.Equal(now) {
			t.Fatalf("the snapshot = %+v", first)
		}

		run.Begin("release")
		step("Begin", s.job(), jobs.Step{Name: "release", State: jobs.StepRunning}, jobs.Step{Name: "verify", State: jobs.StepPending})
		run.Detail("release", "62% · 8.4 of 13.6 MB")
		step("Detail", s.job(), jobs.Step{Name: "release", State: jobs.StepRunning, Detail: "62% · 8.4 of 13.6 MB"}, jobs.Step{Name: "verify", State: jobs.StepPending})
		run.Done("release")
		step("Done", s.job(), jobs.Step{Name: "release", State: jobs.StepOK, Detail: "62% · 8.4 of 13.6 MB"}, jobs.Step{Name: "verify", State: jobs.StepPending})

		end(nil)
		last := s.job()
		if last.State != jobs.StateOK || last.FinishedAt == nil || last.Error != "" || len(last.Steps) != 1 {
			t.Fatalf("the last event = %+v", last)
		}
		if !s.ended() {
			t.Fatal("the stream stays open after the final state")
		}
	}
}

// A job that fails ends the stream the same way.
func TestTheStreamEndsAfterAFailureToo(t *testing.T) {
	r := newRig(t, "")
	job, run, end := r.begin(jobs.Spec{Kind: jobs.KindRemove, Steps: []string{"a"}})
	s := r.open(eventsPath("", job.ID), nil)
	s.job()
	run.Begin("a")
	s.job()
	end(errors.New("it broke"))
	last := s.job()
	if last.State != jobs.StateFailed || last.Error != "it broke" || last.Steps[0].State != jobs.StepFailed {
		t.Fatalf("the last event = %+v", last)
	}
	if !s.ended() {
		t.Fatal("the stream stays open after the final state")
	}
}

func TestTheStreamOfAFinishedJobIsOneEvent(t *testing.T) {
	r := newRig(t, "")
	done := r.finished(jobs.Spec{Kind: jobs.KindRenewCert, NodeID: 2}, func(*jobs.Run) error { return nil })
	s := r.open(eventsPath("", done.ID), nil)
	got := s.job()
	if got.State != jobs.StateOK || got.FinishedAt == nil || got.ID != done.ID {
		t.Fatalf("event = %+v", got)
	}
	if !s.ended() {
		t.Fatal("the stream of a finished job stays open")
	}
}

func TestEveryEventIsOneDataLineAndABlankLine(t *testing.T) {
	r := newRig(t, "")
	job, run, end := r.begin(jobs.Spec{Kind: jobs.KindInstall, Steps: []string{"a"}})
	s := r.open(eventsPath("", job.ID), nil)
	snapshot := s.event()
	// text with newlines in it still goes out on one line
	run.Detail("a", "line one\nline two\r\nline three")
	for _, ev := range [][]string{snapshot, s.event()} {
		if len(ev) != 1 || !strings.HasPrefix(ev[0], "data: {") || !strings.HasSuffix(ev[0], "}") {
			t.Fatalf("event = %q", ev)
		}
	}
	end(nil)
}

func TestAnIdleStreamPingsWithAComment(t *testing.T) {
	r := newRigWith(t, "", func(c *Config, _ *jobs.Engine) { c.JobPing = 15 * time.Millisecond })
	job, run, end := r.begin(jobs.Spec{Kind: jobs.KindInstall, Steps: []string{"a"}})
	s := r.open(eventsPath("", job.ID), nil)
	s.job() // the snapshot
	for i := 0; i < 3; i++ {
		if ev := s.event(); !isPing(ev) {
			t.Fatalf("event %d while idle = %q, want a ping comment", i, ev)
		}
	}
	// events still arrive between the pings
	run.Begin("a")
	if got := s.job(); got.Steps[0].State != jobs.StepRunning {
		t.Fatalf("event = %+v", got)
	}
	end(nil)
	if last := s.job(); last.State != jobs.StateOK {
		t.Fatalf("last = %+v", last)
	}
	if !s.ended() {
		t.Fatal("the stream stays open after the final state")
	}
}

// With a ping due every millisecond, nothing but the final event may be the
// last thing on the stream: not a comment slipped in before the close.
func TestNothingFollowsTheFinalEvent(t *testing.T) {
	r := newRigWith(t, "", func(c *Config, _ *jobs.Engine) { c.JobPing = time.Millisecond })
	for i := 0; i < 40; i++ {
		job, _, end := r.begin(jobs.Spec{Kind: jobs.KindRenewCert})
		s := r.open(eventsPath("", job.ID), nil)
		s.job() // the snapshot
		time.Sleep(time.Duration(i%4) * time.Millisecond)
		end(nil)
		if last := s.job(); last.State != jobs.StateOK {
			t.Fatalf("run %d: last = %+v", i, last)
		}
		if !s.ended() {
			t.Fatalf("run %d: something came after the final event", i)
		}
	}
}

// The hub's server has no write timeout, but one that is added later must not
// cut the stream off: each write sets its own deadline.
func TestTheStreamOutlivesAServerWriteTimeout(t *testing.T) {
	base := newRig(t, "")
	h := New(Config{Store: base.st, Jobs: base.jobs, JobPing: 30 * time.Millisecond, Log: slog.New(slog.NewTextHandler(base.log, nil)), Now: func() time.Time { return now }})
	srv := httptest.NewUnstartedServer(h)
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	r := &rig{t: t, st: base.st, srv: srv, log: base.log, jobs: base.jobs}

	job, run, end := r.begin(jobs.Spec{Kind: jobs.KindInstall, Steps: []string{"a"}})
	s := r.open(eventsPath("", job.ID), nil)
	s.job() // the snapshot
	// six times as long as the timeout, in pings
	for i := 0; i < 20; i++ {
		if ev := s.event(); !isPing(ev) {
			t.Fatalf("event %d = %q, want a ping", i, ev)
		}
	}
	run.Begin("a")
	if got := s.job(); got.Steps[0].State != jobs.StepRunning {
		t.Fatalf("event = %+v", got)
	}
	end(nil)
	if last := s.job(); last.State != jobs.StateOK {
		t.Fatalf("last = %+v", last)
	}
}

func TestThePingIsFifteenSecondsByDefault(t *testing.T) {
	if defaultJobPing != 15*time.Second {
		t.Fatalf("default ping = %v", defaultJobPing)
	}
}

// spy counts the streams that start and the ones that let go.
type spy struct {
	Jobs
	subscribed, cancelled atomic.Int32
}

func (s *spy) Subscribe(id string) (<-chan jobs.Job, func(), bool) {
	ch, cancel, ok := s.Jobs.Subscribe(id)
	if !ok {
		return ch, cancel, ok
	}
	s.subscribed.Add(1)
	return ch, func() { s.cancelled.Add(1); cancel() }, true
}

func TestTheStreamLetsGoWhenTheBrowserDoes(t *testing.T) {
	var sp *spy
	r := newRigWith(t, "", func(c *Config, e *jobs.Engine) { sp = &spy{Jobs: e}; c.Jobs = sp })
	job, run, end := r.begin(jobs.Spec{Kind: jobs.KindInstall, Steps: []string{"a"}})

	s := r.open(eventsPath("", job.ID), nil)
	s.job() // so the handler has subscribed
	if sp.subscribed.Load() != 1 || sp.cancelled.Load() != 0 {
		t.Fatalf("subscribed %d, let go %d", sp.subscribed.Load(), sp.cancelled.Load())
	}
	s.close()
	eventually(t, "the handler letting go of its subscription", func() bool { return sp.cancelled.Load() == 1 })

	// and the job doesn't mind
	run.Begin("a")
	end(nil)
	r.settle(job.ID)

	// a stream that ends with its job lets go too
	other, _, endOther := r.begin(jobs.Spec{Kind: jobs.KindRemove})
	s2 := r.open(eventsPath("", other.ID), nil)
	s2.job()
	endOther(nil)
	s2.job()
	if !s2.ended() {
		t.Fatal("not ended")
	}
	eventually(t, "the second stream letting go", func() bool { return sp.cancelled.Load() == 2 })
}

func TestAnUnknownJobIsRefusedBeforeAnyStreamBegins(t *testing.T) {
	for _, name := range []string{"an engine with no such job", "no job source"} {
		var mod func(*Config, *jobs.Engine)
		if name == "no job source" {
			mod = func(c *Config, _ *jobs.Engine) { c.Jobs = nil }
		}
		for _, b := range []string{"", base} {
			r := newRigWith(t, b, mod)
			for _, p := range []string{eventsPath(b, "nope"), b + "/api/jobs/nope"} {
				for _, hdr := range []map[string]string{{UIHeader: "1"}, nil} {
					if hdr == nil && !strings.HasSuffix(p, "/events") {
						continue // the other route needs the header, which is checked elsewhere
					}
					code, h, body := r.call("GET", p, hdr)
					got := decode[map[string]any](t, body)
					if code != http.StatusNotFound || got["code"] != "not_found" || got["error"] == "" || len(got) != 2 ||
						!strings.HasPrefix(h.Get("Content-Type"), "application/json") {
						t.Errorf("%s, GET %s with %v = %d %q %s", name, p, hdr, code, h.Get("Content-Type"), body)
					}
				}
			}
		}
	}
}

func TestEventsNeedNoHeaderButRefuseAnotherSite(t *testing.T) {
	for _, b := range []string{"", base} {
		r := newRig(t, b)
		done := r.finished(jobs.Spec{Kind: jobs.KindRenewCert}, func(*jobs.Run) error { return nil })
		for _, c := range []struct {
			site string
			ok   bool
		}{
			{"", true}, {"same-origin", true}, {"none", true},
			{"cross-site", false}, {"same-site", false}, {"anything-else", false},
		} {
			for _, withHeader := range []bool{false, true} {
				hdr := map[string]string{}
				if c.site != "" {
					hdr["Sec-Fetch-Site"] = c.site
				}
				if withHeader {
					hdr[UIHeader] = "1"
				}
				what := "GET events with " + c.site
				code, h, body := r.call("GET", eventsPath(b, done.ID), hdr)
				if !c.ok {
					r.wantRefused(code, h, body, what)
					continue
				}
				if code != http.StatusOK || h.Get("Content-Type") != "text/event-stream" || !strings.HasPrefix(string(body), "data: {") {
					t.Errorf("base %q: %s (header %v) = %d %q %s", b, what, withHeader, code, h.Get("Content-Type"), body)
				}
			}
		}
	}
}

// Only the stream is let in without the page's header: the list and one job
// are read with fetch, which can send it, so they must.
func TestTheListAndAJobStillNeedTheHeader(t *testing.T) {
	for _, b := range []string{"", base} {
		r := newRig(t, b)
		done := r.finished(jobs.Spec{Kind: jobs.KindRenewCert}, func(*jobs.Run) error { return nil })
		for _, prefix := range prefixes(b) {
			for _, p := range []string{prefix + "/api/jobs", prefix + "/api/jobs/" + done.ID, prefix + "/api/jobs/nope"} {
				for _, hdr := range []map[string]string{nil, {UIHeader: "0"}, {"Sec-Fetch-Site": "same-origin"}, {"Sec-Fetch-Site": "none"}} {
					code, h, body := r.call("GET", p, hdr)
					r.wantRefused(code, h, body, "GET "+p+" with "+strings.Join(mapPairs(hdr), " "))
				}
				if code, _, body := r.call("GET", p, map[string]string{UIHeader: "1"}); code != 200 && code != 404 {
					t.Errorf("GET %s with the header = %d %s", p, code, body)
				}
			}
		}
	}
}

func mapPairs(m map[string]string) []string {
	var out []string
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// Under /api, anything but a GET of the stream is told off like any other
// path: the exemption is for that one call.
func TestOnlyAGetOfTheStreamIsExempt(t *testing.T) {
	r := newRig(t, "")
	done := r.finished(jobs.Spec{Kind: jobs.KindRenewCert}, func(*jobs.Run) error { return nil })
	p := eventsPath("", done.ID)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		code, h, body := r.call(method, p, nil)
		r.wantRefused(code, h, body, method+" events without the header")
		code, _, body = r.call(method, p, map[string]string{UIHeader: "1"})
		if got := decode[map[string]any](t, body); code != http.StatusNotFound || got["code"] != "not_found" {
			t.Errorf("%s events with the header = %d %s", method, code, body)
		}
	}
	for _, p := range []string{p + "/more", "/api/jobs/" + done.ID + "/event", "/api/jobs/" + done.ID + "/events/"} {
		code, h, body := r.call("GET", p, nil)
		r.wantRefused(code, h, body, "GET "+p)
	}
}

func TestNoJobsMeansAnEmptyList(t *testing.T) {
	for _, b := range []string{"", base} {
		for _, mod := range []func(*Config, *jobs.Engine){nil, func(c *Config, _ *jobs.Engine) { c.Jobs = nil }} {
			r := newRigWith(t, b, mod)
			for _, p := range prefixes(b) {
				if got := string(r.get(p + "/api/jobs")); got != "[]\n" {
					t.Errorf("base %q: GET %s/api/jobs = %q, want []", b, p, got)
				}
			}
		}
	}
}

// What the page reads is the contract's Job, key for key.
func TestTheListAndAJobAreTheContractsJobs(t *testing.T) {
	for _, b := range []string{"", base} {
		r := newRig(t, b)
		failed := r.finished(jobs.Spec{Kind: jobs.KindRotateKeys}, func(run *jobs.Run) error { return errors.New("no luck") })
		job, run, end := r.begin(jobs.Spec{Kind: jobs.KindInstall, NodeID: 4, Target: "Node 4", Steps: []string{"release", "verify"}})
		run.Begin("release")
		run.Detail("release", "38% · 5 of 13 MB")

		for _, prefix := range prefixes(b) {
			list := decode[[]map[string]any](t, r.get(prefix+"/api/jobs"))
			if len(list) != 2 || list[0]["id"] != job.ID || list[1]["id"] != failed.ID {
				t.Fatalf("base %q: the list = %v, want the running job first, then the failed one", b, list)
			}
			wantKeys(t, "a running job", list[0], "id kind state steps startedAt nodeId target")
			wantKeys(t, "a failed job", list[1], "id kind state steps startedAt finishedAt error")
			if list[0]["state"] != "running" || list[0]["kind"] != "install" || list[0]["nodeId"] != 4.0 || list[0]["target"] != "Node 4" ||
				list[0]["startedAt"] != "2026-10-30T12:00:00Z" {
				t.Errorf("the running job = %v", list[0])
			}
			if list[1]["state"] != "failed" || list[1]["error"] != "no luck" || list[1]["finishedAt"] != "2026-10-30T12:00:00Z" {
				t.Errorf("the failed job = %v", list[1])
			}
			if steps, ok := list[1]["steps"].([]any); !ok || len(steps) != 0 {
				t.Errorf("a job with no steps has steps = %#v, want []", list[1]["steps"])
			}
			steps := list[0]["steps"].([]any)
			wantKeys(t, "a step with a detail", steps[0].(map[string]any), "name state detail")
			wantKeys(t, "a step with none", steps[1].(map[string]any), "name state")

			one := decode[map[string]any](t, r.get(prefix+"/api/jobs/"+job.ID))
			if !reflect.DeepEqual(one, list[0]) {
				t.Errorf("GET /api/jobs/{id} = %v, the list has %v", one, list[0])
			}
		}
		end(nil)
	}
}

// A secret the job was told about does not reach any body the page can read,
// whichever way the job tried to say it.
func TestNoJobBodyHoldsASecret(t *testing.T) {
	r := newRig(t, "")
	done := r.finished(jobs.Spec{Kind: jobs.KindInstall, NodeID: 1, Steps: []string{"a", "b", "c"}, Secrets: secrets()}, func(run *jobs.Run) error {
		run.Begin("a")
		run.Detail("a", "using "+privateKey)
		run.Set("b", jobs.StepRunning, "pinned "+hostKeyPin)
		run.Fail("c", errors.New("client "+uuidA))
		return errors.New("failed with " + uuidB + " and " + privateKey)
	})

	bodies := map[string][]byte{
		"list": r.get("/api/jobs"),
		"job":  r.get("/api/jobs/" + done.ID),
	}
	_, _, bodies["events"] = r.call("GET", eventsPath("", done.ID), nil)
	for what, body := range bodies {
		if !strings.Contains(string(body), "…") {
			t.Errorf("%s shows no sign that a secret was hidden: %s", what, body)
		}
		for _, s := range secrets() {
			if strings.Contains(string(body), s) {
				t.Errorf("%s carries %q: %s", what, s, body)
			}
		}
		var generic any
		if json.Unmarshal([]byte(strings.TrimPrefix(string(body), "data: ")), &generic) == nil {
			walkKeys(generic, func(key string) {
				if secretish(key) {
					t.Errorf("%s has a key named %q", what, key)
				}
			})
		}
	}
}
