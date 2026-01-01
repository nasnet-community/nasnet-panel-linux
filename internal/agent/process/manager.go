package process

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

// XrayManager manages the xray-core process lifecycle
type XrayManager struct {
	binPath    string
	configPath string
	apiAddr    string
	dataDir    string // Directory containing geoip.dat/geosite.dat (falls back to config dir)

	cmd       *exec.Cmd
	pid       int
	startTime time.Time
	mu        sync.RWMutex

	// Watchdog settings
	watchdogEnabled bool
	restartDelay    time.Duration
	maxRestarts     int
	restartWindow   time.Duration
	restartHistory  []time.Time

	// Log capture
	captureOutput    bool
	logBuffer        *RingBuffer
	logChan          chan string
	journalStreaming bool          // true when streamJournalLogs goroutine is active
	journalCmd       *exec.Cmd     // current journalctl process (nil when not streaming)
	journalDone      chan struct{} // closed when streamJournalLogs goroutine exits

	// Config Cache
	lastConfigHash    string
	lastConfigTime    time.Time
	lastConfigContent []byte

	// Service Mode settings
	serviceMode bool
	serviceName string

	// Shutdown handling
	stopChan chan struct{}
	stopOnce sync.Once
	stopped  bool
}

// Config for creating a new XrayManager
type Config struct {
	BinaryPath      string
	ConfigPath      string
	APIAddr         string
	DataDir         string // Directory containing geoip.dat/geosite.dat (falls back to config dir)
	WatchdogEnabled bool
	RestartDelay    time.Duration
	MaxRestarts     int
	RestartWindow   time.Duration
	CaptureOutput   bool
	BufferSize      int
	ServiceMode     bool
	ServiceName     string
}

// NewXrayManager creates a new process manager for xray-core
func NewXrayManager(cfg Config) *XrayManager {
	bufSize := cfg.BufferSize
	if bufSize <= 0 {
		bufSize = 1000
	}

	return &XrayManager{
		binPath:         cfg.BinaryPath,
		configPath:      cfg.ConfigPath,
		apiAddr:         cfg.APIAddr,
		dataDir:         cfg.DataDir,
		watchdogEnabled: cfg.WatchdogEnabled,
		restartDelay:    cfg.RestartDelay,
		maxRestarts:     cfg.MaxRestarts,
		restartWindow:   cfg.RestartWindow,
		captureOutput:   cfg.CaptureOutput,
		logBuffer:       NewRingBuffer(bufSize),
		logChan:         make(chan string, 100),
		stopChan:        make(chan struct{}),
		restartHistory:  make([]time.Time, 0),
		serviceMode:     cfg.ServiceMode,
		serviceName:     cfg.ServiceName,
	}
}

// assetDir returns the directory xray-core should use for geoip.dat/geosite.dat.
// It uses the explicitly configured DataDir only if it actually contains geofiles,
// otherwise falls back to the config directory (where the agent writes geofiles).
func (m *XrayManager) assetDir() string {
	if m.dataDir != "" {
		if _, err := os.Stat(filepath.Join(m.dataDir, "geosite.dat")); err == nil {
			return m.dataDir
		}
		if _, err := os.Stat(filepath.Join(m.dataDir, "geoip.dat")); err == nil {
			return m.dataDir
		}
	}
	return filepath.Dir(m.configPath)
}

// ensureServiceEnv creates a systemd drop-in override that sets XRAY_LOCATION_ASSET
// so xray-core can find geoip.dat and geosite.dat when running as a systemd service.
func (m *XrayManager) ensureServiceEnv() error {
	dropInDir := filepath.Join("/etc/systemd/system", m.serviceName+".service.d")
	dropInFile := filepath.Join(dropInDir, "asset-location.conf")

	assetDir := m.assetDir()
	desired := fmt.Sprintf("[Service]\nEnvironment=\"XRAY_LOCATION_ASSET=%s\"\n", assetDir)

	// Check if drop-in already has the correct content
	if existing, err := os.ReadFile(dropInFile); err == nil && string(existing) == desired {
		return nil
	}

	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		return fmt.Errorf("failed to create drop-in directory %s: %w", dropInDir, err)
	}

	if err := os.WriteFile(dropInFile, []byte(desired), 0644); err != nil {
		return fmt.Errorf("failed to write drop-in %s: %w", dropInFile, err)
	}

	// Reload systemd so it picks up the drop-in
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload failed: %s, %w", string(out), err)
	}

	logrus.WithFields(logrus.Fields{
		"service":   m.serviceName,
		"asset_dir": assetDir,
	}).Info("Created systemd drop-in for XRAY_LOCATION_ASSET")

	return nil
}

// Start begins the xray-core process
func (m *XrayManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunningLocked() {
		// Xray is already running — ensure log capture is active
		m.ensureLogCaptureLocked()
		return nil
	}

	return m.startLocked()
}

// startLocked starts the process (must hold lock)
func (m *XrayManager) startLocked() error {
	// Clear stale unix-socket inodes from a previous run so bind() does
	// not fail with EADDRINUSE when xray is restarted after a hard kill.
	m.cleanupStaleUnixSocketsLocked()

	if m.serviceMode {
		logrus.WithField("service", m.serviceName).Info("Starting Xray via systemd")

		// Ensure XRAY_LOCATION_ASSET is set via systemd drop-in so xray can find geofiles
		if err := m.ensureServiceEnv(); err != nil {
			logrus.WithError(err).Warn("Failed to set XRAY_LOCATION_ASSET drop-in (geofile rules may not work)")
		}

		cmd := exec.Command("systemctl", "start", m.serviceName)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to start service %s: %s, %w", m.serviceName, string(output), err)
		}

		m.startTime = time.Now()
		m.stopped = false

		// Guard against duplicate streamers: each failed start retry was leaking a journalctl follower.
		if m.captureOutput && !m.journalStreaming {
			m.journalStreaming = true
			m.journalDone = make(chan struct{})
			go m.streamJournalLogs()
		}

		// In service mode, systemd is responsible for restart-on-failure via
		// its unit directive. Running the internal watchdog in parallel races
		// with systemd and doubles restart attempts, so we suppress it.
		if m.watchdogEnabled {
			logrus.Info("Watchdog suppressed in service mode; systemd owns restart policy")
		}
		return nil
	}

	// Verify binary exists
	if _, err := os.Stat(m.binPath); os.IsNotExist(err) {
		return fmt.Errorf("xray binary not found at %s", m.binPath)
	}

	// Verify config exists
	if _, err := os.Stat(m.configPath); os.IsNotExist(err) {
		return fmt.Errorf("xray config not found at %s", m.configPath)
	}

	m.cmd = exec.Command(m.binPath, "run", "-c", m.configPath)
	// Set working directory to config directory so relative paths in config work
	configDir := filepath.Dir(m.configPath)
	m.cmd.Dir = configDir
	// Set asset location so Xray finds geoip.dat/geosite.dat
	m.cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+m.assetDir())

	// Set up output capture
	if m.captureOutput {
		stdout, err := m.cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to create stdout pipe: %w", err)
		}
		stderr, err := m.cmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("failed to create stderr pipe: %w", err)
		}

		go m.captureLines(stdout, "stdout")
		go m.captureLines(stderr, "stderr")
	} else {
		m.cmd.Stdout = os.Stdout
		m.cmd.Stderr = os.Stderr
	}

	if err := m.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start xray: %w", err)
	}

	m.pid = m.cmd.Process.Pid
	m.startTime = time.Now()
	m.stopped = false

	logrus.WithFields(logrus.Fields{
		"pid":    m.pid,
		"config": m.configPath,
	}).Info("Xray process started")

	// Start watchdog
	if m.watchdogEnabled {
		go m.watchdog()
	}

	return nil
}

// captureLines: stores lines in the ring buffer + forwards to the web-UI
// log channel. Not re-logged to logrus (syslog flood). Only warnings/errors
// hit logrus, rate-limited.
func (m *XrayManager) captureLines(r io.Reader, source string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		text := scanner.Text()
		line := fmt.Sprintf("[%s] %s", source, text)
		m.logBuffer.Write(line)

		// Only log warnings and errors through logrus — not every line
		if len(text) > 20 {
			fields := logrus.Fields{
				"component": "xray-core",
				"source":    source,
			}
			if contains(text, "[Warning]") {
				logrus.WithFields(fields).Warn(text)
			} else if contains(text, "[Error]") {
				logrus.WithFields(fields).Error(text)
			}
		}

		// Non-blocking send to log channel
		select {
		case m.logChan <- line:
		default:
		}
	}
}

// ensureLogCaptureLocked starts log capture if not already active (must hold lock)
func (m *XrayManager) ensureLogCaptureLocked() {
	if !m.captureOutput {
		return
	}
	if m.serviceMode && !m.journalStreaming {
		logrus.Info("Xray already running, starting journal log capture")
		m.journalStreaming = true
		m.journalDone = make(chan struct{})
		go m.streamJournalLogs()
	}
}

// streamJournalLogs streams logs from journalctl
func (m *XrayManager) streamJournalLogs() {
	defer func() {
		m.mu.Lock()
		m.journalCmd = nil
		m.journalStreaming = false
		if m.journalDone != nil {
			close(m.journalDone)
			m.journalDone = nil
		}
		m.mu.Unlock()
	}()

	cmd := exec.Command("journalctl", "-f", "-u", m.serviceName, "--no-pager")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logrus.WithError(err).Error("Failed to create journalctl pipe")
		return
	}

	if err := cmd.Start(); err != nil {
		logrus.WithError(err).Error("Failed to start journalctl")
		return
	}

	m.mu.Lock()
	m.journalCmd = cmd
	m.mu.Unlock()

	// Use existing captureLines logic
	go m.captureLines(stdout, "journald")

	// Wait for stop signal or command exit
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-m.stopChan:
		cmd.Process.Kill()
		<-done // wait for cmd.Wait() to finish
	case <-done:
		// journalctl exited on its own
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// watchdog: process-mode only. In service mode, systemd Restart=on-failure
// owns the policy; watchdog is suppressed to avoid racing systemctl.
func (m *XrayManager) watchdog() {
	for {
		m.mu.RLock()
		stopped := m.stopped
		serviceMode := m.serviceMode
		m.mu.RUnlock()

		if stopped {
			return
		}

		if serviceMode {
			// Safety net: should not reach here because Start suppresses
			// watchdog in service mode. Exit so we never double-drive
			// systemd.
			logrus.Warn("Watchdog running in service mode; exiting to defer to systemd")
			return
		}

		// Process Mode
		m.mu.RLock()
		cmd := m.cmd
		m.mu.RUnlock()

		if cmd == nil {
			return
		}

		// Wait for process to exit
		err := cmd.Wait()

		m.mu.Lock()
		if m.stopped {
			m.mu.Unlock()
			return
		}

		// Process crashed
		logrus.WithError(err).Warn("Xray process exited unexpectedly")

		// release m.mu before canRestart() — it re-acquires the same lock
		m.mu.Unlock()

		// Check restart limit
		if !m.canRestart() {
			logrus.Error("Too many restarts within window, giving up")
			return
		}

		// Wait before restarting — use select so stopChan can interrupt
		select {
		case <-time.After(m.restartDelay):
		case <-m.stopChan:
			return
		}

		m.mu.Lock()
		if m.stopped {
			m.mu.Unlock()
			return
		}

		logrus.Info("Attempting to restart xray...")
		if err := m.startLocked(); err != nil {
			logrus.WithError(err).Error("Failed to restart xray")
		}
		m.mu.Unlock()
	}
}

// canRestart: prune expired entries, enforce per-window limit, record
// attempt — all under m.mu. True if restart should proceed.
func (m *XrayManager) canRestart() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-m.restartWindow)

	recent := m.restartHistory[:0]
	for _, t := range m.restartHistory {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	m.restartHistory = recent

	if len(recent) >= m.maxRestarts {
		return false
	}
	m.restartHistory = append(m.restartHistory, now)
	return true
}

// closeStopChan closes m.stopChan via m.stopOnce. Named helper so
// TestStopIdempotentViaPublicAPI can catch regressions that drop the Do-wrap.
func (m *XrayManager) closeStopChan() {
	m.stopOnce.Do(func() { close(m.stopChan) })
}

// Stop gracefully stops the xray process
func (m *XrayManager) Stop(timeout time.Duration) error {
	m.mu.Lock()

	if !m.isRunningLocked() {
		m.mu.Unlock()
		return nil
	}

	m.stopped = true
	m.closeStopChan()

	// Capture journalDone before releasing lock so we can wait outside the lock
	journalDone := m.journalDone

	if m.serviceMode {
		// Kill journalctl directly if it's still alive (don't rely on stopChan race)
		if m.journalCmd != nil && m.journalCmd.Process != nil {
			m.journalCmd.Process.Kill()
		}
		m.mu.Unlock()

		// Wait for journal goroutine to finish (prevents leak on Restart)
		if journalDone != nil {
			select {
			case <-journalDone:
			case <-time.After(3 * time.Second):
				logrus.Warn("[Stop] Timed out waiting for journal goroutine to exit")
			}
		}

		m.mu.Lock()
		defer m.mu.Unlock()
		logrus.WithField("service", m.serviceName).Info("Stopping Xray service")
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "systemctl", "stop", m.serviceName)
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				logrus.Warn("systemctl stop timed out, force-killing xray service")
				killCmd := exec.Command("systemctl", "kill", "-s", "SIGKILL", m.serviceName)
				return killCmd.Run()
			}
			return err
		}
		return nil
	}

	// Non-service mode: try graceful shutdown first
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil {
		if err := m.cmd.Process.Signal(os.Interrupt); err != nil {
			logrus.WithError(err).Warn("Failed to send SIGINT, trying SIGKILL")
			return m.cmd.Process.Kill()
		}

		// Wait for graceful shutdown with timeout
		done := make(chan error, 1)
		go func() {
			done <- m.cmd.Wait()
		}()

		select {
		case <-done:
			logrus.Info("Xray process stopped gracefully")
			return nil
		case <-time.After(timeout):
			logrus.Warn("Graceful shutdown timed out, killing process")
			return m.cmd.Process.Kill()
		}
	} else {
		// External process running
		logrus.Info("Stopping external Xray process")
		if err := exec.Command("pkill", "-x", "xray").Run(); err != nil {
			logrus.WithError(err).Warn("Failed to stop external xray process")
		}
		// Wait a bit for it to die
		time.Sleep(1 * time.Second)
		return nil
	}
}

// Restart stops and starts the xray process
func (m *XrayManager) Restart(ctx context.Context, validateFirst bool, timeout time.Duration) error {
	// Optionally validate config first
	if validateFirst {
		if err := m.ValidateConfig(); err != nil {
			return fmt.Errorf("config validation failed: %w", err)
		}
	}

	if !m.canRestart() {
		return fmt.Errorf("restart rejected: exceeded %d restarts within %s", m.maxRestarts, m.restartWindow)
	}

	if err := m.Stop(timeout); err != nil {
		logrus.WithError(err).Warn("Error stopping xray (continuing with start)")
	}

	// Install new stopChan before clearing stopOnce: concurrent Stop()
	// must see the new channel, never the old closed one.
	m.mu.Lock()
	m.stopChan = make(chan struct{})
	m.stopOnce = sync.Once{}
	m.stopped = false
	m.mu.Unlock()

	return m.Start()
}

// ValidateConfig runs xray with -test flag to validate config
func (m *XrayManager) ValidateConfig() error {
	cmd := exec.Command(m.binPath, "-test", "-c", m.configPath)
	cmd.Dir = filepath.Dir(m.configPath)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+m.assetDir())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("config validation failed: %s", string(output))
	}
	return nil
}

// IsRunning returns whether the xray process is currently running
func (m *XrayManager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isRunningLocked()
}

func (m *XrayManager) isRunningLocked() bool {
	if m.serviceMode {
		cmd := exec.Command("systemctl", "is-active", m.serviceName)
		return cmd.Run() == nil
	}

	// First check if we started the process ourselves
	if m.cmd != nil && m.cmd.Process != nil {
		// syscall.Kill(pid, 0) returns nil if the process exists, ESRCH if
		// not. os.Process.Signal(nil) has ambiguous cross-platform semantics
		// and was returning "alive" for zombies on some kernels.
		if err := syscall.Kill(m.cmd.Process.Pid, 0); err == nil {
			return true
		}
	}

	// Also check if xray is running externally (e.g., started by systemd)
	// Use pgrep to find any xray process
	cmd := exec.Command("pgrep", "-x", "xray")
	if err := cmd.Run(); err == nil {
		return true // Found an xray process
	}

	return false
}

// PID returns the current process ID
func (m *XrayManager) PID() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.serviceMode {
		cmd := exec.Command("systemctl", "show", "--property", "MainPID", "--value", m.serviceName)
		output, err := cmd.Output()
		if err == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &pid); err == nil && pid > 0 {
				return pid
			}
		}
		return 0
	}

	if m.pid > 0 {
		return m.pid
	}

	// Check for external process
	cmd := exec.Command("pgrep", "-x", "xray")
	output, err := cmd.Output()
	if err == nil {
		var pid int
		if _, err := fmt.Sscanf(string(output), "%d", &pid); err == nil {
			return pid
		}
	}
	return 0
}

// Uptime returns how long the process has been running
func (m *XrayManager) Uptime() time.Duration {
	m.mu.RLock()
	startTime := m.startTime
	m.mu.RUnlock()

	// If we have a tracked start time, use it
	if !startTime.IsZero() {
		return time.Since(startTime)
	}

	// Try to get uptime from external process
	pid := m.PID()
	if pid > 0 {
		// Use ps to get elapsed time in seconds
		cmd := exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "etimes=")
		output, err := cmd.Output()
		if err == nil {
			var seconds int64
			if _, err := fmt.Sscanf(string(output), "%d", &seconds); err == nil {
				return time.Duration(seconds) * time.Second
			}
		}
	}

	return 0
}

// GetConfigContent returns the content of the config file, using cache if unmodified
func (m *XrayManager) GetConfigContent() ([]byte, error) {
	info, err := os.Stat(m.configPath)
	if err != nil {
		return nil, err
	}

	modTime := info.ModTime()

	m.mu.RLock()
	lastContent := m.lastConfigContent
	lastTime := m.lastConfigTime
	m.mu.RUnlock()

	// If modtime hasn't changed, return cached content
	if !lastTime.IsZero() && modTime.Equal(lastTime) && lastContent != nil {
		return lastContent, nil
	}

	// Read file
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return nil, err
	}

	// Update cache
	m.mu.Lock()
	m.lastConfigContent = data
	m.lastConfigTime = modTime
	// Also update hash since we have the data
	hash := md5.Sum(data)
	m.lastConfigHash = hex.EncodeToString(hash[:])
	m.mu.Unlock()

	return data, nil
}

// StartTime returns when the process was started
func (m *XrayManager) StartTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.startTime
}

// ConfigHash returns an MD5 hash of the current config file
func (m *XrayManager) ConfigHash() (string, error) {
	// Re-use GetConfigContent to ensure consistency and single cache source
	_, err := m.GetConfigContent()
	if err != nil {
		return "", err
	}

	m.mu.RLock()
	hash := m.lastConfigHash
	m.mu.RUnlock()

	return hash, nil
}

// UpdateConfig writes new config content to disk
func (m *XrayManager) UpdateConfig(content string) error {
	logger := logrus.WithFields(logrus.Fields{
		"config_path": m.configPath,
		"size_bytes":  len(content),
	})

	// Ensure log directories exist so xray can open log files
	ensureLogDirs(content, logger)

	logger.Debug("Writing new config to temp file")

	// Write to temp file first
	// Note: Must end in .json for Xray to detect format correctly
	tmpPath := m.configPath + ".tmp.json"
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		logger.WithError(err).Error("Failed to write temp config")
		return fmt.Errorf("failed to write temp config: %w", err)
	}

	logger.Debug("Validating new config")

	// Validate the new config
	cmd := exec.Command(m.binPath, "-test", "-c", tmpPath)
	cmd.Dir = filepath.Dir(m.configPath)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+m.assetDir())
	output, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(tmpPath)
		logger.WithError(err).WithField("output", string(output)).Error("Config validation failed")
		return fmt.Errorf("config validation failed: %s", string(output))
	}

	logger.Debug("Config validation passed, performing atomic update")

	// Atomic rename
	if err := os.Rename(tmpPath, m.configPath); err != nil {
		os.Remove(tmpPath)
		logger.WithError(err).Error("Failed to perform atomic rename")
		return fmt.Errorf("failed to update config: %w", err)
	}

	// Update cache immediately to prevent stale reads
	m.mu.Lock()
	data := []byte(content)
	m.lastConfigContent = data
	info, _ := os.Stat(m.configPath)
	if info != nil {
		m.lastConfigTime = info.ModTime()
	} else {
		m.lastConfigTime = time.Now() // Fallback near-enough time
	}
	hash := md5.Sum(data)
	hashStr := hex.EncodeToString(hash[:])
	m.lastConfigHash = hashStr
	m.mu.Unlock()

	logger.WithField("new_hash", hashStr).Info("Config updated successfully")
	return nil
}

// listenedUnixSocketPaths: filesystem paths of unix-socket inbound.listen
// entries. Skips IP/hostname listens and abstract sockets ('@' prefix).
// Normalises xray's "path,<mode>" form to just the path.
func listenedUnixSocketPaths(configJSON []byte) []string {
	var cfg struct {
		Inbounds []struct {
			Listen string `json:"listen"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil
	}
	var paths []string
	for _, in := range cfg.Inbounds {
		v := strings.TrimSpace(in.Listen)
		if !strings.HasPrefix(v, "/") {
			continue
		}
		if i := strings.Index(v, ","); i >= 0 {
			v = v[:i]
		}
		v = strings.TrimSpace(v)
		if v != "" {
			paths = append(paths, v)
		}
	}
	return paths
}

// cleanupStaleUnixSocketsLocked unlinks stale unix-socket inodes left by
// abnormal xray shutdown (SIGKILL/crash). Without this, fresh bind()
// hits EADDRINUSE → 78s "Starting xray" retry loop. Only touches socket
// inodes, never regular files.
func (m *XrayManager) cleanupStaleUnixSocketsLocked() {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return
	}
	for _, p := range listenedUnixSocketPaths(data) {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSocket == 0 {
			continue
		}
		if err := os.Remove(p); err != nil {
			logrus.WithError(err).WithField("path", p).Warn("Failed to remove stale unix socket before xray start")
			continue
		}
		logrus.WithField("path", p).Info("Removed stale unix socket before xray start")
	}
}

// ensureLogDirs parses the xray config JSON and creates parent directories
// for access/error log files so that xray can open them on startup.
func ensureLogDirs(configJSON string, logger *logrus.Entry) {
	var cfg struct {
		Log struct {
			Access string `json:"access"`
			Error  string `json:"error"`
		} `json:"log"`
	}
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return
	}
	for _, p := range []string{cfg.Log.Access, cfg.Log.Error} {
		if p == "" {
			continue
		}
		dir := filepath.Dir(p)
		if err := os.MkdirAll(dir, 0755); err != nil {
			logger.WithError(err).WithField("dir", dir).Warn("Failed to create log directory")
		}
	}
}

// TailLogs returns the last n lines from the log buffer
func (m *XrayManager) TailLogs(n int) []string {
	return m.logBuffer.Tail(n)
}

// LogChannel returns the channel for streaming logs
func (m *XrayManager) LogChannel() <-chan string {
	return m.logChan
}

// BinaryPath returns the path to the xray binary
func (m *XrayManager) BinaryPath() string {
	return m.binPath
}

// ConfigPath returns the path to the config file
func (m *XrayManager) ConfigPath() string {
	return m.configPath
}

// AssetDir returns the directory xray-core uses for geoip.dat/geosite.dat
func (m *XrayManager) AssetDir() string {
	return m.assetDir()
}
