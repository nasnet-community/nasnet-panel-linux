// SPDX-License-Identifier: AGPL-3.0-or-later

// The upload meter, and the way it words its progress, follow the one in the
// Telegram MTProto plugin (github.com/nasnet-community/mtproto-remote-plugin),
// which is MIT licensed, so the page reads the same for both:
//
//	MIT License
//
//	Copyright (c) 2026 NASNET Community
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE.

package agentops

import (
	"fmt"
	"io"
	"math"
	"sync"
	"time"
)

// meter counts the bytes of one upload as the connection takes them, and says
// how far it has got on the step about once a second, so a slow link shows
// progress instead of looking stuck.
type meter struct {
	st    *stepper
	step  string
	total int64
	now   func() time.Time
	start time.Time

	mu   sync.Mutex
	sent int64
	last time.Time
}

func newMeter(st *stepper, step string, total int64, now func() time.Time) *meter {
	return &meter{st: st, step: step, total: total, now: now, start: now()}
}

// reader counts what r gives out. Whoever reads from it is the one that sends,
// so what it has given out is what has been sent, give or take what the
// connection holds in its buffers.
func (m *meter) reader(r io.Reader) io.Reader { return meterReader{r, m} }

type meterReader struct {
	r io.Reader
	m *meter
}

func (c meterReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.m.add(int64(n))
	}
	return n, err
}

func (m *meter) add(n int64) {
	m.mu.Lock()
	m.sent += n
	now := m.now()
	if now.Sub(m.last) < time.Second && m.sent < m.total {
		m.mu.Unlock()
		return
	}
	m.last = now
	detail := m.detailLocked(now)
	m.mu.Unlock()
	m.st.progress(m.step, detail)
}

// detailLocked reads like "38% · 3.9 MB sent · about 2 min left".
func (m *meter) detailLocked(now time.Time) string {
	pct := int64(100)
	if m.total > 0 {
		pct = min(m.sent*100/m.total, 100)
	}
	s := fmt.Sprintf("%d%% · %s sent", pct, byteSize(m.sent))
	// the first seconds say little about the rate
	if el := now.Sub(m.start); el >= 3*time.Second && m.sent > 0 && m.sent < m.total {
		left := time.Duration(float64(el) * float64(m.total-m.sent) / float64(m.sent))
		s += " · about " + roughly(left) + " left"
	}
	return s
}

// summary is what a finished upload says about itself.
func (m *meter) summary() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return byteSize(m.sent)
}

func byteSize(n int64) string {
	if n < 1<<20 {
		return fmt.Sprintf("%d KB", (n+512)>>10)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

// roughly rounds up: a promise of "1 min" that takes two is worse than the reverse.
func roughly(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d s", int(math.Ceil(d.Seconds()/5))*5)
	}
	return fmt.Sprintf("%d min", int(math.Ceil(d.Minutes())))
}
