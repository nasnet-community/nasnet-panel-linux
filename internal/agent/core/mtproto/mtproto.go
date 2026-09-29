package mtproto

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/core"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/traffic"
	"github.com/nasnet-community/nasnet-panel-linux/internal/coredeps"
)

// Core is the agent-side MTProto (telemt) protocol core. It supervises one
// telemt relay process per core_inbound, renders a TOML config, and accounts
// traffic by scraping telemt's loopback Prometheus endpoint. Identity is
// email=secret, embedded directly in the config, and telemt enforces the device
// (unique-IP) limit natively.
type Core struct {
	dataDir string
	dialer  *core.Dialer
	// useRelay routes telemt's Telegram connections through a loopback SOCKS5
	// relay on the tunnel dialer (set when the node marks tunnel egress).
	useRelay bool
	// relayUser/relayPass guard the relays; generated once per start.
	relayUser, relayPass string

	mu       sync.Mutex
	inbounds map[string]*instance
}

type instance struct {
	dir         string
	relay       *relay
	metricsPort int
	coreHash    string
	cmd         *exec.Cmd
	waitCh      chan struct{}
	logs        *ringWriter // last daemon stdout/stderr (diagnostics)
	lastErr     string

	// Prometheus cumulative-counter baselines for per-email delta accounting.
	baseUp   map[string]uint64
	baseDown map[string]uint64
}

// New builds the MTProto core rooted at dataDir. useRelay sends telemt's
// Telegram connections through a loopback relay on dialer. The telemt binary
// is resolved at each start, so one installed later is picked up.
func New(dataDir string, dialer *core.Dialer, useRelay bool) *Core {
	return &Core{
		dataDir:   dataDir,
		dialer:    dialer,
		useRelay:  useRelay,
		relayUser: "telemt",
		relayPass: randHex(16),
		inbounds:  make(map[string]*instance),
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Type implements core.Core.
func (c *Core) Type() core.CoreType { return core.CoreTypeMTProto }

func (c *Core) inboundDir(id string) string {
	return filepath.Join(c.dataDir, "mtproto", id)
}

// Apply reconciles one telemt inbound to the desired state (idempotent). The
// config file is ALWAYS rewritten so telemt inotify-reloads its [access.*] user
// set live; the process is (re)started only when the restart-sensitive core
// section changed or the process is not running.
func (c *Core) Apply(ctx context.Context, d core.DesiredState) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !d.Enabled {
		c.teardownLocked(d.InboundID)
		return nil
	}

	s, err := parseSettings(d.Settings)
	if err != nil {
		return err
	}
	dir := c.inboundDir(d.InboundID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mtproto: mkdir: %w", err)
	}

	in := c.renderInput(d, dir, s)
	res, err := Render(in)
	if err != nil {
		return err
	}
	cfgPath := in.configPath()
	// Always (re)write config.toml so telemt reloads [access.*] without a restart.
	if err := os.WriteFile(cfgPath, []byte(res.Config()), 0o600); err != nil {
		return fmt.Errorf("mtproto: write %s: %w", cfgPath, err)
	}

	inst := c.inbounds[d.InboundID]
	if inst == nil {
		inst = &instance{
			dir:      dir,
			baseUp:   make(map[string]uint64),
			baseDown: make(map[string]uint64),
		}
		c.inbounds[d.InboundID] = inst
	}
	inst.metricsPort = in.metricsPort()

	// telemt refuses to start with an empty user set ("No users configured"). A
	// tunnel with no users yet is not an error: the config is already on disk, so
	// just ensure telemt is stopped and idle. The next reconcile — once a user is
	// added (e.g. via a subscription) — will start it (coreHash reset forces it).
	if len(in.Users) == 0 {
		c.stopProcess(inst)
		inst.relay.close()
		inst.relay = nil
		inst.coreHash = ""
		inst.lastErr = ""
		return nil
	}

	// The relay comes up before telemt dials through it; a changed port
	// (settings) moves it.
	if err := c.ensureRelay(inst, in.RelayAddr); err != nil {
		inst.lastErr = err.Error()
		return err
	}

	// Restart only when the restart-sensitive core section changed or the process
	// is not running. Access-only changes were already flushed to disk above.
	coreHash := hashString(res.Core)
	if inst.cmd != nil && inst.coreHash == coreHash && c.running(inst) {
		return nil
	}
	if err := c.startLocked(inst, cfgPath); err != nil {
		inst.lastErr = err.Error()
		return err
	}
	inst.coreHash = coreHash
	inst.lastErr = ""
	return nil
}

// renderInput builds the renderer input for d, pointing telemt at the
// inbound's relay when the node marks tunnel egress.
func (c *Core) renderInput(d core.DesiredState, dir string, s Settings) RenderInput {
	in := RenderInput{
		InboundID: d.InboundID,
		Listen:    d.Listen,
		Port:      d.Port,
		Dir:       dir,
		Settings:  s,
		Users:     toUsers(d.Users),
	}
	if c.useRelay {
		in.RelayAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(in.relayPort()))
		in.RelayUser, in.RelayPass = c.relayUser, c.relayPass
	}
	return in
}

// ensureRelay runs the inbound's relay on addr ("" = none).
func (c *Core) ensureRelay(inst *instance, addr string) error {
	if inst.relay != nil && inst.relay.addr() == addr {
		return nil
	}
	inst.relay.close()
	inst.relay = nil
	if addr == "" {
		return nil
	}
	r, err := startRelay(addr, c.relayUser, c.relayPass, c.dialer)
	if err != nil {
		return err
	}
	inst.relay = r
	return nil
}

func (c *Core) startLocked(inst *instance, cfgPath string) error {
	c.stopProcess(inst)
	bin, ok := coredeps.ResolveBin("telemt", coredeps.TelemtPaths...)
	if !ok {
		return fmt.Errorf("mtproto: telemt is not installed on this node")
	}
	// telemt runs in the foreground reading its TOML config (passed positionally
	// to the `run` subcommand — telemt has no `--config` flag); it inotify-watches
	// the file for live [access.*] reloads.
	cmd := exec.Command(bin, "run", cfgPath)
	cmd.Dir = inst.dir
	setProcAttrs(cmd)
	rb := &ringWriter{max: 8192}
	cmd.Stdout = rb
	cmd.Stderr = rb
	inst.logs = rb
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mtproto: start telemt: %w", err)
	}
	inst.cmd = cmd
	inst.waitCh = make(chan struct{})
	go func(ch chan struct{}) {
		_ = cmd.Wait()
		close(ch)
	}(inst.waitCh)

	// Give it a moment to fail fast on a bad start (bind conflict, bad config).
	select {
	case <-inst.waitCh:
		return fmt.Errorf("mtproto: telemt exited immediately: %s", rb.String())
	case <-time.After(400 * time.Millisecond):
	}
	return nil
}

func (c *Core) running(inst *instance) bool {
	if inst == nil || inst.cmd == nil || inst.waitCh == nil {
		return false
	}
	select {
	case <-inst.waitCh:
		return false
	default:
		return true
	}
}

func (c *Core) stopProcess(inst *instance) {
	if inst.cmd == nil || inst.cmd.Process == nil {
		return
	}
	_ = inst.cmd.Process.Signal(os.Interrupt)
	select {
	case <-inst.waitCh:
	case <-time.After(3 * time.Second):
		_ = inst.cmd.Process.Kill()
	}
	inst.cmd = nil
}

func (c *Core) teardownLocked(id string) {
	inst := c.inbounds[id]
	if inst == nil {
		return
	}
	c.stopProcess(inst)
	inst.relay.close()
	delete(c.inbounds, id)
}

// Shutdown stops every telemt and relay.
func (c *Core) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range c.inbounds {
		c.teardownLocked(id)
	}
	return nil
}

// Status implements core.Core (aggregate across inbounds). telemt exposes no
// reliable live-session count, so LiveSessions is best-effort 0.
func (c *Core) Status(ctx context.Context) (core.CoreStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := core.CoreStatus{Version: "telemt"}
	for id, inst := range c.inbounds {
		st.InboundIDs = append(st.InboundIDs, id)
		if c.running(inst) {
			st.Running = true
		}
		if inst.lastErr != "" {
			st.Detail = inst.lastErr
		}
	}
	sort.Strings(st.InboundIDs)
	return st, nil
}

// CollectTraffic implements core.Core: scrape telemt's Prometheus endpoint and
// emit per-email deltas since the last collect.
func (c *Core) CollectTraffic(ctx context.Context) (*traffic.XrayStatsSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	snap := &traffic.XrayStatsSnapshot{
		UserUplink:   make(map[string]int64),
		UserDownlink: make(map[string]int64),
	}
	for _, inst := range c.inbounds {
		if !c.running(inst) {
			continue
		}
		text, err := scrapeMetrics(ctx, inst.metricsPort)
		if err != nil {
			continue
		}
		up := parsePromMetric(text, "telemt_user_octets_from_client")
		down := parsePromMetric(text, "telemt_user_octets_to_client")
		accumulateDelta(snap.UserUplink, inst.baseUp, up)
		accumulateDelta(snap.UserDownlink, inst.baseDown, down)
	}
	for _, v := range snap.UserUplink {
		snap.TotalUplink += v
	}
	for _, v := range snap.UserDownlink {
		snap.TotalDownlink += v
	}
	return snap, nil
}

// Sweep implements core.Core: re-render the config from d (reflecting current
// enabled flags / K-limits / secrets) and rewrite it WITHOUT a restart. telemt's
// inotify watcher reconciles disabled users and quota changes live — no ppp/
// iface/session-kill mechanics exist for a userspace TCP relay.
func (c *Core) Sweep(ctx context.Context, d core.DesiredState) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !d.Enabled {
		return nil
	}
	inst := c.inbounds[d.InboundID]
	if inst == nil {
		return nil
	}
	s, err := parseSettings(d.Settings)
	if err != nil {
		return err
	}
	in := c.renderInput(d, inst.dir, s)
	res, err := Render(in)
	if err != nil {
		return err
	}
	if err := os.WriteFile(in.configPath(), []byte(res.Config()), 0o600); err != nil {
		return fmt.Errorf("mtproto: sweep write %s: %w", in.configPath(), err)
	}
	return nil
}

// --- traffic helpers ---

// scrapeMetrics fetches telemt's Prometheus text endpoint (3s timeout, 8MB cap).
func scrapeMetrics(ctx context.Context, port int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	url := fmt.Sprintf("http://127.0.0.1:%d/metrics", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mtproto: metrics status %d", resp.StatusCode)
	}
	const maxBody = 8 << 20 // 8MB
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// accumulateDelta folds cumulative Prometheus counters into per-email deltas,
// updating the baseline map in place. A counter that went backwards (telemt
// restart) is treated as a fresh baseline: the current value is emitted as the
// delta. Mirrors internal/agent/traffic/collector.go diffMap.
func accumulateDelta(dst map[string]int64, base, cur map[string]uint64) {
	for email, c := range cur {
		p, seen := base[email]
		var delta uint64
		switch {
		case !seen:
			delta = c // first observation: full cumulative is the delta
		case c < p:
			delta = c // counter reset (daemon restart): current is the new baseline
		default:
			delta = c - p
		}
		base[email] = c
		if delta > 0 {
			dst[email] += int64(delta)
		}
	}
}

// parsePromMetric parses Prometheus text-format output for metricName and
// returns the summed value keyed by the `user` label. It ignores comment (#)
// lines, other metrics, and unlabelled samples; a metric whose name is a prefix
// of metricName-with-a-suffix does not match (the sample must be `metricName{`).
// Pure and unit-testable.
func parsePromMetric(text, metricName string) map[string]uint64 {
	out := make(map[string]uint64)
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, metricName) {
			continue
		}
		rest := line[len(metricName):]
		if !strings.HasPrefix(rest, "{") {
			continue // unlabelled sample or a longer metric name
		}
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			continue
		}
		user, ok := labelValue(rest[1:end], "user")
		if !ok {
			continue
		}
		valStr := strings.TrimSpace(rest[end+1:])
		if sp := strings.IndexByte(valStr, ' '); sp >= 0 {
			valStr = valStr[:sp] // drop an optional trailing timestamp
		}
		f, err := strconv.ParseFloat(valStr, 64)
		if err != nil || f < 0 {
			continue
		}
		out[user] += uint64(f)
	}
	return out
}

// labelValue extracts the value of key from a Prometheus label set such as
// `user="alice@x",mode="tls"`, handling escaped quotes/backslashes in values.
func labelValue(labels, key string) (string, bool) {
	i := 0
	for i < len(labels) {
		eq := strings.IndexByte(labels[i:], '=')
		if eq < 0 {
			return "", false
		}
		k := strings.TrimSpace(labels[i : i+eq])
		j := i + eq + 1
		if j >= len(labels) || labels[j] != '"' {
			return "", false
		}
		j++ // skip opening quote
		var sb strings.Builder
		for j < len(labels) {
			ch := labels[j]
			if ch == '\\' && j+1 < len(labels) {
				sb.WriteByte(labels[j+1])
				j += 2
				continue
			}
			if ch == '"' {
				break
			}
			sb.WriteByte(ch)
			j++
		}
		if k == key {
			return sb.String(), true
		}
		j++ // skip closing quote
		if j < len(labels) && labels[j] == ',' {
			j++
		}
		i = j
	}
	return "", false
}

// --- misc helpers ---

func toUsers(users []core.UserSpec) []User {
	out := make([]User, 0, len(users))
	for _, u := range users {
		email := u.Email
		if email == "" {
			email = u.Username
		}
		out = append(out, User{
			Email:            email,
			Secret:           u.Secret,
			KLimit:           u.KLimit,
			Enabled:          u.Enabled,
			ExpiresAt:        u.ExpiresAt,
			RateLimitUpBps:   u.RateLimitUpBps,
			RateLimitDownBps: u.RateLimitDownBps,
		})
	}
	return out
}

func hashString(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// ringWriter keeps the last max bytes written (for Status diagnostics).
type ringWriter struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (r *ringWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.max {
		r.buf = r.buf[len(r.buf)-r.max:]
	}
	return len(p), nil
}

func (r *ringWriter) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}
