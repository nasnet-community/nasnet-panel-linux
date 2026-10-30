// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// put down what a server has that none of this is about: other tools, other
// services, an Xray-like name, a drop-in of someone else's. All of it has to be there
// when the agent is gone.
func (sb *sandbox) decoys() {
	sb.t.Helper()
	for p, mode := range map[string]os.FileMode{
		"/usr/local/bin/other-tool":                         0o755,
		"/usr/local/bin/xray-knife":                         0o755, // starts like Xray's, isn't
		"/usr/local/bin/nasnet-agent-notes.txt":             0o644, // starts like the agent's, isn't
		"/etc/systemd/system/other.service":                 0o644,
		"/etc/systemd/system/xray.service.d/their-own.conf": 0o644,
		"/usr/local/etc/other/conf.json":                    0o644,
		"/etc/hosts":                                        0o644,
		"/etc/nasnet-agent-backup/keep":                     0o644,
		"/var/lib/other/data":                               0o644,
		"/var/lib/nasnet-agent.old/data":                    0o644,
		"/var/log/other.log":                                0o644,
	} {
		sb.put(p, "decoy "+p+"\n", mode)
	}
}

// what the hub's pushes put on a node, after the install: the Xray build, its
// config, and the drop-in the agent writes when it starts Xray as a service.
func (sb *sandbox) afterProvisioning() {
	sb.t.Helper()
	sb.put("/usr/local/bin/xray", "#!/bin/sh\necho pushed\n", 0o755)
	sb.put("/usr/local/bin/xray.bak", "#!/bin/sh\necho older\n", 0o755)
	sb.put("/usr/local/bin/.xray-push-123456", "a spool the agent left\n", 0o600)
	sb.put("/usr/local/etc/xray/config.json", "{}", 0o644)
	sb.put("/usr/local/etc/xray/config.json.tmp.json", "{", 0o644)
	sb.put("/etc/systemd/system/xray.service.d/asset-location.conf", "[Service]\nEnvironment=XRAY_LOCATION_ASSET=/usr/local/etc/xray\n", 0o644)
}

func diff(before, after map[string]string) []string {
	var d []string
	for _, p := range slices.Sorted(maps.Keys(before)) {
		switch a, ok := after[p]; {
		case !ok:
			d = append(d, "gone: "+p)
		case a != before[p]:
			d = append(d, "changed: "+p)
		}
	}
	for _, p := range slices.Sorted(maps.Keys(after)) {
		if _, ok := before[p]; !ok {
			d = append(d, "left: "+p)
		}
	}
	return d
}

func (r *installRig) uninstall() error {
	r.t.Helper()
	return r.env.Uninstall(context.Background(), r.node, r.login, r.progress)
}

// The point of the marker: whatever the install and the hub's pushes put on a
// server, taking the agent away leaves the server as it was.
func TestUninstallPutsAServerBackAsItWas(t *testing.T) {
	r := newInstallRig(t)
	r.sb.decoys()
	r.sb.withUFW(true, 22, 443)
	before := r.sb.snapshot()
	rulesBefore := r.sb.read("/ufw.rules")

	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	r.sb.afterProvisioning()
	if len(diff(before, r.sb.snapshot())) < 10 {
		t.Fatal("the install changed next to nothing, so this test shows nothing")
	}
	r.steps = nil
	if err := r.uninstall(); err != nil {
		t.Fatal(err)
	}
	if d := diff(before, r.sb.snapshot()); len(d) != 0 {
		t.Errorf("the server isn't as it was:\n%s", strings.Join(d, "\n"))
	}
	// the firewall: the rule the install added is gone, and the ones that were there stay
	if got := r.sb.read("/ufw.rules"); got != rulesBefore {
		t.Errorf("ufw rules were\n%s\nand are\n%s", rulesBefore, got)
	}
	if log := r.sb.read("/ufw.log"); strings.Contains(log, "delete allow 443") || strings.Contains(log, "delete allow 22") {
		t.Errorf("a rule that was there before was closed:\n%s", log)
	}
	// and the services are stopped
	if r.sb.exists("/active/nasnet-agent") || r.sb.exists("/active/xray") {
		t.Error("a service is still running")
	}
	if got, want := r.stepNames(), UninstallPlan(); !slices.Equal(got, want) {
		t.Errorf("steps %q, want %q", got, want)
	}
	for _, name := range UninstallPlan() {
		if last := r.lastStep(name); last.State != StateOK {
			t.Errorf("%s ended %+v", name, last)
		}
	}
}

// A server that had Xray before: the install leaves it, the marker says so,
// and so does the removal. What the agent wrote for Xray's service is the
// agent's, and goes.
func TestUninstallNeverTakesAForeignXray(t *testing.T) {
	r := newInstallRig(t)
	r.sb.decoys()
	r.sb.put("/usr/local/bin/xray", "#!/bin/sh\necho theirs\n", 0o755)
	r.sb.put("/etc/systemd/system/xray.service", "[Service]\nExecStart=/their/xray\n", 0o644)
	r.sb.put("/usr/local/etc/xray/config.json", `{"theirs":true}`, 0o644)
	before := r.sb.snapshot()

	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	// the agent wrote its drop-in for their service
	r.sb.put("/etc/systemd/system/xray.service.d/asset-location.conf", "[Service]\nEnvironment=XRAY_LOCATION_ASSET=/usr/local/etc/xray\n", 0o644)
	r.steps = nil
	if err := r.uninstall(); err != nil {
		t.Fatal(err)
	}
	if d := diff(before, r.sb.snapshot()); len(d) != 0 {
		t.Errorf("the server isn't as it was:\n%s", strings.Join(d, "\n"))
	}
	if log := r.sb.read("/systemctl.log"); strings.Contains(log, "disable --now xray") {
		t.Errorf("their Xray was stopped:\n%s", log)
	}
	if d := r.lastStep(StepRemoveAgent).Detail; !strings.Contains(d, "Xray") || !strings.Contains(d, "left") {
		t.Errorf("the page isn't told that Xray stayed: %q", d)
	}
}

// What the marker says is what is taken, piece by piece.
func TestUninstallTakesWhatTheMarkerSaysAndNothingElse(t *testing.T) {
	for _, c := range []struct {
		name         string
		binary, unit bool
		config       bool
	}{
		{"only the binary", true, false, false},
		{"only the unit", false, true, false},
		{"only the config", false, false, true},
		{"all three", true, true, true},
		{"none", false, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newInstallRig(t)
			// as an install and a push left it, with the marker edited to the case
			if _, err := r.install(); err != nil {
				t.Fatal(err)
			}
			r.sb.put("/usr/local/bin/xray", "x", 0o755)
			r.sb.put("/usr/local/etc/xray/config.json", "{}", 0o644)
			r.sb.put("/etc/systemd/system/xray.service", "[Service]\n", 0o644)
			mk, err := parseMarker(r.sb.read("/var/lib/nasnet-agent/remote-nodes.json"))
			if err != nil || mk == nil {
				t.Fatal(err)
			}
			mk.XrayBinary, mk.XrayUnit, mk.XrayConfig = c.binary, c.unit, c.config
			r.sb.put("/var/lib/nasnet-agent/remote-nodes.json", string(mk.bytes()), 0o644)

			if err := r.uninstall(); err != nil {
				t.Fatal(err)
			}
			for p, taken := range map[string]bool{
				"/usr/local/bin/xray": c.binary, "/usr/local/etc/xray/config.json": c.config, "/etc/systemd/system/xray.service": c.unit,
			} {
				if r.sb.exists(p) == taken {
					t.Errorf("%s: exists %v, but the marker says taken=%v", p, r.sb.exists(p), taken)
				}
			}
			for _, p := range []string{"/usr/local/bin/nasnet-agent", "/etc/nasnet-agent", "/var/lib/nasnet-agent", "/etc/systemd/system/nasnet-agent.service"} {
				if r.sb.exists(p) {
					t.Errorf("%s is still there", p)
				}
			}
			stopped := strings.Contains(r.sb.read("/systemctl.log"), "disable --now xray")
			if stopped != c.unit {
				t.Errorf("Xray's service stopped: %v, with its unit taken: %v", stopped, c.unit)
			}
		})
	}
}

func TestUninstallLeavesAnotherHubsAgentAlone(t *testing.T) {
	for name, setup := range map[string]func(*installRig){
		"its CA": func(r *installRig) {
			r.sb.put("/etc/nasnet-agent/ca.crt", string(testCA(t).CertPEM()), 0o644)
		},
		"its marker": func(r *installRig) {
			r.sb.put("/var/lib/nasnet-agent/remote-nodes.json", `{"version":1,"ca_sha256":"`+strings.Repeat("cd", 32)+`","agent":true}`, 0o644)
		},
		"a CA file that isn't one": func(r *installRig) {
			r.sb.put("/etc/nasnet-agent/ca.crt", "garbage\n", 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newInstallRig(t)
			r.sb.put("/usr/local/bin/nasnet-agent", "#!/bin/sh\n", 0o755)
			r.sb.put("/etc/systemd/system/nasnet-agent.service", "[Service]\n", 0o644)
			r.sb.put("/etc/nasnet-agent/config.yaml", "listen_addr: :9090\n", 0o600)
			setup(r)
			before := r.sb.snapshot()
			err := r.uninstall()
			if !errors.Is(err, ErrOtherHub) {
				t.Fatalf("err = %v", err)
			}
			if d := diff(before, r.sb.snapshot()); len(d) != 0 {
				t.Errorf("something was changed on another hub's server:\n%s", strings.Join(d, "\n"))
			}
			for _, c := range r.sb.commands() {
				if strings.Contains(c.Cmd, "rm ") || strings.Contains(c.Cmd, "disable") {
					t.Errorf("ran %.100s", c.Cmd)
				}
			}
		})
	}
}

// An agent with nothing on the server to say whose it is isn't taken: the
// install writes the marker first and the CA soon after, so one of them is there.
func TestUninstallLeavesAnAgentNoOneVouchesFor(t *testing.T) {
	r := newInstallRig(t)
	r.sb.put("/usr/local/bin/nasnet-agent", "#!/bin/sh\n", 0o755)
	before := r.sb.snapshot()
	if err := r.uninstall(); !errors.Is(err, ErrOtherHub) {
		t.Fatalf("err = %v", err)
	}
	if d := diff(before, r.sb.snapshot()); len(d) != 0 {
		t.Errorf("changed: %v", d)
	}
}

// The CA says whose it is when the marker is gone: the agent goes, and Xray,
// which may be the server's, stays.
func TestUninstallWithoutAMarkerTakesTheAgentOnly(t *testing.T) {
	r := newInstallRig(t)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	r.sb.afterProvisioning()
	os.Remove(r.sb.path("/var/lib/nasnet-agent/remote-nodes.json"))
	if err := r.uninstall(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/usr/local/bin/nasnet-agent", "/etc/nasnet-agent", "/var/lib/nasnet-agent", "/etc/systemd/system/nasnet-agent.service"} {
		if r.sb.exists(p) {
			t.Errorf("%s is still there", p)
		}
	}
	for _, p := range []string{"/usr/local/bin/xray", "/usr/local/etc/xray/config.json", "/etc/systemd/system/xray.service"} {
		if !r.sb.exists(p) {
			t.Errorf("%s was taken with no marker to say the install put it there", p)
		}
	}
}

// The marker is written before anything else is, so what an install left when it
// stopped has the marker and is ours, even before the CA file is there.
func TestUninstallWithAMarkerAndNoCAFinishesAnInstallThatStopped(t *testing.T) {
	r := newInstallRig(t)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(r.sb.path("/etc/nasnet-agent"))
	if err := r.uninstall(); err != nil {
		t.Fatal(err)
	}
	if r.sb.exists("/var/lib/nasnet-agent") || r.sb.exists("/usr/local/bin/nasnet-agent") {
		t.Errorf("left: %v", r.sb.tree("/var", "/usr"))
	}
}

func TestUninstallOfAServerWithNothingOnItIsFine(t *testing.T) {
	r := newInstallRig(t)
	r.sb.decoys()
	// the empty folders an install leaves when it dies at its first step
	os.MkdirAll(r.sb.path("/var/lib/nasnet-agent"), 0o755)
	os.MkdirAll(r.sb.path("/etc/nasnet-agent"), 0o755)
	before := r.sb.snapshot()
	if err := r.uninstall(); err != nil {
		t.Fatal(err)
	}
	d := diff(before, r.sb.snapshot())
	if !slices.Equal(d, []string{"gone: /etc/nasnet-agent", "gone: /var/lib/nasnet-agent"}) {
		t.Errorf("the empty folders should go and nothing else: %v", d)
	}
	for _, name := range UninstallPlan() {
		if last := r.lastStep(name); last.State != StateOK {
			t.Errorf("%s ended %+v: a server with nothing on it is not a failure", name, last)
		}
	}
	if d := r.lastStep(StepCheckServer).Detail; !strings.Contains(d, "Nothing") {
		t.Errorf("check says %q", d)
	}
	// and again
	if err := r.uninstall(); err != nil {
		t.Fatalf("a second time: %v", err)
	}
}

func TestUninstallTwiceIsTheSameAsOnce(t *testing.T) {
	r := newInstallRig(t)
	r.sb.decoys()
	before := r.sb.snapshot()
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := r.uninstall(); err != nil {
			t.Fatalf("removal %d: %v", i+1, err)
		}
		if d := diff(before, r.sb.snapshot()); len(d) != 0 {
			t.Fatalf("after removal %d:\n%s", i+1, strings.Join(d, "\n"))
		}
	}
}

// Whatever command an install dies at, the server can be cleaned with
// Uninstall, and comes out as it was. A connection that drops, a command that
// fails, the hub that stops: each is tried after each command the install sends.
func TestAnInstallThatDiesAnywhereCanBeUninstalled(t *testing.T) {
	probe := newInstallRig(t)
	if _, err := probe.install(); err != nil {
		t.Fatal(err)
	}
	total := len(probe.sb.commands())
	if total < 20 {
		t.Fatalf("an install sends %d commands?", total)
	}
	// the first command that writes: until it, the install only asks
	first := 0
	for i, c := range probe.sb.commands() {
		if strings.HasPrefix(c.Cmd, "install -d") {
			first = i + 1
			break
		}
	}
	if first == 0 {
		t.Fatal("no command writes")
	}
	for n := 1; n <= total; n++ {
		t.Run(fmt.Sprintf("dies at command %d", n), func(t *testing.T) {
			t.Parallel() // each has a server of its own
			r := newInstallRig(t)
			r.sb.decoys()
			r.sb.withUFW(true, 22)
			before := r.sb.snapshot()
			rules := r.sb.read("/ufw.rules")

			calls := 0
			r.shell.failOn = func(cmd string) error {
				if calls++; calls == n {
					return errors.New("connection lost")
				}
				return nil
			}
			res, err := r.install()
			// a question that only asks (is there a gzip, is there a ufw) has its failure taken as
			// an answer, and the install goes on; one that changes the server can't be
			if err == nil && n >= first {
				t.Fatalf("the install didn't notice that command %d failed", n)
			}
			// the result never says the server is as it was when it isn't
			if changed := len(diff(before, r.sb.snapshot())) > 0; changed && !res.Changed {
				t.Errorf("the install changed the server, and says it didn't")
			}

			r.shell.failOn = nil
			if err := r.uninstall(); err != nil {
				t.Fatalf("removal after dying at command %d: %v", n, err)
			}
			if d := diff(before, r.sb.snapshot()); len(d) != 0 {
				t.Errorf("not as it was:\n%s", strings.Join(d, "\n"))
			}
			if got := r.sb.read("/ufw.rules"); got != rules {
				t.Errorf("the firewall: %q, was %q", got, rules)
			}
			if r.sb.exists("/active/nasnet-agent") || r.sb.exists("/active/xray") {
				t.Error("a service still runs")
			}
		})
	}
}

// A user who needs sudo can take it away too, with the password on stdin.
func TestUninstallThroughSudo(t *testing.T) {
	r := newInstallRig(t)
	r.shell.root, r.shell.password = false, "s3cret-sudo"
	r.sb.env["FAKE_UID"], r.sb.env["FAKE_SUDO_PW"] = "1000", "s3cret-sudo"
	r.login = Login{Password: "s3cret-sudo"}
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	r.sb.forget()
	if err := r.uninstall(); err != nil {
		t.Fatal(err)
	}
	if r.sb.exists("/etc/nasnet-agent") || r.sb.exists("/usr/local/bin/nasnet-agent") {
		t.Error("not removed")
	}
	for _, c := range r.sb.commands() {
		if strings.Contains(c.Cmd, "s3cret-sudo") {
			t.Errorf("the password is on a command line: %.100s", c.Cmd)
		}
	}
}

func TestUninstallChecksWhatItNeeds(t *testing.T) {
	r := newInstallRig(t)
	if err := (&Env{}).Uninstall(context.Background(), r.node, r.login, nil); !errors.Is(err, ErrNoCA) {
		t.Errorf("without a CA: %v", err)
	}
	r.node.SSHHostKey = ""
	if err := r.uninstall(); !errors.Is(err, ErrNoHostKey) {
		t.Errorf("without a pinned key: %v", err)
	}
	r.node.SSHHostKey = pinnedKey
	r.shows = "SHA256:another"
	var hk *HostKeyChangedError
	if err := r.uninstall(); !errors.As(err, &hk) {
		t.Errorf("a changed key: %v", err)
	}
	r.shows = pinnedKey
	r.login = Login{}
	if err := r.uninstall(); !errors.Is(err, ErrNoLogin) {
		t.Errorf("without a login: %v", err)
	}
	if len(r.sb.commands()) != 0 {
		t.Error("something ran")
	}
	// the node's own words, for a server given as a name
	var inv *model.InvalidError
	r.node.Host = "no way"
	if err := r.uninstall(); !errors.As(err, &inv) {
		t.Errorf("a bad host: %v", err)
	}
}

func TestAMarkerThatCannotBeReadIsSaid(t *testing.T) {
	r := newInstallRig(t)
	r.sb.put("/var/lib/nasnet-agent/remote-nodes.json", "{not json", 0o644)
	err := r.uninstall()
	if err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("err = %v", err)
	}
	if !r.sb.exists("/var/lib/nasnet-agent/remote-nodes.json") {
		t.Error("the marker was removed although it couldn't be read")
	}
}
