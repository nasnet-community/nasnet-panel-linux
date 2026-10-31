// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/mtproto-remote-plugin/installer"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// A first install on a clean server, signed in as root, with no firewall.
// The commands are what the server is sent, one per line, and who runs them:
// the login user for the questions, root for everything that changes
// something or reads what only root may.
func TestAnInstallRunsExactlyTheseCommands(t *testing.T) {
	r := newInstallRig(t)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	type step struct {
		root bool
		cmd  string
	}
	want := []step{
		// 1. look at the server before touching it
		{false, `uname -sm`},
		{false, `test -d '/run/systemd/system' && command -v systemctl`},
		{false, `command -v gzip`},
		{false, `command -v sha256sum`},
		{false, `. '/etc/os-release' 2>/dev/null && echo "$PRETTY_NAME"`},
		{false, `for f in '/usr/local/bin/nasnet-agent' '/etc/systemd/system/nasnet-agent.service' '/usr/local/bin/xray' '/etc/systemd/system/xray.service' '/usr/local/etc/xray/config.json'; do if [ -e "$f" ]; then echo "$f"; fi; done`},
		{true, `cat '/etc/nasnet-agent/ca.crt' 2>/dev/null || true`},
		{true, `cat '/var/lib/nasnet-agent/remote-nodes.json' 2>/dev/null || true`},
		// no marker of this plugin's, so whether the server runs an Xray of its own
		{false, `systemctl cat xray.service >/dev/null 2>&1 && echo unit; cat /proc/[0-9]*/comm 2>/dev/null | grep -qx xray && echo process; true`},
		{true, `ss -ltnp 2>/dev/null`},
		{false, `command -v ufw`},
		{false, `cat '/proc/sys/net/ipv4/tcp_retries2' 2>/dev/null || true`}, // what the kernel setting is now, for removal to give back
		// 4. the marker first, then the folders, config and certificates (the contents go on stdin)
		{true, `install -d -m 0755 '/var/lib/nasnet-agent'`},
		{true, `umask 077 && cat > '/var/lib/nasnet-agent/remote-nodes.json.new' && chmod 0644 '/var/lib/nasnet-agent/remote-nodes.json.new' && mv -f '/var/lib/nasnet-agent/remote-nodes.json.new' '/var/lib/nasnet-agent/remote-nodes.json'`},
		{true, `install -d -m 0755 '/etc/nasnet-agent' '/var/log/nasnet-agent' '/usr/local/etc/xray'`},
		{true, `umask 077 && cat > '/etc/nasnet-agent/config.yaml.new' && mv -f '/etc/nasnet-agent/config.yaml.new' '/etc/nasnet-agent/config.yaml'`},
		{true, `umask 077 && cat > '/etc/nasnet-agent/ca.crt.new' && chmod 0644 '/etc/nasnet-agent/ca.crt.new' && mv -f '/etc/nasnet-agent/ca.crt.new' '/etc/nasnet-agent/ca.crt'`},
		{true, `umask 077 && cat > '/etc/nasnet-agent/server.crt.new' && chmod 0644 '/etc/nasnet-agent/server.crt.new' && mv -f '/etc/nasnet-agent/server.crt.new' '/etc/nasnet-agent/server.crt'`},
		{true, `umask 077 && cat > '/etc/nasnet-agent/server.key.new' && mv -f '/etc/nasnet-agent/server.key.new' '/etc/nasnet-agent/server.key'`},
		// the kernel setting for the tunnel: live, then in the file systemd reads at boot
		{true, `echo '5' > '/proc/sys/net/ipv4/tcp_retries2'`},
		{true, `mkdir -p -m 0755 '/etc/sysctl.d'`},
		{true, `umask 077 && cat > '/etc/sysctl.d/60-nasnet-agent.conf.new' && chmod 0644 '/etc/sysctl.d/60-nasnet-agent.conf.new' && mv -f '/etc/sysctl.d/60-nasnet-agent.conf.new' '/etc/sysctl.d/60-nasnet-agent.conf'`},
		// 5. the agent streams through gzip on the server, whose hash of it is checked before it moves into place
		{true, `umask 077 && gzip -dc > '/usr/local/bin/.nasnet-agent.upload'`},
		{true, `sha256sum '/usr/local/bin/.nasnet-agent.upload'`},
		{true, `chmod 0755 '/usr/local/bin/.nasnet-agent.upload' && mv -f '/usr/local/bin/.nasnet-agent.upload' '/usr/local/bin/nasnet-agent'`},
		{true, `'/usr/local/bin/nasnet-agent' version`},
		// 6. the units, and the service
		{true, `umask 077 && cat > '/etc/systemd/system/nasnet-agent.service.new' && chmod 0644 '/etc/systemd/system/nasnet-agent.service.new' && mv -f '/etc/systemd/system/nasnet-agent.service.new' '/etc/systemd/system/nasnet-agent.service'`},
		{true, `umask 077 && cat > '/etc/systemd/system/xray.service.new' && chmod 0644 '/etc/systemd/system/xray.service.new' && mv -f '/etc/systemd/system/xray.service.new' '/etc/systemd/system/xray.service'`},
		{true, `systemctl daemon-reload && systemctl enable nasnet-agent && systemctl restart nasnet-agent`},
		{false, `systemctl is-active nasnet-agent`},
		{true, `ss -ltnp 2>/dev/null`},
	}
	var got []step
	for _, c := range r.sb.commands() {
		got = append(got, step{c.Root, c.Cmd})
	}
	for i := 0; i < max(len(got), len(want)); i++ {
		switch {
		case i >= len(got):
			t.Errorf("command %d never ran: %s", i+1, want[i].cmd)
		case i >= len(want):
			t.Errorf("command %d wasn't expected: %s", i+1, got[i].cmd)
		case got[i] != want[i]:
			t.Errorf("command %d\n got  root=%v %s\n want root=%v %s", i+1, got[i].root, got[i].cmd, want[i].root, want[i].cmd)
		}
	}
	// and none of them failed but the question about ufw
	for _, c := range r.sb.commands() {
		if c.Code != 0 && c.Cmd != "command -v ufw" {
			t.Errorf("%q ended with %d", c.Cmd, c.Code)
		}
	}
	if len(r.verified) != 1 {
		t.Errorf("the agent was checked over the link %d times", len(r.verified))
	}
}

func TestAnInstallLeavesTheServerAsTheAgentNeedsIt(t *testing.T) {
	r := newInstallRig(t)
	res, err := r.install()
	if err != nil {
		t.Fatal(err)
	}
	sb := r.sb

	// modes: the key and the config for root alone, the certificates readable, the agent runs
	for p, want := range map[string]os.FileMode{
		"/etc/nasnet-agent/config.yaml": 0o600, "/etc/nasnet-agent/server.key": 0o600,
		"/etc/nasnet-agent/ca.crt": 0o644, "/etc/nasnet-agent/server.crt": 0o644,
		"/usr/local/bin/nasnet-agent":              0o755,
		"/etc/systemd/system/nasnet-agent.service": 0o644,
		"/etc/systemd/system/xray.service":         0o644,
		"/var/lib/nasnet-agent/remote-nodes.json":  0o644,
		"/etc/sysctl.d/60-nasnet-agent.conf":       0o644,
	} {
		if got := sb.mode(p); got != want {
			t.Errorf("%s is %v, want %v", p, got, want)
		}
	}
	// no half-written files and no upload left
	for _, p := range sb.tree("/etc/nasnet-agent", "/var/lib/nasnet-agent", "/usr/local/bin", "/etc/systemd/system", "/etc/sysctl.d") {
		if strings.HasSuffix(p, ".new") || strings.HasSuffix(p, ".upload") {
			t.Errorf("left behind: %s", p)
		}
	}
	if !sb.exists("/var/log/nasnet-agent") {
		t.Error("no log folder")
	}
	// the agent checks a config by writing a temp file beside the real one, so
	// the folder has to be there before the first config is pushed
	if !sb.exists("/usr/local/etc/xray") {
		t.Error("no folder for Xray's config")
	}

	// the agent on the server is what was sent, unpacked
	ag, _ := r.files.Agent(context.Background(), "amd64")
	if got := sb.read("/usr/local/bin/nasnet-agent"); sum256([]byte(got)) != ag.PlainSHA256 {
		t.Error("the binary on the server isn't the agent that was sent")
	}

	// what the agent reads: its own config loader takes the file, and sees the managed profile
	cfg, err := config.Load(sb.path("/etc/nasnet-agent/config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("the agent would refuse its config: %v", err)
	}
	checks := map[string][2]string{
		"profile":      {string(cfg.Profile), "managed"},
		"listen_addr":  {cfg.ListenAddr, ":9090"},
		"connect mode": {cfg.ConnectMode, ""},
		"xray binary":  {cfg.Xray.BinaryPath, "/usr/local/bin/xray"},
		"xray config":  {cfg.Xray.ConfigPath, "/usr/local/etc/xray/config.json"},
		"xray api":     {cfg.Xray.APIAddr, "127.0.0.1:10085"},
		"tls ca":       {cfg.TLS.CACert, "/etc/nasnet-agent/ca.crt"},
		"tls cert":     {cfg.TLS.ServerCert, "/etc/nasnet-agent/server.crt"},
		"tls key":      {cfg.TLS.ServerKey, "/etc/nasnet-agent/server.key"},
		"service name": {cfg.Process.ServiceName, "xray"},
		"node uuid":    {cfg.NodeUUID, res.NodeUUID},
	}
	for what, c := range checks {
		if c[0] != c[1] {
			t.Errorf("config %s = %q, want %q", what, c[0], c[1])
		}
	}
	if !cfg.TLS.Enabled || !cfg.Process.ServiceMode || !cfg.Traffic.BufferEnabled || cfg.Xray.AccessLogPath != "" {
		t.Errorf("config: tls %v, service mode %v, traffic buffer %v, access log %q", cfg.TLS.Enabled, cfg.Process.ServiceMode, cfg.Traffic.BufferEnabled, cfg.Xray.AccessLogPath)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(res.NodeUUID) {
		t.Errorf("node uuid %q is not a version 4 UUID", res.NodeUUID)
	}

	// the certificates: ours, for this node, for its address, and the key is the certificate's
	if got := sb.read("/etc/nasnet-agent/ca.crt"); got != string(r.ca.CertPEM()) {
		t.Error("ca.crt is not the hub's CA")
	}
	pair, err := tls.LoadX509KeyPair(sb.path("/etc/nasnet-agent/server.crt"), sb.path("/etc/nasnet-agent/server.key"))
	if err != nil {
		t.Fatalf("the certificate and key don't match: %v", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(r.ca.Certificate())
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Errorf("not a server certificate from the hub's CA: %v", err)
	}
	if err := leaf.VerifyHostname(rigHost); err != nil {
		t.Errorf("not for the node's address: %v", err)
	}
	if leaf.Subject.CommonName != "node-3-Frankfurt" {
		t.Errorf("CN %q", leaf.Subject.CommonName)
	}
	if res.CertSerial != leaf.SerialNumber.String() || !res.CertNotAfter.Equal(leaf.NotAfter) || !res.CertNotBefore.Equal(leaf.NotBefore) {
		t.Errorf("the result says %s %v..%v, the certificate %s %v..%v", res.CertSerial, res.CertNotBefore, res.CertNotAfter, leaf.SerialNumber, leaf.NotBefore, leaf.NotAfter)
	}

	// the units: root runs the agent with that config, and Xray's service is written for the agent to start
	unit := sb.read("/etc/systemd/system/nasnet-agent.service")
	for _, want := range []string{"ExecStart=/usr/local/bin/nasnet-agent --config /etc/nasnet-agent/config.yaml", "User=root", "Restart=always", "WantedBy=multi-user.target"} {
		if !strings.Contains(unit, want) {
			t.Errorf("the agent's unit lacks %q:\n%s", want, unit)
		}
	}
	xunit := sb.read("/etc/systemd/system/xray.service")
	for _, want := range []string{"ExecStart=/usr/local/bin/xray run -config /usr/local/etc/xray/config.json", "User=root"} {
		if !strings.Contains(xunit, want) {
			t.Errorf("Xray's unit lacks %q:\n%s", want, xunit)
		}
	}
	if log := sb.read("/systemctl.log"); !strings.Contains(log, "enable nasnet-agent") || strings.Contains(log, "enable xray") {
		t.Errorf("systemctl was told:\n%s", log)
	}

	// the marker says what was added, and that the CA that signed it is ours
	var mk marker
	if err := json.Unmarshal([]byte(sb.read("/var/lib/nasnet-agent/remote-nodes.json")), &mk); err != nil {
		t.Fatal(err)
	}
	if mk.NodeID != 3 || !mk.Agent || !mk.XrayBinary || !mk.XrayUnit || !mk.XrayConfig || mk.CASHA256 != caFingerprint(r.ca.Certificate()) ||
		mk.AgentPort != 9090 || mk.EntryPort != 443 || len(mk.Firewall) != 0 || mk.Version != markerVersion {
		t.Errorf("marker: %+v", mk)
	}
	// the kernel setting: given up on after about a minute and not fifteen, live and kept for the next boot, and
	// the marker says what it was
	if got := sb.read("/proc/sys/net/ipv4/tcp_retries2"); got != "5\n" {
		t.Errorf("net.ipv4.tcp_retries2 is %q", got)
	}
	if mk.TCPRetries2Was != "15" {
		t.Errorf("the marker says the setting was %q", mk.TCPRetries2Was)
	}
	if got := sb.read("/etc/sysctl.d/60-nasnet-agent.conf"); got != sysctlText || !strings.Contains(got, "\nnet.ipv4.tcp_retries2 = 5\n") {
		t.Errorf("the sysctl file:\n%s", got)
	}

	// and what the manager is told
	if !res.Changed || res.Repaired || res.Arch != "amd64" || res.OS != "Debian GNU/Linux 12 (bookworm)" || res.AgentVersion != "v1.3.0" || res.XrayVersion != "" {
		t.Errorf("result: %+v", res)
	}
}

// The key of a node's certificate goes to the node once. It isn't on the
// command line, nor in what the steps say, and the install keeps no copy.
func TestTheNodesKeyTravelsOnStdinOnly(t *testing.T) {
	r := newInstallRig(t)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	key := r.sb.read("/etc/nasnet-agent/server.key")
	if !strings.Contains(key, "PRIVATE KEY") {
		t.Fatalf("no key on the server: %q", key)
	}
	body := strings.Split(strings.TrimSpace(key), "\n")[1]
	for _, c := range r.sb.commands() {
		if strings.Contains(c.Cmd, body) {
			t.Errorf("the key is on a command line: %.80s", c.Cmd)
		}
	}
	for _, s := range r.steps {
		if strings.Contains(s.Detail, body) {
			t.Errorf("the key is in a step: %+v", s)
		}
	}
	var keyWrites int
	for _, c := range r.sb.commands() {
		if strings.Contains(string(c.Stdin), body) {
			keyWrites++
			if !strings.Contains(c.Cmd, "server.key") {
				t.Errorf("the key was written by %q", c.Cmd)
			}
		}
	}
	if keyWrites != 1 {
		t.Errorf("the key was sent %d times", keyWrites)
	}
}

func TestAnInstallReportsItsStepsInTheOrderOfItsPlan(t *testing.T) {
	r := newInstallRig(t)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	if got, want := r.stepNames(), InstallPlan(); !slices.Equal(got, want) {
		t.Fatalf("steps %q, want %q", got, want)
	}
	for _, name := range InstallPlan() {
		if last := r.lastStep(name); last.State != StateOK {
			t.Errorf("%s ended %+v", name, last)
		}
	}
	for name, want := range map[string]string{StepCheckServer: "amd64 · Debian GNU/Linux 12 (bookworm)", StepCheckAgent: "v1.3.0, managed profile"} {
		if got := r.lastStep(name).Detail; got != want {
			t.Errorf("%s says %q, want %q", name, got, want)
		}
	}
	if d := r.lastStep(StepDownloadAgent).Detail; !strings.HasPrefix(d, "v1.3.0 · ") {
		t.Errorf("download says %q", d)
	}
	// and a nil Progress is fine
	r2 := newInstallRig(t)
	if _, err := r2.env.InstallAgent(context.Background(), r2.node, r2.login, nil); err != nil {
		t.Fatal(err)
	}
}

func TestTheUploadReportsItsProgress(t *testing.T) {
	r := newInstallRig(t)
	r.files.padding = 3 << 20 // random, so it doesn't pack: about 3 MB goes over the wire
	// a clock that moves a second and a bit at every look, so every read is worth a report
	clock := time.Now()
	r.env.Now = func() time.Time { clock = clock.Add(1100 * time.Millisecond); return clock }
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	var details []string
	for _, s := range r.steps {
		if s.Name == StepUploadAgent && s.State == StateRunning && s.Detail != "" {
			details = append(details, s.Detail)
		}
	}
	if len(details) < 3 {
		t.Fatalf("%d reports: %q", len(details), details)
	}
	re := regexp.MustCompile(`^(\d+)% · (\d+ KB|\d+\.\d MB) sent( · about \d+ (s|min) left)?$`)
	prev := -1
	for _, d := range details {
		m := re.FindStringSubmatch(d)
		if m == nil {
			t.Fatalf("not the usual wording: %q", d)
		}
		var pct int
		if _, err := fmtSscan(m[1], &pct); err != nil || pct < prev || pct > 100 {
			t.Fatalf("%q after %d%%", d, prev)
		}
		prev = pct
	}
	if !strings.HasPrefix(details[len(details)-1], "100% · ") {
		t.Errorf("the last report is %q", details[len(details)-1])
	}
	if !strings.HasSuffix(r.lastStep(StepUploadAgent).Detail, " sent") || r.lastStep(StepUploadAgent).State != StateOK {
		t.Errorf("the step ended %+v", r.lastStep(StepUploadAgent))
	}
}

// What the install won't do on a server, and that it says so before changing
// anything: the server is left as it was.
func TestAnInstallStopsBeforeChangingAServerThatCannotTakeIt(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*installRig)
		is    error
		say   []string
	}{
		{"an unsupported architecture", func(r *installRig) { r.sb.env["FAKE_ARCH"] = "armv7l" }, ErrUnsupportedHost, []string{"armv7l", "amd64", "arm64"}},
		{"another operating system", func(r *installRig) { r.sb.env["FAKE_OS"] = "FreeBSD" }, ErrUnsupportedHost, []string{"FreeBSD", "Linux"}},
		{"no systemd", func(r *installRig) { os.RemoveAll(r.sb.path("/run/systemd")) }, ErrUnsupportedHost, []string{"systemd"}},
		{"no gzip", func(r *installRig) { r.sb.without("gzip", "gunzip") }, ErrUnsupportedHost, []string{"gzip"}},
		{"no sha256sum", func(r *installRig) { r.sb.without("sha256sum") }, ErrUnsupportedHost, []string{"sha256sum"}},
		{"the agent's port in use", func(r *installRig) { r.sb.listening(9090, "python3") }, ErrPortInUse, []string{"TCP port 9090", "agent", "python3"}},
		{"the entry port in use", func(r *installRig) { r.sb.listening(443, "nginx") }, ErrPortInUse, []string{"TCP port 443", "entry", "nginx"}},
		{"a port in use by nobody ss will name", func(r *installRig) { r.sb.listening(443, "") }, ErrPortInUse, []string{"TCP port 443"}},
		{"another hub's agent", func(r *installRig) {
			other := testCA(t)
			r.sb.put("/etc/nasnet-agent/ca.crt", string(other.CertPEM()), 0o644)
			r.sb.put("/usr/local/bin/nasnet-agent", "#!/bin/sh\n", 0o755)
		}, ErrServerHasAgent, []string{"another hub", "one hub"}},
		{"a CA file that isn't one", func(r *installRig) { r.sb.put("/etc/nasnet-agent/ca.crt", "not a certificate\n", 0o644) }, ErrServerHasAgent, []string{"another hub"}},
		{"a marker from another hub's install", func(r *installRig) {
			r.sb.put("/var/lib/nasnet-agent/remote-nodes.json", `{"version":1,"ca_sha256":"`+strings.Repeat("ab", 32)+`","agent":true}`, 0o644)
		}, ErrServerHasAgent, []string{"another hub"}},
		{"a CA file with another CA beside ours", func(r *installRig) {
			r.sb.put("/etc/nasnet-agent/ca.crt", string(r.ca.CertPEM())+string(testCA(t).CertPEM()), 0o644)
		}, ErrServerHasAgent, []string{"another hub"}},
		// a Linux panel's node with no TLS has an agent and nothing that says whose
		{"an agent nobody vouches for", func(r *installRig) {
			r.sb.put("/usr/local/bin/nasnet-agent", "#!/bin/sh\n", 0o755)
		}, ErrServerHasAgent, []string{"did not install", "Linux panel", "one hub"}},
		{"an agent's unit nobody vouches for", func(r *installRig) {
			r.sb.put("/etc/systemd/system/nasnet-agent.service", "[Service]\nExecStart=/opt/agent\n", 0o644)
		}, ErrServerHasAgent, []string{"did not install"}},
		// the server's own Xray, found any of the ways it shows
		{"an Xray binary at the standard path", func(r *installRig) {
			r.sb.put("/usr/local/bin/xray", "#!/bin/sh\necho theirs\n", 0o755)
		}, ErrServerHasXray, []string{"already runs an Xray", "Remote Nodes did not install", "/usr/local/bin/xray", "without one"}},
		{"an xray.service where the install writes its own", func(r *installRig) {
			r.sb.put("/etc/systemd/system/xray.service", "[Service]\nExecStart=/their/xray\n", 0o644)
		}, ErrServerHasXray, []string{"xray.service unit"}},
		{"an xray.service from a package", func(r *installRig) {
			r.sb.put("/lib/systemd/system/xray.service", "[Service]\nExecStart=/usr/bin/xray\n", 0o644)
		}, ErrServerHasXray, []string{"xray.service unit"}},
		{"an xray process from somewhere else", func(r *installRig) {
			r.sb.put("/proc/4321/comm", "xray\n", 0o644)
		}, ErrServerHasXray, []string{"running xray process"}},
		{"an Xray with a CA file of ours and no marker", func(r *installRig) {
			r.sb.put("/etc/nasnet-agent/ca.crt", string(r.ca.CertPEM()), 0o644)
			r.sb.put("/usr/local/bin/xray", "#!/bin/sh\n", 0o755)
		}, ErrServerHasXray, []string{"/usr/local/bin/xray"}},
		{"a login that isn't root and no sudo", func(r *installRig) {
			r.shell.root = false
			r.sb.without("sudo")
		}, ErrNeedsRoot, []string{"sudo"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newInstallRig(t)
			c.setup(r)
			before := r.sb.tree("/etc", "/var", "/usr", "/run")
			res, err := r.install()
			if !errors.Is(err, c.is) {
				t.Fatalf("err = %v, want %v", err, c.is)
			}
			for _, s := range c.say {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("%q should mention %q", err, s)
				}
			}
			if res.Changed {
				t.Error("the result says the server was changed")
			}
			// nothing was written: the questions all read
			for _, run := range r.sb.commands() {
				if strings.Contains(run.Cmd, "install -d") || strings.Contains(run.Cmd, "cat >") || strings.Contains(run.Cmd, "gzip -dc") || strings.Contains(run.Cmd, "systemctl enable") {
					t.Errorf("a command that changes the server ran: %.100s", run.Cmd)
				}
			}
			if after := r.sb.tree("/etc", "/var", "/usr", "/run"); !slices.Equal(before, after) {
				t.Errorf("the server changed:\nbefore %v\nafter  %v", before, after)
			}
			// the step that found it says so, and the later ones never ran
			if last := r.lastStep(StepCheckServer); last.State != StateFailed || last.Detail != err.Error() {
				t.Errorf("check step: %+v", last)
			}
			if n := r.stepNames(); !slices.Equal(n, []string{StepCheckServer}) {
				t.Errorf("steps that ran: %q", n)
			}
			if len(r.files.asked) != 0 {
				t.Errorf("something was downloaded: %v", r.files.asked)
			}
		})
	}
}

func TestAPortInUseNamesTheOneAndTheOtherPassesWhenFree(t *testing.T) {
	r := newInstallRig(t)
	r.sb.listening(22, "sshd")
	r.sb.listening(8443, "nginx") // not one of ours
	if _, err := r.install(); err != nil {
		t.Fatalf("ports that aren't the agent's or the entry's are none of its business: %v", err)
	}
	// and with ss missing, the kernel's own table is read
	r2 := newInstallRig(t)
	r2.sb.without("ss")
	r2.sb.put("/proc/net/tcp", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n   0: 00000000:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1 0000\n", 0o644) // 0x01BB is 443
	_, err := r2.install()
	var pu *PortInUseError
	if !errors.As(err, &pu) || pu.Port != 443 || pu.Role != "entry" {
		t.Fatalf("with no ss, a busy 443 in /proc/net/tcp: %v", err)
	}
}

// A server with the agent already on it, put there by this hub, is a repair:
// its own processes hold its ports, and they don't stop it.
func TestAnInstallOverTheHubsOwnAgentIsARepair(t *testing.T) {
	r := newInstallRig(t)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	// the agent runs, holding its port; Xray was pushed over the link and holds the entry's,
	// and runs under the unit the install wrote, which the marker vouches for
	r.sb.listening(443, "xray")
	r.sb.put("/usr/local/bin/xray", "#!/bin/sh\n", 0o755)
	r.sb.put("/usr/local/etc/xray/config.json", "{}", 0o644)
	r.sb.put("/proc/4243/comm", "xray\n", 0o644)
	oldKey, oldCert := r.sb.read("/etc/nasnet-agent/server.key"), r.sb.read("/etc/nasnet-agent/server.crt")
	var first marker
	json.Unmarshal([]byte(r.sb.read("/var/lib/nasnet-agent/remote-nodes.json")), &first)

	r.steps, r.verified = nil, nil
	r.sb.forget()
	res, err := r.install()
	if err != nil {
		t.Fatalf("a repair of this hub's own agent: %v", err)
	}
	if !res.Repaired || !res.Changed {
		t.Errorf("result %+v", res)
	}
	if d := r.lastStep(StepCheckServer).Detail; !strings.Contains(d, "agent already installed") {
		t.Errorf("the check says %q", d)
	}
	// everything is written again: a new certificate and key, with the same CA
	if r.sb.read("/etc/nasnet-agent/server.key") == oldKey || r.sb.read("/etc/nasnet-agent/server.crt") == oldCert {
		t.Error("the certificate wasn't renewed")
	}
	// what the first install put there is still what is on record: the Xray that is there now
	// was pushed through it, and must not look like a foreign one
	var now marker
	if err := json.Unmarshal([]byte(r.sb.read("/var/lib/nasnet-agent/remote-nodes.json")), &now); err != nil {
		t.Fatal(err)
	}
	if !now.XrayBinary || !now.XrayUnit || !now.XrayConfig {
		t.Errorf("the repair forgot what the install added: %+v", now)
	}
	if !now.InstalledAt.Equal(first.InstalledAt) {
		t.Errorf("installed at %v, was %v", now.InstalledAt, first.InstalledAt)
	}
	// the agent's port is held by the agent, which is the one thing that may hold it
	r.sb.listening(8000, "nginx")
}

func TestARepairStillStopsForSomeoneElseOnItsPorts(t *testing.T) {
	r := newInstallRig(t)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	r.sb.listening(443, "nginx") // someone took the entry port since
	var pu *PortInUseError
	if _, err := r.install(); !errors.As(err, &pu) || pu.Port != 443 || pu.Process != "nginx" {
		t.Fatalf("err = %v", err)
	}
}

// The Xray an install finds on a server isn't left to run beside the node's:
// both want the same paths and the same service. The install stops before it
// writes anything, and says what it found.
func TestAnInstallRefusesAServerThatRunsItsOwnXray(t *testing.T) {
	r := newInstallRig(t)
	r.sb.put("/usr/local/bin/xray", "#!/bin/sh\necho theirs\n", 0o755)
	r.sb.put("/etc/systemd/system/xray.service", "[Service]\nExecStart=/their/xray\n", 0o644)
	r.sb.put("/usr/local/etc/xray/config.json", `{"theirs":true}`, 0o644)
	r.sb.put("/proc/812/comm", "xray\n", 0o644)
	before := r.sb.snapshot()
	_, err := r.install()
	if !errors.Is(err, ErrServerHasXray) {
		t.Fatalf("err = %v", err)
	}
	for _, s := range []string{"/usr/local/bin/xray", "an xray.service unit", "a running xray process"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("%q doesn't name %q", err, s)
		}
	}
	if d := diff(before, r.sb.snapshot()); len(d) != 0 {
		t.Errorf("the server changed:\n%s", strings.Join(d, "\n"))
	}
	if r.sb.exists("/var/lib/nasnet-agent/remote-nodes.json") {
		t.Error("a marker was written")
	}
}

// A user that isn't root signs in and the install goes through sudo, with
// the password on stdin and never in a command.
func TestAnInstallAsAUserWhoCanUseSudo(t *testing.T) {
	r := newInstallRig(t)
	r.shell.root, r.shell.password = false, "s3cret-sudo"
	r.sb.env["FAKE_UID"], r.sb.env["FAKE_SUDO_PW"] = "1000", "s3cret-sudo"
	r.login = Login{Password: "s3cret-sudo"}
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	if !r.sb.exists("/usr/local/bin/nasnet-agent") || !r.sb.exists("/etc/nasnet-agent/server.key") {
		t.Fatal("nothing was installed")
	}
	if log := r.sb.read("/sudo.log"); !strings.Contains(log, "sudo -Sk") {
		t.Errorf("sudo was called as:\n%s", log)
	}
	// questions are asked as the user, changes as root
	for _, c := range r.sb.commands() {
		if c.Root != strings.HasPrefix(c.Cmd, "sudo ") {
			t.Errorf("%q: root=%v", c.Cmd, c.Root)
		}
		if strings.Contains(c.Cmd, "s3cret-sudo") {
			t.Errorf("the password is on a command line: %.100s", c.Cmd)
		}
		// the password is the first line of stdin of a sudo -S command, and nowhere else
		onStdin := strings.Contains(string(c.Stdin), "s3cret-sudo")
		if onStdin && !strings.HasPrefix(c.Cmd, "sudo -k -S") {
			t.Errorf("the password was sent to %.100s", c.Cmd)
		}
		if onStdin && !strings.HasPrefix(string(c.Stdin), "s3cret-sudo\n") {
			t.Errorf("the password isn't the first line of stdin of %.100s", c.Cmd)
		}
	}
	// and the server never kept it
	for _, p := range r.sb.tree("/etc", "/var", "/usr") {
		if strings.Contains(r.sb.read(p), "s3cret-sudo") {
			t.Errorf("the password is in %s", p)
		}
	}
}

func TestAWrongSudoPasswordIsSaidPlainly(t *testing.T) {
	r := newInstallRig(t)
	r.shell.root, r.shell.password = false, "wrong-pw"
	r.sb.env["FAKE_UID"], r.sb.env["FAKE_SUDO_PW"] = "1000", "right-pw"
	r.login = Login{Password: "wrong-pw"}
	res, err := r.install()
	if !errors.Is(err, ErrNeedsRoot) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "wrong-pw") || res.Changed {
		t.Errorf("err %q, changed %v", err, res.Changed)
	}
}

// A password goes into the install and must not come out of it: not in a
// command, a step, an error, or what is written to the server. A library that
// echoes what it was given is scrubbed.
func TestThePasswordNeverComesOut(t *testing.T) {
	const pw = "pa55-w0rd-xyz"
	r := newInstallRig(t)
	r.login = Login{Password: pw, Passphrase: "key-passphrase-xyz"}
	r.connectFn = func(ctx context.Context, tg installer.Target) (Shell, error) {
		// a library that puts the login in its complaint
		return nil, errors.New("ssh: unable to authenticate, attempted password " + tg.Password + " and passphrase " + string(tg.Passphrase))
	}
	var logged strings.Builder
	r.env.Log = newCaptureLog(&logged)
	res, err := r.install()
	if !errors.Is(err, ErrLoginFailed) {
		t.Fatalf("err = %v", err)
	}
	for _, s := range []string{pw, "key-passphrase-xyz"} {
		if strings.Contains(err.Error(), s) {
			t.Errorf("the error carries the login: %q", err)
		}
		for e := error(err); e != nil; e = errors.Unwrap(e) {
			if strings.Contains(e.Error(), s) {
				t.Errorf("the login is in what the error wraps: %q", e)
			}
		}
		if strings.Contains(logged.String(), s) {
			t.Errorf("the log carries the login: %s", logged.String())
		}
		for _, st := range r.steps {
			if strings.Contains(st.Detail, s) || strings.Contains(st.Name, s) {
				t.Errorf("a step carries the login: %+v", st)
			}
		}
	}
	if res.Changed {
		t.Error("changed")
	}

	// and on a run that gets as far as the server, as root: the password is never sent at all
	r2 := newInstallRig(t)
	r2.login = Login{Password: pw}
	r2.sb.env["FAKE_JOURNAL"] = "journal line"
	if _, err := r2.install(); err != nil {
		t.Fatal(err)
	}
	for _, c := range r2.sb.commands() {
		if strings.Contains(c.Cmd, pw) || strings.Contains(string(c.Stdin), pw) {
			t.Errorf("the password reached the server in %.100s", c.Cmd)
		}
	}
	for _, p := range r2.sb.tree("/etc", "/var", "/usr") {
		if strings.Contains(r2.sb.read(p), pw) {
			t.Errorf("the password is in %s", p)
		}
	}
}

// The pinned host key is what the connection is held to, and a login can be
// told how it differs from a missing one.
func TestTheLoginIsHeldToThePinnedKeyAndTheNodesPortAndUser(t *testing.T) {
	r := newInstallRig(t)
	r.node.SSHPort, r.node.SSHUser = 2222, "deploy"
	r.shell.root = true
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	tg := r.targets[0]
	if tg.Host != rigHost || tg.Port != 2222 || tg.User != "deploy" || tg.HostKey != pinnedKey || tg.Password != "hunter2" || tg.Dial == nil {
		t.Errorf("target: %+v", tg)
	}
	// no user on record means root
	r2 := newInstallRig(t)
	r2.node.SSHUser, r2.node.SSHPort = "", 0
	r2.install()
	if tg := r2.targets[0]; tg.User != "root" || tg.Port != 22 {
		t.Errorf("defaults: %+v", tg)
	}
	// no host key on record: nothing is sent to a server nobody vouched for
	r3 := newInstallRig(t)
	r3.node.SSHHostKey = ""
	if _, err := r3.install(); !errors.Is(err, ErrNoHostKey) || len(r3.targets) != 0 {
		t.Errorf("err = %v, connected %d times", err, len(r3.targets))
	}
	// and no login at all
	r4 := newInstallRig(t)
	r4.login = Login{}
	if _, err := r4.install(); !errors.Is(err, ErrNoLogin) {
		t.Errorf("err = %v", err)
	}
}

func TestAChangedHostKeyStopsTheInstallWithBothFingerprints(t *testing.T) {
	r := newInstallRig(t)
	r.shows = "SHA256:somethingelseentirely"
	res, err := r.install()
	var hk *HostKeyChangedError
	var ihk *installer.HostKeyError
	if !errors.Is(err, ErrHostKeyChanged) || !errors.As(err, &hk) || !errors.As(err, &ihk) {
		t.Fatalf("err = %v", err)
	}
	if hk.Got != "SHA256:somethingelseentirely" || hk.Pinned != pinnedKey || hk.Host != rigHost {
		t.Errorf("%+v", hk)
	}
	if res.Changed || len(r.sb.commands()) != 0 {
		t.Error("something was done on a server whose key isn't the pinned one")
	}
	if last := r.lastStep(StepCheckServer); last.State != StateFailed || !strings.Contains(last.Detail, "host key") {
		t.Errorf("step: %+v", last)
	}
}

func TestTheReleaseErrorsPassThroughTheInstall(t *testing.T) {
	for _, base := range []error{artifacts.ErrAgentTooOld, artifacts.ErrNoGzAgents, artifacts.ErrBadHash, artifacts.ErrManifest} {
		r := newInstallRig(t)
		r.files.agentErr = wrapped{base}
		res, err := r.install()
		if !errors.Is(err, base) {
			t.Fatalf("%v: err = %v", base, err)
		}
		if strings.Contains(err.Error(), "artifacts:") {
			t.Errorf("the page would read a package name: %q", err)
		}
		if res.Changed {
			t.Errorf("%v: the server was changed for a release that can't be used", base)
		}
		if last := r.lastStep(StepDownloadAgent); last.State != StateFailed {
			t.Errorf("download step: %+v", last)
		}
		// the first step passed, so the check had run, and nothing after the download did
		if n := r.stepNames(); !slices.Equal(n, []string{StepCheckServer, StepDownloadAgent}) {
			t.Errorf("steps: %q", n)
		}
	}
}

type wrapped struct{ base error }

func (w wrapped) Error() string {
	return w.base.Error() + ": the source has v1.2.0 and the plugin needs 1.3.0 or later"
}
func (w wrapped) Unwrap() error { return w.base }

func TestAnInstallNeedsWhatItSignsAndFetchesWith(t *testing.T) {
	r := newInstallRig(t)
	env := &Env{Files: r.files, connectSSH: r.env.connectSSH}
	if _, err := env.InstallAgent(context.Background(), r.node, r.login, nil); !errors.Is(err, ErrNoCA) {
		t.Errorf("without a CA: %v", err)
	}
	env = &Env{CA: r.ca, connectSSH: r.env.connectSSH}
	if _, err := env.InstallAgent(context.Background(), r.node, r.login, nil); !errors.Is(err, ErrNoFiles) {
		t.Errorf("without downloads: %v", err)
	}
	for name, node := range map[string]model.Node{
		"no id":      {Host: rigHost, AgentPort: 9090, EntryPort: 443},
		"a bad host": {ID: 1, Host: "no way", AgentPort: 9090, EntryPort: 443},
		"no port":    {ID: 1, Host: rigHost, EntryPort: 443},
		"no entry":   {ID: 1, Host: rigHost, AgentPort: 9090},
	} {
		if _, err := r.env.InstallAgent(context.Background(), node, r.login, nil); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if len(r.sb.commands()) != 0 {
		t.Error("a bad request got as far as the server")
	}
}

// A firewall that is on gets the two ports, once; one that is off, or none, is left alone.
func TestTheFirewallIsOpenedOnlyWhenItIsOnAndOnlyForWhatIsMissing(t *testing.T) {
	r := newInstallRig(t)
	r.sb.withUFW(true, 22, 443)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	if log := r.sb.read("/ufw.log"); strings.TrimSpace(log) != "allow 9090/tcp" {
		t.Errorf("ufw was told:\n%s", log)
	}
	var mk marker
	json.Unmarshal([]byte(r.sb.read("/var/lib/nasnet-agent/remote-nodes.json")), &mk)
	if !slices.Equal(mk.Firewall, []int{9090}) {
		t.Errorf("the marker says the install opened %v", mk.Firewall)
	}

	// an inactive ufw is none of its business
	r2 := newInstallRig(t)
	r2.sb.withUFW(false)
	if _, err := r2.install(); err != nil {
		t.Fatal(err)
	}
	if r2.sb.exists("/ufw.log") {
		t.Errorf("an inactive ufw was changed: %s", r2.sb.read("/ufw.log"))
	}
	for _, c := range r2.sb.commands() {
		if strings.HasPrefix(c.Cmd, "ufw allow") {
			t.Errorf("ran %q", c.Cmd)
		}
	}
}

// If the agent never listens, the server's log is what says why.
func TestAnAgentThatDoesNotStartIsReportedWithItsLog(t *testing.T) {
	r := newInstallRig(t)
	r.sb.env["FAKE_NO_START"] = "1"
	r.sb.env["FAKE_JOURNAL"] = "nasnet-agent[41]: invalid configuration: listen_addr is required"
	res, err := r.install()
	if !errors.Is(err, ErrAgentNotStarted) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "invalid configuration: listen_addr is required") {
		t.Errorf("the log isn't in it: %q", err)
	}
	if !res.Changed {
		t.Error("the server was changed, and the result says it wasn't")
	}
	if len(r.verified) != 0 {
		t.Error("the agent was checked over the link although it never listened")
	}
}

// A firewall that blocks the router is told after the agent is known to run on
// the server, with the port, and what was installed stays for a retry.
func TestAnAgentTheRouterCannotReachIsReportedWithThePort(t *testing.T) {
	r := newInstallRig(t)
	r.verifyErr = &UnreachableError{What: "agent", Host: rigHost, Port: 9090, Err: errors.New("i/o timeout")}
	res, err := r.install()
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "TCP port 9090") {
		t.Fatalf("err = %v", err)
	}
	if !res.Changed || res.AgentVersion != "" {
		t.Errorf("result %+v", res)
	}
	if last := r.lastStep(StepCheckAgent); last.State != StateFailed || !strings.Contains(last.Detail, "9090") {
		t.Errorf("step %+v", last)
	}
}

func TestACorruptedUploadIsCaughtOnTheServer(t *testing.T) {
	r := newInstallRig(t)
	r.sb.env["FAKE_BAD_SHA"] = "1" // the server hashes something other than what was sent
	res, err := r.install()
	if !errors.Is(err, ErrUploadCorrupt) {
		t.Fatalf("err = %v", err)
	}
	if !res.Changed {
		t.Error("changed should be true: the marker and the config are there")
	}
	// the damaged file is gone, and the agent was never put in place
	if r.sb.exists("/usr/local/bin/.nasnet-agent.upload") || r.sb.exists("/usr/local/bin/nasnet-agent") {
		t.Errorf("left behind: %v", r.sb.tree("/usr/local/bin"))
	}
	if log := r.sb.read("/systemctl.log"); strings.Contains(log, "restart") {
		t.Error("the service was started")
	}
	// and a stream that isn't gzip at all fails the unpacking
	r2 := newInstallRig(t)
	r2.env.Files = badGzip{r2.files}
	if _, err := r2.install(); err == nil || !strings.Contains(err.Error(), "upload") {
		t.Errorf("a stream that isn't gzip: %v", err)
	}
	if r2.sb.exists("/usr/local/bin/nasnet-agent") {
		t.Error("an agent that failed to unpack was put in place")
	}
}

// badGzip hands out an agent whose bytes don't unpack.
type badGzip struct{ *fakeFiles }

func (b badGzip) Agent(ctx context.Context, arch string) (*artifacts.Agent, error) {
	a, err := b.fakeFiles.Agent(ctx, arch)
	if err != nil {
		return nil, err
	}
	a.Gz = append([]byte("this is not gzip data at all"), a.Gz...)
	return a, nil
}

func TestACancelledInstallStopsAtOnce(t *testing.T) {
	r := newInstallRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	r.shell.failOn = func(cmd string) error {
		if n++; n == 4 {
			cancel()
		}
		return nil
	}
	_, err := r.env.InstallAgent(ctx, r.node, r.login, r.progress)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
