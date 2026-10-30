// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"bytes"
	"io"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestUploadDetailReadsLikeProgress(t *testing.T) {
	t0 := time.Unix(1000, 0)
	m := &meter{total: 100 << 20, start: t0, sent: 38 << 20}
	if got, want := m.detailLocked(t0.Add(time.Minute)), "38% · 38.0 MB sent · about 2 min left"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	m.sent = 4089446 // 3.9 MB
	m.total = int64(float64(m.sent) / 0.38)
	if got, want := m.detailLocked(t0.Add(30*time.Second)), "38% · 3.9 MB sent · about 50 s left"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// no guess in the first seconds, and kilobytes below a megabyte
	m.sent, m.total = 300<<10, 100<<20
	if got, want := m.detailLocked(t0.Add(2*time.Second)), "0% · 300 KB sent"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// a promise is rounded up, never down
	for in, want := range map[time.Duration]string{
		time.Second: "5 s", 12 * time.Second: "15 s", 59 * time.Second: "60 s", 61 * time.Second: "2 min", 119 * time.Second: "2 min",
	} {
		if got := roughly(in); got != want {
			t.Errorf("roughly(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestTheMeterReportsAboutOnceASecond(t *testing.T) {
	var details []string
	st := newStepper(func(s Step) {
		if s.Name != StepUploadAgent || s.State != StateRunning {
			t.Errorf("unexpected step %+v", s)
		}
		details = append(details, s.Detail)
	})
	clk := &clock{t: time.Unix(5000, 0)}
	data := bytes.Repeat([]byte("x"), 10<<20)
	m := newMeter(st, StepUploadAgent, int64(len(data)), clk.now)
	r := m.reader(bytes.NewReader(data))

	buf := make([]byte, 1<<20)
	var reads int
	for {
		// 400 ms per megabyte: three reports in the 4 s the upload takes
		clk.t = clk.t.Add(400 * time.Millisecond)
		n, err := r.Read(buf)
		if n > 0 {
			reads++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if reads != 10 {
		t.Fatalf("%d reads", reads)
	}
	// the first read reports at once, then none for a second, and the last
	// always does, so the page ends on 100%
	if len(details) < 3 || len(details) > 6 {
		t.Fatalf("%d reports for a 4 s upload: %q", len(details), details)
	}
	if details[0] != "10% · 1.0 MB sent" {
		t.Errorf("first report %q", details[0])
	}
	if last := details[len(details)-1]; last != "100% · 10.0 MB sent" {
		t.Errorf("last report %q", last)
	}
	if got := m.summary(); got != "10.0 MB" {
		t.Errorf("summary %q", got)
	}
}

func TestTheMeterNeverGoesPastAHundredPercent(t *testing.T) {
	var last string
	st := newStepper(func(s Step) { last = s.Detail })
	clk := &clock{t: time.Unix(1, 0)}
	m := newMeter(st, "x", 10, clk.now)
	m.reader(bytes.NewReader(make([]byte, 50))).Read(make([]byte, 50))
	if last != "100% · 0 KB sent" {
		t.Errorf("report %q", last)
	}
	// and an upload of unknown size doesn't divide by zero
	newMeter(st, "x", 0, clk.now).add(5)
}
