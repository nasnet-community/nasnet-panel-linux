// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nasnet-community/mtproto-remote-plugin/installer"
)

// What the install does about a tunnel connection that has gone away: the
// kernel gives one up after about fifteen minutes by default, and with
// net.ipv4.tcp_retries2 = 5 after about a minute. That is the setting these
// tests are about. The sandbox keeps the live value in a file, as /proc does.

const (
	liveRetries2 = "/proc/sys/net/ipv4/tcp_retries2"
	retries2File = "/etc/sysctl.d/60-nasnet-agent.conf"
	markerFile   = "/var/lib/nasnet-agent/remote-nodes.json"
)

// live is the kernel setting as the server has it, without the newline.
func (sb *sandbox) live() string { return strings.TrimSpace(sb.read(liveRetries2)) }

func (r *installRig) markerOnServer() marker {
	r.t.Helper()
	var mk marker
	if err := json.Unmarshal([]byte(r.sb.read(markerFile)), &mk); err != nil {
		r.t.Fatal(err)
	}
	return mk
}

// An install writes the setting where the server reads it at boot, and makes
// it live. The file is something systemd-sysctl takes: comments, then
// "key = value" lines.
func TestTheInstallMakesTheKernelSettingLiveAndKeepsItForTheNextBoot(t *testing.T) {
	r := newInstallRig(t)
	if got := r.sb.live(); got != "15" {
		t.Fatalf("the sandbox's server starts with %q", got)
	}
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	if got := r.sb.live(); got != "5" {
		t.Errorf("net.ipv4.tcp_retries2 is %q after the install, want 5", got)
	}
	if got := r.sb.mode(retries2File); got != 0o644 {
		t.Errorf("the file is %v", got)
	}
	text := r.sb.read(retries2File)
	var settings []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			settings = append(settings, line)
		}
	}
	if !slices.Equal(settings, []string{"net.ipv4.tcp_retries2 = 5"}) {
		t.Errorf("the file holds %q:\n%s", settings, text)
	}
	if !strings.HasPrefix(text, "# Written by the V2ray/Xray plugin") {
		t.Errorf("the file doesn't say whose it is:\n%s", text)
	}
	if mk := r.markerOnServer(); mk.TCPRetries2Was != "15" {
		t.Errorf("the marker says the setting was %q", mk.TCPRetries2Was)
	}
	if d := r.lastStep(StepPrepareServer); d.State != StateOK || d.Detail != "" {
		t.Errorf("a server that took the setting has a note: %+v", d)
	}
}

// The setting helps and the node doesn't need it. A server that refuses it
// (a container whose kernel settings are read-only) still gets the agent; the
// step says what it did not get, and nothing half-made is left.
func TestAServerThatWontTakeTheSettingStillGetsTheAgent(t *testing.T) {
	refused := &installer.CommandError{
		Cmd: "echo", Stderr: "sh: 1: cannot create /proc/sys/net/ipv4/tcp_retries2: Read-only file system\n",
		Err: errors.New("Process exited with status 2"),
	}
	for _, c := range []struct {
		name    string
		fail    func(cmd string) bool
		mention string
		file    bool // the file got written
		live    string
	}{
		{"the live value is refused", func(cmd string) bool { return cmd == cmdSetRetries2 }, "Read-only file system", false, "15"},
		{"the folder can't be made", func(cmd string) bool { return cmd == cmdMkSysctlDir }, "Couldn't make /etc/sysctl.d", false, "5"},
		{"the file can't be written", func(cmd string) bool { return strings.Contains(cmd, "/etc/sysctl.d/60-nasnet-agent.conf.new") }, "Couldn't write /etc/sysctl.d/60-nasnet-agent.conf", false, "5"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newInstallRig(t)
			r.sb.decoys() // a server with other things on it, so that what is left shows
			before := r.sb.snapshot()
			r.shell.failOn = func(cmd string) error {
				if c.fail(cmd) {
					return refused
				}
				return nil
			}
			res, err := r.install()
			if err != nil {
				t.Fatalf("the install failed over a setting it can do without: %v", err)
			}
			if !res.Changed || res.AgentVersion == "" {
				t.Errorf("result %+v", res)
			}
			step := r.lastStep(StepPrepareServer)
			if step.State != StateOK || !strings.Contains(step.Detail, c.mention) || !strings.Contains(step.Detail, "takes longer to drop a tunnel connection") {
				t.Errorf("the step says %+v, want a note with %q", step, c.mention)
			}
			if r.sb.exists(retries2File) != c.file || r.sb.exists(retries2File+".new") {
				t.Errorf("the file exists: %v, half-written one: %v", r.sb.exists(retries2File), r.sb.exists(retries2File+".new"))
			}
			if got := r.sb.live(); got != c.live {
				t.Errorf("the live value is %q, want %q", got, c.live)
			}
			// the agent is there and runs
			if !r.sb.exists("/active/nasnet-agent") {
				t.Error("the agent is not running")
			}

			// and removal puts the server back, whichever half of it was made
			r.shell.failOn = nil
			if err := r.uninstall(); err != nil {
				t.Fatal(err)
			}
			if d := diff(before, r.sb.snapshot()); len(d) != 0 {
				t.Errorf("not as it was:\n%s", strings.Join(d, "\n"))
			}
		})
	}
}

// A server with no such setting, which is a kernel without the file and not
// one that refuses it, is left alone, and the step says so.
func TestAServerWithNoSettingToReadIsLeftAlone(t *testing.T) {
	r := newInstallRig(t)
	r.sb.decoys()
	os.Remove(r.sb.path(liveRetries2))
	before := r.sb.snapshot()
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	if r.sb.exists(retries2File) || r.sb.exists(liveRetries2) {
		t.Error("something was made for a setting the server doesn't have")
	}
	for _, cmd := range r.ran() {
		if strings.Contains(cmd, "tcp_retries2") && !strings.HasPrefix(cmd, "cat ") {
			t.Errorf("the install touched the setting: %s", cmd)
		}
	}
	if d := r.lastStep(StepPrepareServer).Detail; !strings.Contains(d, "no net.ipv4.tcp_retries2 setting") {
		t.Errorf("the step says %q", d)
	}
	if err := r.uninstall(); err != nil {
		t.Fatal(err)
	}
	if d := diff(before, r.sb.snapshot()); len(d) != 0 {
		t.Errorf("not as it was:\n%s", strings.Join(d, "\n"))
	}
}

// What removal gives back is what the server had, and only while the value is
// still the install's own. The file goes either way.
func TestUninstallPutsTheKernelSettingBackOnlyIfItIsStillTheInstalls(t *testing.T) {
	for _, c := range []struct {
		name      string
		before    string // the server's value before the install
		afterward func(r *installRig)
		want      string // the value after removal
	}{
		{"as installed", "15", nil, "15"},
		{"a server that had tuned it", "8", nil, "8"},
		{"a server that had set it to the same", "5", nil, "5"},
		{"someone changed it since", "15", func(r *installRig) { r.sb.put(liveRetries2, "8\n", 0o644) }, "8"},
		{"someone changed it since, on a tuned server", "8", func(r *installRig) { r.sb.put(liveRetries2, "12\n", 0o644) }, "12"},
		{"someone set it back to the default", "15", func(r *installRig) { r.sb.put(liveRetries2, "15\n", 0o644) }, "15"},
		{"the marker is gone", "8", func(r *installRig) { os.Remove(r.sb.path(markerFile)) }, "15"},
		{"an install that never read it", "15", func(r *installRig) { r.dropFromMarker("tcp_retries2_was") }, "15"},
		{"a marker with something else in the value", "8", func(r *installRig) { r.setInMarker("tcp_retries2_was", "1; touch /etc/pwned") }, "15"},
		{"a marker with a number that is too long", "8", func(r *installRig) { r.setInMarker("tcp_retries2_was", "123456") }, "15"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newInstallRig(t)
			r.sb.put(liveRetries2, c.before+"\n", 0o644)
			if _, err := r.install(); err != nil {
				t.Fatal(err)
			}
			if got := r.sb.live(); got != "5" {
				t.Fatalf("the install left %q", got)
			}
			if c.afterward != nil {
				c.afterward(r)
			}
			r.sb.forget()
			if err := r.uninstall(); err != nil {
				t.Fatal(err)
			}
			if got := r.sb.live(); got != c.want {
				t.Errorf("net.ipv4.tcp_retries2 is %q after removal, want %q", got, c.want)
			}
			if r.sb.exists(retries2File) || r.sb.exists(retries2File+".new") {
				t.Error("the file stays")
			}
			for _, cmd := range r.ran() {
				if strings.Contains(cmd, "pwned") || strings.Contains(cmd, "123456") {
					t.Errorf("what the marker said went into a command: %s", cmd)
				}
			}
		})
	}
}

// A server that was installed before the setting existed has no file and no
// field in its marker, and a value that was never the install's: removal leaves
// the value as it is, whatever it is.
func TestUninstallOfAnInstallThatNeverMadeTheSettingLeavesTheServersValue(t *testing.T) {
	for _, value := range []string{"15", "8", "5"} {
		r := newInstallRig(t)
		if _, err := r.install(); err != nil {
			t.Fatal(err)
		}
		os.Remove(r.sb.path(retries2File))
		r.dropFromMarker("tcp_retries2_was")
		r.sb.put(liveRetries2, value+"\n", 0o644)
		if err := r.uninstall(); err != nil {
			t.Fatal(err)
		}
		// 5 is the install's value, and with nothing to say it was somebody's, it goes back to the default
		want := value
		if value == "5" {
			want = "15"
		}
		if got := r.sb.live(); got != want {
			t.Errorf("a value of %s became %q", value, got)
		}
	}
}

// A second install over the hub's own agent is a repair, and the value it reads
// then is the first install's own: the marker keeps what the first one saw.
func TestARepairKeepsWhatTheFirstInstallSawOfTheSetting(t *testing.T) {
	r := newInstallRig(t)
	r.sb.put(liveRetries2, "8\n", 0o644)
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	r.sb.listening(443, "xray")
	if _, err := r.install(); err != nil {
		t.Fatalf("the repair: %v", err)
	}
	if mk := r.markerOnServer(); mk.TCPRetries2Was != "8" {
		t.Errorf("after the repair the marker says the setting was %q, want 8", mk.TCPRetries2Was)
	}
	if got := r.sb.live(); got != "5" {
		t.Errorf("the repair left %q", got)
	}
	if err := r.uninstall(); err != nil {
		t.Fatal(err)
	}
	if got := r.sb.live(); got != "8" {
		t.Errorf("after removal the setting is %q, want what the server had at first", got)
	}
}

// retries2Value is all that stands between a marker or a server and a root
// command, so it takes a plain number and nothing else.
func TestOnlyAPlainNumberIsTakenForTheSetting(t *testing.T) {
	for in, want := range map[string]string{
		"15": "15", "15\n": "15", " 8 ": "8", "5": "5", "0": "0", "255": "255",
		"": "", "\n": "", "abc": "", "1.5": "", "-1": "", "+5": "", "1 2": "", "1234": "", "١٥": "", "5;id": "", "$(id)": "", "'5'": "",
	} {
		if got := retries2Value(in); got != want {
			t.Errorf("retries2Value(%q) = %q, want %q", in, got, want)
		}
	}
}

// The command that gives the value back is a plain line, and what it puts back
// is quoted.
func TestTheCommandThatPutsTheSettingBackIsExact(t *testing.T) {
	want := `[ "$(cat '/proc/sys/net/ipv4/tcp_retries2' 2>/dev/null)" = '5' ] && echo '15' > '/proc/sys/net/ipv4/tcp_retries2' || true`
	if got := cmdRestoreRetries2("15"); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if cmdSetRetries2 != `echo '5' > '/proc/sys/net/ipv4/tcp_retries2'` || cmdReadRetries2 != `cat '/proc/sys/net/ipv4/tcp_retries2' 2>/dev/null || true` {
		t.Errorf("%s | %s", cmdSetRetries2, cmdReadRetries2)
	}
}

// setInMarker and dropFromMarker edit the marker on the server as a test
// needs it, as JSON, so that a field the code doesn't know about is kept.
func (r *installRig) setInMarker(key string, value any) {
	r.t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal([]byte(r.sb.read(markerFile)), &m); err != nil {
		r.t.Fatal(err)
	}
	m[key] = value
	b, _ := json.Marshal(m)
	r.sb.put(markerFile, string(b)+"\n", 0o644)
}

func (r *installRig) dropFromMarker(key string) {
	r.t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal([]byte(r.sb.read(markerFile)), &m); err != nil {
		r.t.Fatal(err)
	}
	delete(m, key)
	b, _ := json.Marshal(m)
	r.sb.put(markerFile, string(b)+"\n", 0o644)
}
