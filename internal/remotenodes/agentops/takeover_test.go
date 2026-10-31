// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// theirXray is an Xray a server runs of its own, laid out as the project's own
// install script leaves one: the binary, a service with a drop-in, a config
// folder, assets and logs, and a process of the service holding the port.
type theirXray struct {
	version   string   // what the binary says it is; "" puts no binary there
	patched   bool     // the same version, other bytes: a different build
	silent    bool     // a binary that prints nothing for `version`
	unit      string   // where the service is: "etc", "lib" (a package's) or "" for none
	dropIns   []string // names in /etc/systemd/system/xray.service.d
	files     []string // names in /usr/local/etc/xray
	runs      bool     // a process of the service runs, and holds port 443
	container bool     // a process named xray that no service owns, holding port 8444
}

const (
	theirUnitText   = "[Unit]\nDescription=Xray Service\n\n[Service]\nUser=nobody\nExecStart=/usr/local/bin/xray run -config /usr/local/etc/xray/config.json\nRestart=on-failure\n"
	theirDropInText = "[Service]\nExecStart=\nExecStart=/usr/local/bin/xray run -config /usr/local/etc/xray/config.json\n"
)

func (x theirXray) binary() []byte {
	if x.version == "" {
		return nil
	}
	body := "#!/bin/sh\n"
	if x.patched {
		body += "# a patched build\n"
	}
	if !x.silent {
		body += fmt.Sprintf("if [ \"$1\" = version ]; then echo 'Xray %s (Xray, Penetrates Everything.) test (go1.27 linux/amd64)'; echo 'A unified platform.'; fi\n", x.version)
	}
	return []byte(body)
}

// put lays it out in the sandbox and gives the checksum of its binary.
func (x theirXray) put(sb *sandbox) string {
	sb.t.Helper()
	sha := ""
	if b := x.binary(); b != nil {
		sb.put("/usr/local/bin/xray", string(b), 0o755)
		sha = sum256(b)
	}
	switch x.unit {
	case "etc":
		sb.put("/etc/systemd/system/xray.service", theirUnitText, 0o644)
	case "lib":
		sb.put("/lib/systemd/system/xray.service", theirUnitText, 0o644)
	}
	for _, n := range x.dropIns {
		sb.put("/etc/systemd/system/xray.service.d/"+n, theirDropInText, 0o644)
	}
	for _, n := range x.files {
		sb.put("/usr/local/etc/xray/"+n, `{"theirs":true}`, 0o644)
	}
	// what a takeover leaves alone
	sb.put("/usr/local/share/xray/geoip.dat", "assets\n", 0o644)
	sb.put("/var/log/xray/access.log", "log\n", 0o644)
	if x.runs {
		sb.put("/proc/812/comm", "xray\n", 0o644)
		sb.put("/proc/812/unit", "xray\n", 0o644)
		sb.put("/active/xray", "", 0o644)
		sb.listeningAs(443, "xray", 812)
	}
	if x.container {
		sb.put("/proc/4321/comm", "xray\n", 0o644)
		sb.listeningAs(8444, "xray", 4321)
	}
	return sha
}

// listeningAs is listening for a process with a pid of its own.
func (sb *sandbox) listeningAs(port int, process string, pid int) {
	sb.t.Helper()
	f, err := os.OpenFile(sb.path("/listening"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		sb.t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintf(f, "LISTEN 0      511          0.0.0.0:%d      0.0.0.0:*    users:((\"%s\",pid=%d,fd=6))\n", port, process, pid)
}

// asksToTakeOver makes the rig's install ask for a takeover, with the pinned
// build being whatever has this checksum ("" means the real one's, which no
// binary here has).
func (r *installRig) asksToTakeOver(pinnedSHA string) {
	r.opts = InstallOptions{TakeOverXray: true}
	if pinnedSHA != "" {
		r.env.pinnedBuild = func(string) (string, string) { return pin.Version, pinnedSHA }
	}
}

// the pinned build of these tests: the binary of the plain Xray that says the pinned version
func pinnedSHA() string { return sum256(theirXray{version: pin.Version}.binary()) }

func (r *installRig) marker() marker {
	r.t.Helper()
	mk, err := parseMarker(r.sb.read("/var/lib/nasnet-agent/remote-nodes.json"))
	if err != nil || mk == nil {
		r.t.Fatalf("the marker: %v, %v", mk, err)
	}
	return *mk
}

func (r *installRig) takeoverCommands(from string) []string {
	var out []string
	started := false
	for _, c := range r.ran() {
		started = started || c == from
		if started {
			out = append(out, c)
		}
	}
	return out
}

// Without the option the server's Xray is refused as it was, and the refusal
// says which Xray it found and whether it is the build this plugin pins.
func TestWithoutTheOptionAServersXrayIsRefusedAndItsBuildIsSaid(t *testing.T) {
	for _, c := range []struct {
		name    string
		x       theirXray
		version string
		same    bool
		found   []string
		say     []string
	}{
		{"the pinned build", theirXray{version: pin.Version, unit: "etc", runs: true}, pin.Version, true, []string{"binary", "unit", "process"},
			[]string{"It is Xray " + pin.Version + ", the same build as this plugin's.", "take it over"}},
		{"another version", theirXray{version: "26.9.1", unit: "etc"}, "26.9.1", false, []string{"binary", "unit"},
			[]string{"It is Xray 26.9.1, not the same build as this plugin's.", "take it over"}},
		{"the pinned version, another build", theirXray{version: pin.Version, patched: true}, pin.Version, false, []string{"binary"},
			[]string{"It is Xray " + pin.Version + ", not the same build as this plugin's."}},
		{"a binary that says nothing", theirXray{version: "1", silent: true}, "", false, []string{"binary"},
			[]string{"Its version can't be read"}},
		{"a service and a process, and no binary at the standard path", theirXray{unit: "lib", runs: true}, "", false, []string{"unit", "process"},
			[]string{"Its binary isn't at /usr/local/bin/xray"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newInstallRig(t)
			r.env.pinnedBuild = func(string) (string, string) { return pin.Version, pinnedSHA() }
			c.x.put(r.sb)
			before := r.sb.snapshot()
			res, err := r.install()
			var he *ServerHasXrayError
			if !errors.Is(err, ErrServerHasXray) || !errors.As(err, &he) {
				t.Fatalf("err = %v", err)
			}
			if he.Version != c.version || he.SameBuild != c.same || !slices.Equal(he.Found(), c.found) {
				t.Errorf("it says %+v, want version %q, same build %v, found %v", he, c.version, c.same, c.found)
			}
			for _, s := range append([]string{"Remote Nodes did not install", "Use a server without one, or take it over."}, c.say...) {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("%q should say %q", err, s)
				}
			}
			if res.Changed || res.TookOver {
				t.Errorf("the result says the server was touched: %+v", res)
			}
			if d := diff(before, r.sb.snapshot()); len(d) != 0 {
				t.Errorf("the server changed:\n%s", strings.Join(d, "\n"))
			}
			if log := r.sb.read("/systemctl.log"); strings.Contains(log, "stop") || strings.Contains(log, "disable") {
				t.Errorf("a service was stopped:\n%s", log)
			}
			if n := r.stepNames(); !slices.Equal(n, []string{StepCheckServer}) {
				t.Errorf("steps that ran: %q", n)
			}
		})
	}
}

// A takeover reads before it changes, stops the service before anything is
// taken, and looks for what still runs before it takes it.
func TestATakeoverRunsExactlyTheseCommands(t *testing.T) {
	r := newInstallRig(t)
	r.asksToTakeOver("")
	theirXray{version: "26.9.1", unit: "etc", dropIns: []string{"10-donot_touch_single_conf.conf"}, files: []string{"config.json"}, runs: true}.put(r.sb)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	got := r.takeoverCommands("systemctl cat xray.service >/dev/null 2>&1 && echo unit; cat /proc/[0-9]*/comm 2>/dev/null | grep -qx xray && echo process; true")
	// up to where the install starts writing its own folders
	end := slices.IndexFunc(got, func(c string) bool { return strings.HasPrefix(c, "install -d -m 0755 '/etc/nasnet-agent'") })
	if end < 0 {
		t.Fatalf("no commands for the agent's folders in\n%s", strings.Join(got, "\n"))
	}
	want := []string{
		// 1. what it is, before anything is touched
		`systemctl cat xray.service >/dev/null 2>&1 && echo unit; cat /proc/[0-9]*/comm 2>/dev/null | grep -qx xray && echo process; true`,
		`[ -x '/usr/local/bin/xray' ] && '/usr/local/bin/xray' version 2>/dev/null | head -n 1; true`,
		`sha256sum '/usr/local/bin/xray' 2>/dev/null; true`,
		`systemctl cat xray.service 2>/dev/null | head -n 1; true`,
		`systemctl is-active xray.service 2>/dev/null; true`,
		`ls -1A '/etc/systemd/system/xray.service.d' 2>/dev/null; true`,
		`ls -1A '/usr/local/etc/xray' 2>/dev/null; true`,
		`ss -ltnp 2>/dev/null`,
		`command -v ufw`,
		`cat '/proc/sys/net/ipv4/tcp_retries2' 2>/dev/null || true`,
		// 4. the takeover: the service is stopped, nothing is left running, and only then the marker and the removals
		`systemctl stop xray.service`,
		`cat /proc/[0-9]*/comm 2>/dev/null | grep -qx xray && echo process; true`,
		`install -d -m 0755 '/var/lib/nasnet-agent'`,
		`umask 077 && cat > '/var/lib/nasnet-agent/remote-nodes.json.new' && chmod 0644 '/var/lib/nasnet-agent/remote-nodes.json.new' && mv -f '/var/lib/nasnet-agent/remote-nodes.json.new' '/var/lib/nasnet-agent/remote-nodes.json'`,
		`systemctl disable xray.service 2>/dev/null; true`,
		`rm -rf '/etc/systemd/system/xray.service.d'`,
		`rm -f '/etc/systemd/system/xray.service' '/etc/systemd/system/xray.service.new'`,
		`rm -rf '/usr/local/etc/xray'/* '/usr/local/etc/xray'/.[!.]* '/usr/local/etc/xray'/..?*`,
		`rm -f '/usr/local/bin/xray'`,
		`systemctl daemon-reload 2>/dev/null || true`,
		// and then the install's own step writes the marker again, which is the same
		`install -d -m 0755 '/var/lib/nasnet-agent'`,
		`umask 077 && cat > '/var/lib/nasnet-agent/remote-nodes.json.new' && chmod 0644 '/var/lib/nasnet-agent/remote-nodes.json.new' && mv -f '/var/lib/nasnet-agent/remote-nodes.json.new' '/var/lib/nasnet-agent/remote-nodes.json'`,
	}
	for i := 0; i < max(end, len(want)); i++ {
		switch {
		case i >= end:
			t.Errorf("command %d never ran: %s", i+1, want[i])
		case i >= len(want):
			t.Errorf("command %d wasn't expected: %s", i+1, got[i])
		case got[i] != want[i]:
			t.Errorf("command %d\n got  %s\n want %s", i+1, got[i], want[i])
		}
	}
}

// The binary that is the pinned build stays where it is, and the install uses
// it as it is; everything else that was the server's is gone, with no copy.
func TestATakeoverKeepsAnXrayThatIsTheBuildThePluginPins(t *testing.T) {
	r := newInstallRig(t)
	sha := theirXray{version: pin.Version}.binary()
	r.asksToTakeOver(sum256(sha))
	theirXray{version: pin.Version, unit: "etc", dropIns: []string{"10-donot_touch_single_conf.conf", "override.conf"}, files: []string{"config.json", "inbounds.json"}, runs: true}.put(r.sb)

	res, err := r.install()
	if err != nil {
		t.Fatal(err)
	}
	if !res.TookOver || !res.XrayKept || !res.Changed {
		t.Errorf("result %+v", res)
	}
	if got, want := r.stepNames(), InstallPlanFor(r.opts); !slices.Equal(got, want) {
		t.Fatalf("steps %q, want %q", got, want)
	}
	for _, name := range InstallPlanFor(r.opts) {
		if last := r.lastStep(name); last.State != StateOK {
			t.Errorf("%s ended %+v", name, last)
		}
	}
	// the binary is the same bytes, in the same place
	if got := r.sb.read("/usr/local/bin/xray"); got != string(sha) {
		t.Errorf("the binary changed: %q", got)
	}
	// theirs is gone: the service stopped and switched off, its unit replaced by ours, drop-ins, config
	if log := r.sb.read("/systemctl.log"); !strings.Contains(log, "stop xray.service") || !strings.Contains(log, "disable xray.service") {
		t.Errorf("their service wasn't stopped and switched off:\n%s", log)
	}
	if r.sb.exists("/active/xray") || r.sb.exists("/proc/812") {
		t.Error("their Xray is still running")
	}
	if got := r.sb.read("/etc/systemd/system/xray.service"); got != xrayUnitText {
		t.Errorf("the service is %q, want the install's own", got)
	}
	if r.sb.exists("/etc/systemd/system/xray.service.d") {
		t.Error("their drop-ins are still there")
	}
	if entries, _ := os.ReadDir(r.sb.path("/usr/local/etc/xray")); len(entries) != 0 {
		t.Errorf("their config is still there: %v", entries)
	}
	// logs and assets are not the install's
	for _, p := range []string{"/usr/local/share/xray/geoip.dat", "/var/log/xray/access.log"} {
		if !r.sb.exists(p) {
			t.Errorf("%s was taken", p)
		}
	}
	// no copy of anything was kept
	for _, p := range r.sb.tree("/etc", "/usr", "/var") {
		if strings.Contains(p, "bak") || strings.Contains(p, "backup") || strings.Contains(p, "theirs") || strings.HasSuffix(p, ".old") {
			t.Errorf("a copy was kept: %s", p)
		}
	}
	// the marker says the Xray here is the plugin's from now on, and what it was
	mk := r.marker()
	if !mk.XrayBinary || !mk.XrayUnit || !mk.XrayConfig || !mk.XrayTakenOver || mk.XrayWas != pin.Version {
		t.Errorf("marker %+v", mk)
	}
	// the steps say what happened
	if d := r.lastStep(StepCheckServer).Detail; !strings.Contains(d, "takes over Xray "+pin.Version+" (the pinned build)") {
		t.Errorf("the check says %q", d)
	}
	d := r.lastStep(StepTakeOverXray).Detail
	for _, s := range []string{"Xray " + pin.Version + " was stopped", "its service, 2 drop-ins, 2 config files removed", "pinned build, so its binary stays"} {
		if !strings.Contains(d, s) {
			t.Errorf("the takeover step says %q, which lacks %q", d, s)
		}
	}
	if strings.Contains(d, "its binary,") {
		t.Errorf("the step says the binary went: %q", d)
	}
}

// Another version, and the pinned version with other bytes, are replaced: the
// binary goes, and the normal push puts the pinned one there.
func TestATakeoverReplacesAnXrayThatIsAnotherBuild(t *testing.T) {
	for _, c := range []struct {
		name   string
		x      theirXray
		pinSHA string // the checksum the pinned build has here; the plain pinned build's when empty
		say    string
	}{
		{"another version", theirXray{version: "26.9.1", unit: "etc", files: []string{"config.json"}, runs: true}, "", "Xray 26.9.1 was stopped, and its service, 1 config file, its binary removed. Xray " + pin.Version + " goes on later."},
		{"the pinned version, another build", theirXray{version: pin.Version, patched: true, unit: "etc", files: []string{"config.json"}, runs: true}, "", "has the pinned version but not the pinned build (its checksum differs), so it is replaced."},
		// both have to match: a binary whose checksum is the pinned one but that says another version is not the pinned build
		{"the pinned checksum, another version", theirXray{version: "26.9.1", unit: "etc", files: []string{"config.json"}, runs: true}, sum256(theirXray{version: "26.9.1"}.binary()), "Xray 26.9.1 was stopped, and its service, 1 config file, its binary removed."},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newInstallRig(t)
			r.asksToTakeOver(cmp.Or(c.pinSHA, pinnedSHA()))
			c.x.put(r.sb)
			res, err := r.install()
			if err != nil {
				t.Fatal(err)
			}
			if !res.TookOver || res.XrayKept {
				t.Errorf("result %+v", res)
			}
			if r.sb.exists("/usr/local/bin/xray") {
				t.Error("their binary is still there for the push to find")
			}
			if got := r.sb.read("/etc/systemd/system/xray.service"); got != xrayUnitText {
				t.Errorf("the service is %q", got)
			}
			if d := r.lastStep(StepTakeOverXray).Detail; !strings.Contains(d, c.say) {
				t.Errorf("the takeover step says %q, which lacks %q", d, c.say)
			}
			if mk := r.marker(); !mk.XrayBinary || !mk.XrayTakenOver || mk.XrayWas != c.x.version {
				t.Errorf("marker %+v", mk)
			}
		})
	}
}

// A package's unit is not the install's to remove: the one it writes to /etc
// comes first.
func TestATakeoverLeavesAPackagedUnitWhereItIs(t *testing.T) {
	r := newInstallRig(t)
	r.asksToTakeOver(pinnedSHA())
	theirXray{version: "26.9.1", unit: "lib", files: []string{"config.json"}, runs: true}.put(r.sb)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	if got := r.sb.read("/lib/systemd/system/xray.service"); got != theirUnitText {
		t.Errorf("the package's unit is now %q", got)
	}
	if got := r.sb.read("/etc/systemd/system/xray.service"); got != xrayUnitText {
		t.Errorf("the install's own unit is %q", got)
	}
	if d := r.lastStep(StepTakeOverXray).Detail; !strings.Contains(d, "a package's") {
		t.Errorf("the step says %q", d)
	}
}

// An Xray that runs after its service was stopped is no business of the
// takeover's: it stops there, with the service started again and nothing taken.
func TestATakeoverStopsWhenAnXrayStillRunsAfterItsServiceWasStopped(t *testing.T) {
	r := newInstallRig(t)
	r.asksToTakeOver(pinnedSHA())
	theirXray{version: "26.9.1", unit: "etc", dropIns: []string{"override.conf"}, files: []string{"config.json"}, runs: true, container: true}.put(r.sb)
	before := r.sb.snapshot()

	res, err := r.install()
	if !errors.Is(err, ErrXrayNotStopped) {
		t.Fatalf("err = %v", err)
	}
	for _, s := range []string{
		"An Xray is still running on this server after its service was stopped, for example in a container. Stop it there, or use another server.",
		"Its service was started again.",
	} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("%q should say %q", err, s)
		}
	}
	if res.Changed || res.TookOver || res.XrayKept {
		t.Errorf("the result says the server was touched: %+v", res)
	}
	// nothing was removed, and nothing written: the server is as it was, running as it was
	if d := diff(before, r.sb.snapshot()); len(d) != 0 {
		t.Errorf("the server changed:\n%s", strings.Join(d, "\n"))
	}
	if !r.sb.exists("/active/xray") {
		t.Error("their service was left stopped")
	}
	log := r.sb.read("/systemctl.log")
	if stop, start := strings.Index(log, "stop xray.service"), strings.Index(log, "start xray.service"); stop < 0 || start < stop {
		t.Errorf("the service was stopped and started again in this order? log:\n%s", log)
	}
	if r.sb.exists("/var/lib/nasnet-agent/remote-nodes.json") {
		t.Error("a marker was written")
	}
	// it ended at the takeover step, and nothing after it ran
	if last := r.lastStep(StepTakeOverXray); last.State != StateFailed || last.Detail != err.Error() {
		t.Errorf("takeover step: %+v", last)
	}
	if n := r.stepNames(); !slices.Equal(n, []string{StepCheckServer, StepDownloadAgent, StepCertificate, StepTakeOverXray}) {
		t.Errorf("steps that ran: %q", n)
	}
}

// An Xray with no service at all, started by hand, is found the same way: there is
// nothing to stop, so the takeover ends there and touches nothing.
func TestATakeoverOfAnXrayWithNoServiceStopsAtOnce(t *testing.T) {
	r := newInstallRig(t)
	r.asksToTakeOver(pinnedSHA())
	theirXray{version: "26.9.1", files: []string{"config.json"}, container: true}.put(r.sb)
	before := r.sb.snapshot()
	res, err := r.install()
	if !errors.Is(err, ErrXrayNotStopped) || strings.Contains(err.Error(), "service was started again") {
		t.Fatalf("err = %v", err)
	}
	if res.Changed {
		t.Errorf("result %+v", res)
	}
	if d := diff(before, r.sb.snapshot()); len(d) != 0 {
		t.Errorf("the server changed:\n%s", strings.Join(d, "\n"))
	}
	if log := r.sb.read("/systemctl.log"); strings.Contains(log, "stop") || strings.Contains(log, "start") {
		t.Errorf("a service was touched on a server that has none:\n%s", log)
	}
}

// A service that was not running is not started again, and one that can't be
// is said so: then the server is changed after all.
func TestAServiceThatCannotBeStartedAgainIsSaidSo(t *testing.T) {
	r := newInstallRig(t)
	r.asksToTakeOver(pinnedSHA())
	theirXray{version: "26.9.1", unit: "etc", runs: true, container: true}.put(r.sb)
	r.sb.env["FAKE_START_FAILS"] = "1"
	res, err := r.install()
	if !errors.Is(err, ErrXrayNotStopped) || !strings.Contains(err.Error(), "Its service was stopped and could not be started again.") {
		t.Fatalf("err = %v", err)
	}
	if !res.Changed {
		t.Error("the result says the server wasn't changed, with its service stopped")
	}

	// a service that did not run is left as it is
	r2 := newInstallRig(t)
	r2.asksToTakeOver(pinnedSHA())
	theirXray{version: "26.9.1", unit: "etc", container: true}.put(r2.sb)
	_, err = r2.install()
	if !errors.Is(err, ErrXrayNotStopped) || strings.Contains(err.Error(), "started again") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(r2.sb.read("/systemctl.log"), "start xray.service") {
		t.Error("a service that was not running was started")
	}
}

func TestAServiceThatWillNotStopChangesNothing(t *testing.T) {
	r := newInstallRig(t)
	r.asksToTakeOver(pinnedSHA())
	theirXray{version: "26.9.1", unit: "etc", files: []string{"config.json"}, runs: true}.put(r.sb)
	r.sb.env["FAKE_STOP_FAILS"] = "1"
	before := r.sb.snapshot()
	res, err := r.install()
	if err == nil || errors.Is(err, ErrXrayNotStopped) || !strings.Contains(err.Error(), "Couldn't stop Xray's service") || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("err = %v", err)
	}
	if res.Changed || res.TookOver {
		t.Errorf("result %+v", res)
	}
	if d := diff(before, r.sb.snapshot()); len(d) != 0 {
		t.Errorf("the server changed:\n%s", strings.Join(d, "\n"))
	}
}

// The ports are the server's Xray's own: it is about to be stopped, so what it
// holds is not in the way. Whatever else holds one still is.
func TestATakeoverIgnoresThePortsOfTheXrayItWillStopAndNoOtherHolder(t *testing.T) {
	r := newInstallRig(t)
	r.asksToTakeOver(pinnedSHA())
	theirXray{version: "26.9.1", unit: "etc", runs: true}.put(r.sb) // holds 443
	r.sb.listeningAs(9090, "xray", 812)                             // and the agent's port
	if _, err := r.install(); err != nil {
		t.Fatalf("their Xray holds the entry and agent ports and is about to be stopped: %v", err)
	}

	for _, c := range []struct {
		name    string
		hold    func(*installRig)
		port    int
		role    string
		process string
	}{
		{"a docker-proxy on the entry port beside it", func(r *installRig) { r.sb.listeningAs(443, "docker-proxy", 900) }, 443, "entry", "docker-proxy"},
		{"nginx on the agent's port", func(r *installRig) { r.sb.listeningAs(9090, "nginx", 901) }, 9090, "agent", "nginx"},
		// a takeover lets pass only what the server named as the Xray it will stop
		{"a holder the server won't name", func(r *installRig) { r.sb.listening(9090, "") }, 9090, "agent", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newInstallRig(t)
			r.asksToTakeOver(pinnedSHA())
			theirXray{version: "26.9.1", unit: "etc", files: []string{"config.json"}, runs: true}.put(r.sb)
			c.hold(r)
			before := r.sb.snapshot()
			res, err := r.install()
			var pu *PortInUseError
			if !errors.Is(err, ErrPortInUse) || !errors.As(err, &pu) || pu.Port != c.port || pu.Role != c.role || pu.Process != c.process {
				t.Fatalf("err = %v (%+v)", err, pu)
			}
			if c.process != "" && !strings.Contains(err.Error(), "by "+c.process) {
				t.Errorf("%q doesn't name %s", err, c.process)
			}
			if res.Changed {
				t.Error("the result says the server was changed")
			}
			if d := diff(before, r.sb.snapshot()); len(d) != 0 {
				t.Errorf("the server changed:\n%s", strings.Join(d, "\n"))
			}
			if log := r.sb.read("/systemctl.log"); strings.Contains(log, "stop xray") {
				t.Errorf("their service was stopped:\n%s", log)
			}
		})
	}
}

// The option on a server with no Xray of its own is nothing to act on: the
// install is the one it would have been, and its step says there was nothing.
func TestTheOptionOnAServerWithNoXrayOfItsOwnChangesNothing(t *testing.T) {
	plain := newInstallRig(t)
	if _, err := plain.install(); err != nil {
		t.Fatal(err)
	}

	r := newInstallRig(t)
	r.asksToTakeOver("")
	res, err := r.install()
	if err != nil {
		t.Fatal(err)
	}
	if res.TookOver || res.XrayKept {
		t.Errorf("result %+v", res)
	}
	if got, want := r.ran(), plain.ran(); !slices.Equal(got, want) {
		t.Errorf("commands differ from a plain install's:\n got %v\nwant %v", got, want)
	}
	// the step is shown done and never begins, as a step with nothing to do is
	if got, want := r.stepNames(), InstallPlan(); !slices.Equal(got, want) {
		t.Errorf("steps %q, want %q", got, want)
	}
	if last := r.lastStep(StepTakeOverXray); last.State != StateOK || last.Detail != "There is no Xray of the server's own" {
		t.Errorf("takeover step: %+v", last)
	}
	if mk := r.marker(); mk.XrayTakenOver || mk.XrayWas != "" {
		t.Errorf("marker %+v", mk)
	}
}

// A takeover that goes wrong later is cleaned away like any install, and what
// was the server's is not brought back: the cleanup says so.
func TestAFailedInstallAfterATakeoverIsCleanedAwayAndTheirXrayStaysGone(t *testing.T) {
	for _, c := range []struct {
		name string
		x    theirXray
	}{
		{"the pinned build", theirXray{version: pin.Version, unit: "etc", dropIns: []string{"override.conf"}, files: []string{"config.json"}, runs: true}},
		{"another version", theirXray{version: "26.9.1", unit: "etc", dropIns: []string{"override.conf"}, files: []string{"config.json"}, runs: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newInstallRig(t)
			r.asksToTakeOver(pinnedSHA())
			c.x.put(r.sb)
			r.verifyErr = errors.New("the agent doesn't answer")
			res, err := r.install()
			if err == nil {
				t.Fatal("the install worked")
			}
			if !res.TookOver || !res.Changed {
				t.Fatalf("result %+v", res)
			}

			r.steps = nil
			if err := r.env.UndoInstall(t.Context(), r.node, r.login, r.progress); err != nil {
				t.Fatal(err)
			}
			// ours is gone, and theirs is not back
			for _, p := range []string{
				"/usr/local/bin/xray", "/etc/systemd/system/xray.service", "/etc/systemd/system/xray.service.d",
				"/usr/local/etc/xray", "/usr/local/bin/nasnet-agent", "/etc/nasnet-agent", "/var/lib/nasnet-agent",
			} {
				if r.sb.exists(p) {
					t.Errorf("%s is still there", p)
				}
			}
			if r.sb.exists("/active/xray") {
				t.Error("an Xray runs")
			}
			// what isn't the install's stays
			for _, p := range []string{"/usr/local/share/xray/geoip.dat", "/var/log/xray/access.log"} {
				if !r.sb.exists(p) {
					t.Errorf("%s was taken", p)
				}
			}
			if d := r.lastStep(StepCleanServer).Detail; !strings.Contains(d, "was taken over, and is not put back") || !strings.Contains(d, "("+c.x.version+")") {
				t.Errorf("the cleanup says %q", d)
			}
		})
	}
}

// Removing a node whose Xray was taken over takes that Xray off, like any
// node's: the marker says all three are the plugin's.
func TestRemovingATakenOverNodeTakesItsXrayOffLikeAnyOther(t *testing.T) {
	for _, c := range []struct {
		name string
		x    theirXray
	}{
		{"the pinned build, kept", theirXray{version: pin.Version, unit: "etc", dropIns: []string{"override.conf"}, files: []string{"config.json"}, runs: true}},
		{"another version, replaced", theirXray{version: "26.9.1", unit: "etc", dropIns: []string{"override.conf"}, files: []string{"config.json"}, runs: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newInstallRig(t)
			r.asksToTakeOver(pinnedSHA())
			c.x.put(r.sb)
			before := r.sb.snapshot()
			if _, err := r.install(); err != nil {
				t.Fatal(err)
			}
			r.sb.afterProvisioning() // what the hub's pushes put there after the install
			r.steps = nil
			if err := r.uninstall(); err != nil {
				t.Fatal(err)
			}
			// the server is as it was but for what the takeover took of theirs, which is not back
			var gone []string
			for _, line := range diff(before, r.sb.snapshot()) {
				p, ok := strings.CutPrefix(line, "gone: ")
				if !ok {
					t.Errorf("%s", line)
				}
				gone = append(gone, p)
			}
			slices.Sort(gone)
			want := []string{
				"/etc/systemd/system/xray.service", "/etc/systemd/system/xray.service.d", "/etc/systemd/system/xray.service.d/override.conf",
				"/proc/812", "/proc/812/comm", "/proc/812/unit",
				"/usr/local/bin/xray", "/usr/local/etc/xray", "/usr/local/etc/xray/config.json",
			}
			if !slices.Equal(gone, want) {
				t.Errorf("gone:\n%s\nwant:\n%s", strings.Join(gone, "\n"), strings.Join(want, "\n"))
			}
			if d := r.lastStep(StepRemoveAgent).Detail; !strings.Contains(d, "was taken over, and is not put back") {
				t.Errorf("the removal says %q", d)
			}
		})
	}
}

// Installing again over a node whose Xray was taken over is a repair, which
// keeps what the first install recorded.
func TestARepairOfATakenOverNodeKeepsWhatTheMarkerSays(t *testing.T) {
	r := newInstallRig(t)
	r.asksToTakeOver(pinnedSHA())
	theirXray{version: "26.9.1", unit: "etc", files: []string{"config.json"}, runs: true}.put(r.sb)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	first := r.marker()
	// the push put Xray on, which would look like a foreign one but for the marker
	r.sb.afterProvisioning()
	r.opts = InstallOptions{}
	r.steps = nil
	res, err := r.install()
	if err != nil || !res.Repaired || res.TookOver {
		t.Fatalf("a repair: res %+v, err %v", res, err)
	}
	if now := r.marker(); !now.XrayTakenOver || now.XrayWas != "26.9.1" || !now.XrayBinary || !now.InstalledAt.Equal(first.InstalledAt) {
		t.Errorf("the repair changed what the marker says: %+v", now)
	}
	if got, want := r.stepNames(), InstallPlan(); !slices.Equal(got, want) {
		t.Errorf("steps %q, want %q", got, want)
	}
}

// An install that wasn't asked for a takeover never says a word of one: its steps
// are the plan's, and none of them is the takeover's, whatever the server has.
func TestAPlainInstallNeverMentionsTheTakeoverStep(t *testing.T) {
	r := newInstallRig(t)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	for _, s := range r.steps {
		if s.Name == StepTakeOverXray {
			t.Fatalf("a plain install reported %+v", s)
		}
	}
}

// A job that asked for a takeover has the step, and one that didn't never shows
// it: the plan says so before the install starts.
func TestTheTakeoverStepIsInThePlanOnlyWhenAskedFor(t *testing.T) {
	if got, want := InstallPlanFor(InstallOptions{}), InstallPlan(); !slices.Equal(got, want) {
		t.Errorf("without the option %q, want %q", got, want)
	}
	plan := InstallPlanFor(InstallOptions{TakeOverXray: true})
	i := slices.Index(plan, StepTakeOverXray)
	// after what only looks and fetches, and before the server is written to
	if i < 0 || plan[i-1] != StepCertificate || plan[i+1] != StepPrepareServer || len(plan) != len(InstallPlan())+1 {
		t.Errorf("plan %q", plan)
	}
	// and asking again does not grow the plan it was built from
	if len(InstallPlan()) != 7 {
		t.Errorf("InstallPlan changed: %q", InstallPlan())
	}
}

// A login that isn't root, on a server set up by the official Xray script: its
// config folder is 750 root:xray, so the login can't open it, and /proc may be
// mounted so that it sees only its own processes. Whatever of the server's own
// Xray is read, and whatever of it is taken, goes through sudo.
func (r *installRig) asASudoUserWhoSeesLittle() {
	r.t.Helper()
	r.shell.root, r.shell.password = false, "s3cret-sudo"
	r.sb.env["FAKE_UID"], r.sb.env["FAKE_SUDO_PW"] = "1000", "s3cret-sudo"
	r.login = Login{Password: "s3cret-sudo"}
	r.shell.rootOnly = []string{"/usr/local/etc/xray", "/proc/[0-9]*"}
}

// the official script's layout: its drop-in, and the config in a folder only root can open
func officialXray(version string) theirXray {
	return theirXray{version: version, unit: "etc", dropIns: []string{"10-donot_touch_single_conf.conf"}, files: []string{"config.json"}, runs: true}
}

func TestWhatAServersOwnXrayIsIsReadAsRootNotAsTheLogin(t *testing.T) {
	t.Run("a refusal sees the process and says so", func(t *testing.T) {
		// only a process, which the login can't see: nothing else says there is an Xray
		r := newInstallRig(t)
		r.asASudoUserWhoSeesLittle()
		theirXray{runs: true}.put(r.sb)
		before := r.sb.snapshot()
		res, err := r.install()
		var he *ServerHasXrayError
		if !errors.As(err, &he) || !he.Process || he.Binary || he.Unit {
			t.Fatalf("err = %v (%+v)", err, he)
		}
		if res.Changed {
			t.Errorf("result %+v", res)
		}
		if d := diff(before, r.sb.snapshot()); len(d) != 0 {
			t.Errorf("the server changed:\n%s", strings.Join(d, "\n"))
		}
	})

	t.Run("a refusal reads the build of a binary the login can't open", func(t *testing.T) {
		// a binary only root can run, as on a server that closes its programs: the version and checksum are read as root
		r := newInstallRig(t)
		r.asASudoUserWhoSeesLittle()
		r.shell.rootOnly = append(r.shell.rootOnly, "/usr/local/bin/xray")
		r.env.pinnedBuild = func(string) (string, string) { return pin.Version, pinnedSHA() }
		theirXray{version: pin.Version, unit: "etc"}.put(r.sb)
		_, err := r.install()
		var he *ServerHasXrayError
		if !errors.As(err, &he) || he.Version != pin.Version || !he.SameBuild || !he.Binary {
			t.Fatalf("err = %v (%+v)", err, he)
		}
	})

	t.Run("a takeover lists what it removes, and removes it", func(t *testing.T) {
		r := newInstallRig(t)
		r.asASudoUserWhoSeesLittle()
		r.asksToTakeOver(pinnedSHA())
		x := officialXray("26.9.1")
		x.files = []string{"config.json", "inbounds.json"}
		x.put(r.sb)
		if _, err := r.install(); err != nil {
			t.Fatal(err)
		}
		// the two files in the closed folder were seen, which only root could, and are gone
		if d := r.lastStep(StepTakeOverXray).Detail; !strings.Contains(d, "1 drop-in, 2 config files") {
			t.Errorf("the takeover step says %q", d)
		}
		if entries, _ := os.ReadDir(r.sb.path("/usr/local/etc/xray")); len(entries) != 0 {
			t.Errorf("their config is still there: %v", entries)
		}
		if r.sb.exists("/etc/systemd/system/xray.service.d") || r.sb.exists("/proc/812") || r.sb.exists("/active/xray") {
			t.Error("something of theirs is left")
		}
		// nothing was asked of a place the login can't see by the login: every such command went through sudo
		for _, c := range r.sb.commands() {
			if !c.Root && strings.Contains(c.Cmd, "/.closed") {
				t.Errorf("the login ran %q against what only root sees", c.Cmd)
			}
		}
	})

	// a config that was there before is not the install's, whether or not the login could see it
	t.Run("a config the login can't see is a config that was there", func(t *testing.T) {
		r := newInstallRig(t)
		r.asASudoUserWhoSeesLittle()
		r.sb.put("/usr/local/etc/xray/config.json", `{"theirs":true}`, 0o640)
		if _, err := r.install(); err != nil {
			t.Fatal(err)
		}
		if mk := r.marker(); mk.XrayConfig {
			t.Errorf("the marker claims a config that was theirs: %+v", mk)
		}
	})
}

// Removing a node whose Xray was taken over, and cleaning up after an install
// that failed once it had, reach the folders only root can open.
func TestTakingAnXrayOffAFolderOnlyRootCanOpen(t *testing.T) {
	for _, how := range []string{"removing the node", "cleaning up after a failed install"} {
		t.Run(how, func(t *testing.T) {
			r := newInstallRig(t)
			r.asASudoUserWhoSeesLittle()
			r.asksToTakeOver(pinnedSHA())
			officialXray("26.9.1").put(r.sb)
			if how == "cleaning up after a failed install" {
				r.verifyErr = errors.New("the agent doesn't answer")
			}
			res, err := r.install()
			if how == "removing the node" {
				if err != nil {
					t.Fatal(err)
				}
				r.sb.afterProvisioning() // the config the hub pushed, in the closed folder
				must(t, r.uninstall())
			} else {
				if err == nil || !res.TookOver {
					t.Fatalf("res %+v, err %v", res, err)
				}
				must(t, r.env.UndoInstall(t.Context(), r.node, r.login, r.progress))
			}
			for _, p := range []string{"/usr/local/etc/xray", "/usr/local/bin/xray", "/etc/systemd/system/xray.service", "/etc/systemd/system/xray.service.d", "/var/lib/nasnet-agent", "/etc/nasnet-agent"} {
				if r.sb.exists(p) {
					t.Errorf("%s is still there", p)
				}
			}
			// what the takeover leaves alone is left
			if !r.sb.exists("/usr/local/share/xray/geoip.dat") || !r.sb.exists("/var/log/xray/access.log") {
				t.Error("assets or logs were taken")
			}
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
