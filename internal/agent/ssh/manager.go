package ssh

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

var re = regexp.MustCompile(`^Port\s+(\d+)`)

// Config holds SSH manager configuration
type Config struct {
	ConfigPath  string // Default /etc/ssh/sshd_config
	ServiceName string // Default ssh or sshd
	LogPath     string // Default /var/log/auth.log or /var/log/secure
}

// Manager handles SSH service operations
type Manager struct {
	cfg Config
	mu  sync.Mutex
}

// NewManager creates a new SSH manager
func NewManager(cfg Config) *Manager {
	if cfg.ConfigPath == "" {
		cfg.ConfigPath = "/etc/ssh/sshd_config"
	}
	if cfg.ServiceName == "" {
		// Detect service name
		// Default to ssh
		cfg.ServiceName = "ssh"

		if _, err := exec.LookPath("systemctl"); err == nil {
			// Check sshd first (often the real service)
			if output, err := exec.Command("systemctl", "is-enabled", "sshd").Output(); err == nil {
				status := strings.TrimSpace(string(output))
				if status == "enabled" || status == "alias" || status == "linked" || status == "static" {
					cfg.ServiceName = "sshd"
				}
			} else {
				// If sshd check failed (e.g. not found), check ssh
				if output, err := exec.Command("systemctl", "is-enabled", "ssh").Output(); err == nil {
					status := strings.TrimSpace(string(output))
					if status == "enabled" || status == "alias" || status == "linked" || status == "static" {
						cfg.ServiceName = "ssh"
					}
				} else {
					// Both check failed or are disabled/masked.
					// Fallback to checking which one exists/isActive
					if err := exec.Command("systemctl", "status", "sshd").Run(); err == nil {
						cfg.ServiceName = "sshd"
					}
				}
			}
		}
	}
	if cfg.LogPath == "" {
		// Detect log path
		if _, err := os.Stat("/var/log/auth.log"); err == nil {
			cfg.LogPath = "/var/log/auth.log"
		} else if _, err := os.Stat("/var/log/secure"); err == nil {
			cfg.LogPath = "/var/log/secure"
		} else {
			cfg.LogPath = "/var/log/auth.log"
		}
	}

	return &Manager{
		cfg: cfg,
	}
}

// GetStatus returns the current status of the SSH service
func (m *Manager) GetStatus() (bool, int, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if active
	isActive := false
	if output, err := exec.Command("systemctl", "is-active", m.cfg.ServiceName).Output(); err == nil {
		if strings.TrimSpace(string(output)) == "active" {
			isActive = true
		}
	}

	// Check if enabled (we now treat "active" as enabled for UI simplicity)
	isEnabled := isActive

	/*
		// Old logic checking persistence
		if output, err := exec.Command("systemctl", "is-enabled", m.cfg.ServiceName).Output(); err == nil {
			status := strings.TrimSpace(string(output))
			// enabled, alias, linked, static are all considered "enabled"
			if status == "enabled" || status == "alias" || status == "linked" || status == "static" {
				isEnabled = true
			}
		}
	*/

	// Get port from config
	port := 22
	f, err := os.Open(m.cfg.ConfigPath)
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "#") {
				continue
			}
			matches := re.FindStringSubmatch(line)
			if len(matches) == 2 {
				p, err := strconv.Atoi(matches[1])
				if err == nil {
					port = p
					break
				}
			}
		}
	} else {
		logrus.Warnf("Failed to read ssh config: %v", err)
	}

	return isEnabled, port, isActive, nil
}

// UpdateConfig updates the SSH configuration
func (m *Manager) UpdateConfig(enabled bool, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Update Port in config
	if port > 0 && port < 65536 {
		if err := m.updatePortInFile(port); err != nil {
			return fmt.Errorf("failed to update port in config: %w", err)
		}
	}

	// 2. Validate config
	// Ensure privilege separation directory exists (required for sshd -t validation on some systems)
	if _, err := os.Stat("/run/sshd"); os.IsNotExist(err) {
		_ = os.MkdirAll("/run/sshd", 0755)
	}

	if output, err := exec.Command("sshd", "-t").CombinedOutput(); err != nil {
		return fmt.Errorf("invalid sshd config: %s", string(output))
	}

	// 3. Start/Stop service

	// We use "restart" if enabling, just to be sure it picks up config changes
	// But if we are disabling, just stop it.
	if enabled {
		// Try to enable it for persistence, but don't fail if it doesn't work (e.g. docker container)
		_ = exec.Command("systemctl", "enable", m.cfg.ServiceName).Run()

		if err := exec.Command("systemctl", "restart", m.cfg.ServiceName).Run(); err != nil {
			return fmt.Errorf("failed to start/restart ssh service: %w", err)
		}
	} else {
		// Try to disable for persistence
		_ = exec.Command("systemctl", "disable", m.cfg.ServiceName).Run()

		if err := exec.Command("systemctl", "stop", m.cfg.ServiceName).Run(); err != nil {
			return fmt.Errorf("failed to stop ssh service: %w", err)
		}
	}

	return nil
}

// Restart restarts the SSH service
func (m *Manager) Restart() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return exec.Command("systemctl", "restart", m.cfg.ServiceName).Run()
}

// updatePortInFile updates the Port directive in sshd_config
func (m *Manager) updatePortInFile(newPort int) error {
	content, err := os.ReadFile(m.cfg.ConfigPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	found := false

	var newLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Port ") {
			newLines = append(newLines, fmt.Sprintf("Port %d", newPort))
			found = true
		} else {
			newLines = append(newLines, line)
		}
	}

	if !found {
		// Add Port directive if not found
		// Try to insert after 'Include /etc/ssh/sshd_config.d/*.conf' if it exists, otherwise at the top
		insertIdx := 0
		for i, line := range newLines {
			if strings.Contains(line, "Include") {
				insertIdx = i + 1
				break
			}
		}

		// Insert
		newLines = append(newLines[:insertIdx+1], newLines[insertIdx:]...)
		newLines[insertIdx] = fmt.Sprintf("Port %d", newPort)
	}

	return os.WriteFile(m.cfg.ConfigPath, []byte(strings.Join(newLines, "\n")), 0644)
}

// ClearLogs clears the SSH auth logs
func (m *Manager) ClearLogs() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Truncate the file
	if err := os.Truncate(m.cfg.LogPath, 0); err != nil {
		return fmt.Errorf("failed to truncate log file %s: %w", m.cfg.LogPath, err)
	}

	return nil
}
