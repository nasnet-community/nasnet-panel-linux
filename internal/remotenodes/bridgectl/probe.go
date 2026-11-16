// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgectl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// ErrNoProgram is Probe's answer when the bridge program is not there to run,
// as on a machine the hub is only tried out on.
var ErrNoProgram = errors.New("bridgectl: there is no bridge program to run")

const (
	// defaultProbeMargin is how much longer than the request's own timeout a
	// probe may take: the program has to start, build the outbound and answer.
	defaultProbeMargin = 5 * time.Second
	// maxProbeOutput is how much of what the program prints is kept.
	maxProbeOutput = 64 << 10
)

// Probe runs the bridge program once to try an outbound, apart from the bridge
// the supervisor keeps: ProbeArg as its argument, the request as JSON on its
// stdin, and the result read back from its stdout. It runs as the bridge does,
// as the same user and with the same environment.
//
// One probe runs at a time; another waits for it, or for its own ctx. The
// program is killed when ctx ends or when the request's timeout and a margin
// have gone by. An error means there is no result: ErrNoProgram when there is
// no program, else what went wrong running it. What the program found wrong
// with the outbound is a result, with OK false.
func (s *Supervisor) Probe(ctx context.Context, req bridgespec.ProbeRequest) (bridgespec.ProbeResult, error) {
	var none bridgespec.ProbeResult
	if err := req.Validate(); err != nil {
		return none, fmt.Errorf("bridgectl: the probe request: %w", err)
	}
	if _, err := exec.LookPath(s.cfg.Binary); err != nil {
		return none, fmt.Errorf("%w: %v", ErrNoProgram, err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return none, fmt.Errorf("bridgectl: %w", err)
	}

	select {
	case s.probeGate <- struct{}{}:
		defer func() { <-s.probeGate }()
	case <-ctx.Done():
		return none, ctx.Err()
	}

	limit := time.Duration(req.TimeoutOrDefault())*time.Second + s.probeMargin
	pctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(pctx, s.cfg.Binary, bridgespec.ProbeArg)
	cmd.Env = append([]string{"GOMEMLIMIT=" + s.cfg.MemLimit}, s.cfg.Env...)
	cmd.Dir = "/"
	cmd.Stdin = bytes.NewReader(body)
	stdout, stderr := &capped{max: maxProbeOutput}, &capped{max: maxProbeOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	if s.cfg.Credential != nil {
		applyCredential(cmd, *s.cfg.Credential)
	}
	runErr := cmd.Run()

	switch {
	case ctx.Err() != nil:
		return none, ctx.Err()
	case pctx.Err() != nil:
		return none, fmt.Errorf("the bridge program took longer than %s to try the outbound, and was stopped", limit)
	case errors.Is(runErr, exec.ErrNotFound) || errors.Is(runErr, fs.ErrNotExist):
		return none, fmt.Errorf("%w: %v", ErrNoProgram, runErr)
	}
	if res, ok := lastResult(stdout.b.Bytes()); ok {
		return res, nil
	}
	why := "it printed no result"
	if runErr != nil {
		why = runErr.Error()
	}
	if tail := lastLine(stderr.b.String()); tail != "" {
		why += ": " + tail
	}
	return none, fmt.Errorf("the bridge program did not say how the try went (%s)", why)
}

// lastResult is the last line of the output that reads as a result. The
// result is the program's last word; what comes before it may be a log.
func lastResult(out []byte) (bridgespec.ProbeResult, bool) {
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if !bytes.HasPrefix(line, []byte("{")) {
			continue
		}
		var res bridgespec.ProbeResult
		if json.Unmarshal(line, &res) == nil {
			return res, true
		}
	}
	return bridgespec.ProbeResult{}, false
}

// lastLine is the last line of something printed, cut to a length a message
// can carry.
func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// capped keeps the first max bytes written to it and drops the rest, so a
// program that prints without end can't fill the hub's memory, and never
// fails a write, so it isn't stopped by a broken pipe either.
type capped struct {
	b   bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.b.Len(); room > 0 {
		c.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
