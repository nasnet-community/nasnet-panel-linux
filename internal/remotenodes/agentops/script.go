// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// Where things go on a node. They are what the Linux panel's own node install
// writes (internal/node/handler/deploy_script.go), so an agent installed by
// this plugin is found where an agent is expected, and the agent's defaults
// still apply.
const (
	agentBin      = "/usr/local/bin/nasnet-agent"
	agentUpload   = "/usr/local/bin/.nasnet-agent.upload" // beside the binary, so the final move is a rename
	agentUnit     = "nasnet-agent"
	agentUnitPath = "/etc/systemd/system/nasnet-agent.service"

	confDir    = "/etc/nasnet-agent"
	configPath = confDir + "/config.yaml"
	caPath     = confDir + "/ca.crt"
	certPath   = confDir + "/server.crt"
	keyPath    = confDir + "/server.key"
	dataDir    = "/var/lib/nasnet-agent"
	logDir     = "/var/log/nasnet-agent"

	// markerPath says what the install put on the node, so that Uninstall
	// takes away that and nothing else. It sits in the agent's data directory
	// and goes with it, last.
	markerPath = dataDir + "/remote-nodes.json"

	xrayBin      = "/usr/local/bin/xray"
	xrayConfDir  = "/usr/local/etc/xray"
	xrayConfPath = xrayConfDir + "/config.json"
	xrayUnitPath = "/etc/systemd/system/xray.service"

	// xrayDropIn is the agent's own doing: in service mode it points Xray at
	// its data files with a drop-in, each time it starts Xray.
	xrayDropIn    = "/etc/systemd/system/xray.service.d/asset-location.conf"
	xrayDropInDir = "/etc/systemd/system/xray.service.d"

	// The kernel setting the install makes for the tunnel, see tuneKernel:
	// the live value, and the file that keeps it across a reboot.
	retries2Path = "/proc/sys/net/ipv4/tcp_retries2"
	sysctlDir    = "/etc/sysctl.d"
	sysctlPath   = sysctlDir + "/60-nasnet-agent.conf"
)

// How many times the kernel resends what nobody acknowledges before it gives
// a connection up: the install's value, which is about a minute with the usual
// timeouts, and the kernel's own, which is about fifteen minutes.
const (
	retries2Want    = "5"
	retries2Default = "15"
)

// sysctlText is the file that keeps retries2Want across a reboot. Both the
// file and the live value go when the agent does, if nobody changed them.
const sysctlText = `# Written by the Remote Nodes plugin with the agent, and removed with it.
# A tunnel connection whose other end has gone away, such as the router's
# after its address changed, is given up after about a minute and not about
# fifteen. The kernel's default is 15.
net.ipv4.tcp_retries2 = ` + retries2Want + `
`

// shq quotes s for a POSIX shell.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func quoted(paths ...string) string {
	q := make([]string, len(paths))
	for i, p := range paths {
		q[i] = shq(p)
	}
	return strings.Join(q, " ")
}

// The commands the install runs on a node. They are plain and one thing each,
// so a recorded run reads as a script and a test can say exactly what ran.
var (
	cmdUname     = "uname -sm"
	cmdSystemd   = "test -d '/run/systemd/system' && command -v systemctl"
	cmdGzip      = "command -v gzip"
	cmdSHA256    = "command -v sha256sum"
	cmdSudo      = "command -v sudo"
	cmdOSRelease = `. '/etc/os-release' 2>/dev/null && echo "$PRETTY_NAME"`
	cmdCA        = "cat " + shq(caPath) + " 2>/dev/null || true"
	cmdMarker    = "cat " + shq(markerPath) + " 2>/dev/null || true"
	cmdListeners = "ss -ltnp 2>/dev/null"
	cmdProcNet   = "cat /proc/net/tcp 2>/dev/null; cat /proc/net/tcp6 2>/dev/null; true"
	cmdUFW       = "command -v ufw"
	cmdUFWStatus = "ufw status"
	cmdStart     = "systemctl daemon-reload && systemctl enable " + agentUnit + " && systemctl restart " + agentUnit
	cmdIsActive  = "systemctl is-active " + agentUnit
	cmdJournal   = "journalctl -u " + agentUnit + " -n 12 --no-pager 2>&1"

	// cmdUpload unpacks the agent as it arrives, to a file only root can
	// read, so nothing can run a half-written binary.
	cmdUpload = "umask 077 && gzip -dc > " + shq(agentUpload)
	cmdHash   = "sha256sum " + shq(agentUpload)
	// cmdInstallBin makes the upload executable and puts it in place with one
	// rename, so the agent is the old one or the new one and never a part of one.
	cmdInstallBin = "chmod 0755 " + shq(agentUpload) + " && mv -f " + shq(agentUpload) + " " + shq(agentBin)
	// cmdAgentRuns only has to start: it shows a binary that can't run here
	// (a noexec mount, a wrong build) before anything depends on it.
	cmdAgentRuns = shq(agentBin) + " version"

	// cmdReadRetries2 reads the kernel setting as the server has it, and says
	// nothing on a server that doesn't have it.
	cmdReadRetries2 = "cat " + shq(retries2Path) + " 2>/dev/null || true"
	// cmdSetRetries2 makes the install's value live. The same write is what
	// sysctl does, without needing it to be installed.
	cmdSetRetries2 = "echo " + shq(retries2Want) + " > " + shq(retries2Path)
	// cmdMkSysctlDir only makes the folder where a server has none.
	cmdMkSysctlDir = "mkdir -p -m 0755 " + shq(sysctlDir)
)

// cmdPresent names which of the paths the install cares about are there
// already: a foreign Xray, or an agent from an earlier install.
func cmdPresent(paths []string) string {
	return "for f in " + quoted(paths...) + "; do if [ -e \"$f\" ]; then echo \"$f\"; fi; done"
}

// cmdDirs makes directories, readable by everyone and writable by root.
func cmdDirs(paths ...string) string { return "install -d -m 0755 " + quoted(paths...) }

// cmdWrite writes stdin to path with the given mode. The bytes go to a file
// beside it first, which umask keeps from anyone else, and the file is moved
// in once it is whole, so a reader sees all of the old one or all of the new
// one. Secrets travel on stdin and never on the command line.
func cmdWrite(path, mode string) string {
	tmp := path + ".new"
	cmd := "umask 077 && cat > " + shq(tmp)
	if mode != "0600" {
		cmd += " && chmod " + mode + " " + shq(tmp)
	}
	return cmd + " && mv -f " + shq(tmp) + " " + shq(path)
}

func cmdUFWAllow(port int) string { return "ufw allow " + strconv.Itoa(port) + "/tcp" }

func cmdUFWDelete(port int) string {
	return "ufw delete allow " + strconv.Itoa(port) + "/tcp 2>/dev/null || true"
}

// xrayBinSpool is where the agent spools a pushed build before it swaps it in.
const xrayBinSpool = "/usr/local/bin/.xray-push-"

// Uninstall's commands. Each stands on its own and none fails over something
// that is already gone, so a half-installed node comes clean.
var (
	cmdStopAgent = "systemctl disable --now " + agentUnit + " 2>/dev/null || true"
	cmdStopXray  = "systemctl disable --now xray 2>/dev/null || true"
	cmdRmAgent   = "rm -f " + quoted(agentUnitPath, agentUnitPath+".new", agentBin, agentBin+".new", agentBin+".old", agentUpload)
	cmdRmDirs    = "rm -rf " + quoted(confDir, logDir)
	cmdRmXray    = "rm -f " + quoted(xrayBin, xrayBin+".bak") + " " + shq(xrayBinSpool) + "*"
	cmdRmXrayCfg = "rm -f " + quoted(xrayConfPath, xrayConfPath+".tmp.json") + " && rmdir " + shq(xrayConfDir) + " 2>/dev/null || true"
	cmdRmXrayDef = "rm -f " + quoted(xrayUnitPath, xrayUnitPath+".new")
	cmdRmDropIn  = "rm -f " + shq(xrayDropIn) + " && rmdir " + shq(xrayDropInDir) + " 2>/dev/null || true"
	cmdRmData    = "rm -rf " + shq(dataDir)
	cmdRmEmpty   = "rmdir " + quoted(confDir, dataDir, logDir) + " 2>/dev/null || true"
	cmdReload    = "systemctl daemon-reload 2>/dev/null || true"
	cmdRmSysctl  = "rm -f " + quoted(sysctlPath, sysctlPath+".new")
)

// cmdRestoreRetries2 puts the kernel setting back to what it was, but only when
// it still has the value the install gave it: one that someone has changed
// since is theirs. The file that kept it across a reboot is gone by then, so
// without this the value would stay until the server restarts.
func cmdRestoreRetries2(was string) string {
	return "[ \"$(cat " + shq(retries2Path) + " 2>/dev/null)\" = " + shq(retries2Want) + " ] && echo " + shq(was) + " > " + shq(retries2Path) + " || true"
}

// retries2Value is a kernel setting's text if it is a plain number, and ""
// otherwise. What the server says, and what the marker on it says, is not put
// into a command as it is.
func retries2Value(s string) string {
	s = strings.TrimSpace(s)
	if n := len(s); n == 0 || n > 3 {
		return ""
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return s
}

// The paths whose presence the install asks about before it writes anything.
var presentPaths = []string{agentBin, agentUnitPath, xrayBin, xrayUnitPath, xrayConfPath}

// parseUname reads "Linux x86_64".
func parseUname(out string) (osName, machine string) {
	f := strings.Fields(out)
	if len(f) >= 2 {
		return f[0], f[1]
	}
	return "", ""
}

// archOf is the architecture of a build for what uname says.
func archOf(machine string) (string, bool) {
	switch machine {
	case "x86_64", "amd64":
		return "amd64", true
	case "aarch64", "arm64":
		return "arm64", true
	}
	return "", false
}

var procName = regexp.MustCompile(`\("([^"]+)"`)

// parseListeners reads `ss -ltnp` into the TCP ports in use and who holds
// each, where ss says. Several processes can share a port.
func parseListeners(out string) map[int][]string {
	m := map[int][]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "LISTEN" {
			continue
		}
		i := strings.LastIndexByte(f[3], ':')
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(f[3][i+1:])
		if err != nil {
			continue
		}
		names := m[port] // the same process listens on IPv4 and IPv6 in two lines
		if len(f) > 5 {
			for _, sub := range procName.FindAllStringSubmatch(strings.Join(f[5:], " "), -1) {
				if !slices.Contains(names, sub[1]) {
					names = append(names, sub[1])
				}
			}
		}
		m[port] = names
	}
	return m
}

// parseProcNet reads /proc/net/tcp and tcp6, for a server that has no ss.
// There is no process in it, only the port.
func parseProcNet(out string) map[int][]string {
	m := map[int][]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[3] != "0A" { // 0A is LISTEN
			continue
		}
		i := strings.LastIndexByte(f[1], ':')
		if i < 0 {
			continue
		}
		if port, err := strconv.ParseUint(f[1][i+1:], 16, 16); err == nil {
			m[int(port)] = nil
		}
	}
	return m
}

// parsePresent is the lines of cmdPresent's answer that are paths asked about.
func parsePresent(out string) map[string]bool {
	m := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if p := strings.TrimSpace(line); slices.Contains(presentPaths, p) {
			m[p] = true
		}
	}
	return m
}

// certsIn reads every certificate in PEM text.
func certsIn(data []byte) []*x509.Certificate {
	var out []*x509.Certificate
	for {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			return out
		}
		if b.Type != "CERTIFICATE" {
			continue
		}
		if c, err := x509.ParseCertificate(b.Bytes); err == nil {
			out = append(out, c)
		}
	}
}

// isOurCA says whether the node's ca.crt is this CA and nothing else: a file
// that trusts another CA as well still trusts another hub.
func isOurCA(nodeCA []byte, ours *x509.Certificate) bool {
	certs := certsIn(nodeCA)
	return len(certs) == 1 && bytes.Equal(certs[0].Raw, ours.Raw)
}

// caFingerprint names a CA in the marker.
func caFingerprint(ca *x509.Certificate) string {
	s := sha256.Sum256(ca.Raw)
	return hex.EncodeToString(s[:])
}

// marker is what the install wrote to the node about itself.
type marker struct {
	Version     int       `json:"version"`
	NodeID      uint      `json:"node_id"`
	CASHA256    string    `json:"ca_sha256"` // of the CA that signed the node, which says whose it is
	InstalledAt time.Time `json:"installed_at"`
	AgentPort   int       `json:"agent_port"`
	EntryPort   int       `json:"entry_port"`

	// What the install put there. Each is true only for what wasn't on the
	// server before, so that taking it away never takes someone else's:
	// where Xray was already there, the install leaves it as it found it.
	Agent      bool  `json:"agent"`
	XrayBinary bool  `json:"xray_binary"` // no Xray was at its path, so what is there now was pushed by the hub
	XrayUnit   bool  `json:"xray_unit"`   // the service file for Xray
	XrayConfig bool  `json:"xray_config"` // Xray's config file
	Firewall   []int `json:"firewall,omitempty"`

	// TCPRetries2Was is net.ipv4.tcp_retries2 as the server had it before the
	// install set it, which is what removal gives back if it is still the
	// install's. Empty when the install couldn't read it, or is older than the
	// setting: removal then gives back the kernel's default.
	TCPRetries2Was string `json:"tcp_retries2_was,omitempty"`
}

const markerVersion = 1

func parseMarker(out string) (*marker, error) {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	var m marker
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return nil, fmt.Errorf("the marker on the server can't be read: %w", err)
	}
	return &m, nil
}

func (m *marker) bytes() []byte {
	b, _ := json.MarshalIndent(m, "", "  ")
	return append(b, '\n')
}

func union(a, b []int) []int {
	out := slices.Clone(a)
	for _, p := range b {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// agentConfigTmpl is the agent's config.yaml. The keys are the ones
// internal/agent/config reads, and a test loads the result with it. The
// profile line is what keeps a shell, file writes, mode switches and
// uninstalls off this node: no call can change it, only this file.
var agentConfigTmpl = template.Must(template.New("config.yaml").Parse(`# Written by the Remote Nodes plugin. The hub writes this file again when it
# installs the agent again, so changes made here don't last.
profile: managed

listen_addr: ":{{.Port}}"
node_uuid: "{{.UUID}}"

xray:
  binary_path: "` + xrayBin + `"
  config_path: "` + xrayConfPath + `"
  api_addr: "127.0.0.1:{{.APIPort}}"
  api_timeout: 10
  restart_delay: 1000
  max_restarts: 5
  restart_window: 60
  access_log_path: ""

tls:
  enabled: true
  ca_cert: "` + caPath + `"
  server_cert: "` + certPath + `"
  server_key: "` + keyPath + `"

logging:
  level: "info"
  format: "text"
  output: "stdout"

process:
  service_mode: true
  service_name: "xray"
  graceful_timeout: 10s
  watchdog_enabled: true
  capture_output: true
  buffer_size: 1000

traffic:
  buffer_enabled: true
  store_path: "` + dataDir + `/traffic.json"
`))

func renderAgentConfig(nodeUUID string, agentPort, apiPort int) []byte {
	var b bytes.Buffer
	if err := agentConfigTmpl.Execute(&b, struct {
		Port, APIPort int
		UUID          string
	}{agentPort, apiPort, nodeUUID}); err != nil {
		panic(err) // the template and its data are fixed
	}
	return b.Bytes()
}

// agentUnitText runs the agent as root, as the Linux panel's install does. It
// doesn't pull Xray in: the agent starts and stops Xray itself.
const agentUnitText = `[Unit]
Description=NasNet Agent
After=network.target

[Service]
Type=simple
User=root
ExecStart=` + agentBin + ` --config ` + configPath + `
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=nasnet-agent
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`

// xrayUnitText is the service the agent starts Xray through, written only
// where the server has none. It is the Linux panel's own, and it is not
// enabled: the agent starts Xray when it has a config, which is also what
// brings it back after a reboot.
const xrayUnitText = `[Unit]
Description=Xray Service
Documentation=https://xtls.github.io
After=network.target nss-lookup.target

[Service]
User=root
ExecStart=` + xrayBin + ` run -config ` + xrayConfPath + `
Restart=on-failure
RestartPreventExitStatus=23
LimitNPROC=10000
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
`
