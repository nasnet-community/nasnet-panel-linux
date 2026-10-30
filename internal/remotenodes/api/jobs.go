// SPDX-License-Identifier: AGPL-3.0-or-later

// The event stream (the headers, a comment now and then to keep the line
// open, a flush after every event, an end once the job has one) follows
// jobStream in the Telegram MTProto plugin
// (github.com/nasnet-community/mtproto-remote-plugin), which is MIT licensed:
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

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
)

// Jobs is where the API reads jobs from. *jobs.Engine is one. What it hands
// out is the contract's Job as it is: the engine has already taken every
// secret out of the text.
type Jobs interface {
	List() []jobs.Job
	Get(id string) (jobs.Job, bool)
	Subscribe(id string) (events <-chan jobs.Job, cancel func(), ok bool)
}

var _ Jobs = (*jobs.Engine)(nil)

const (
	defaultJobPing = 15 * time.Second
	// streamWriteWait is how long one write to an event stream may take. The
	// server has no write timeout, which a stream could not live with, so each
	// write sets its own deadline: a reader that stopped taking data is let go,
	// and the stream does not depend on how the server is set up.
	streamWriteWait = 30 * time.Second
)

var errNoJob = &Error{CodeNotFound, "There is no such job."}

// listJobs is the job tray: what runs and what ended lately, newest first.
func (a *API) listJobs(w http.ResponseWriter, _ *http.Request) {
	list := []jobs.Job{}
	if a.cfg.Jobs != nil {
		if got := a.cfg.Jobs.List(); got != nil {
			list = got
		}
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) getJob(w http.ResponseWriter, r *http.Request) {
	if a.cfg.Jobs != nil {
		if job, ok := a.cfg.Jobs.Get(r.PathValue("id")); ok {
			writeJSON(w, http.StatusOK, job)
			return
		}
	}
	a.fail(w, errNoJob)
}

// jobEvents streams a job as server-sent events: the job as it stands, then
// the job after every change, each a full Job. It ends with the final state,
// or when the browser goes.
func (a *API) jobEvents(w http.ResponseWriter, r *http.Request) {
	if a.cfg.Jobs == nil {
		a.fail(w, errNoJob)
		return
	}
	events, cancel, ok := a.cfg.Jobs.Subscribe(r.PathValue("id"))
	if !ok {
		a.fail(w, errNoJob) // as plain JSON: the stream has not begun
		return
	}
	defer cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // nginx would hold the stream back
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	send := func(s string) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(streamWriteWait))
		if _, err := io.WriteString(w, s); err != nil {
			return false
		}
		err := rc.Flush()
		return err == nil || errors.Is(err, http.ErrNotSupported)
	}
	ping := time.NewTicker(a.cfg.JobPing)
	defer ping.Stop()
	for {
		select {
		case job, open := <-events:
			if !open {
				return
			}
			b, err := json.Marshal(job)
			if err != nil {
				a.cfg.Log.Error("job event not encoded", "err", err)
				return
			}
			if !send("data: "+string(b)+"\n\n") || job.State != jobs.StateRunning {
				return // the final state is the last thing sent, not even a ping after it
			}
		case <-ping.C:
			if !send(": ping\n\n") {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
