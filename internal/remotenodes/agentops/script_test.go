// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestQuotingKeepsAPathInOneWord(t *testing.T) {
	// what the shell makes of the quoted text is the text, whatever is in it
	for _, s := range []string{"/etc/nasnet-agent", "it's here", `a "quoted" word`, "with space", "$HOME", "`id`", "a;b", "*", ""} {
		out, err := exec.Command("sh", "-c", "printf %s "+shq(s)).Output()
		if err != nil || string(out) != s {
			t.Errorf("shq(%q) came out as %q (%v)", s, out, err)
		}
	}
	if got := quoted("/a b", "/c"); got != "'/a b' '/c'" {
		t.Errorf("quoted = %s", got)
	}
}

// cmdWrite runs for real here, with the paths in a temp directory, to show
// what it does: the file arrives whole, with the mode, and nobody else could
// have read it while it was written.
func TestCmdWriteWritesWholeFilesWithTheirMode(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []string{"0600", "0644"} {
		path := filepath.Join(dir, "it's a file "+mode)
		c := exec.Command("sh", "-c", "umask 022 && "+cmdWrite(path, mode)) // a looser umask than the command's own
		c.Stdin = strings.NewReader("contents " + mode)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", mode, err, out)
		}
		fi, err := statMode(path)
		if err != nil || fi != mode {
			t.Errorf("mode %s: the file is %s (%v)", mode, fi, err)
		}
		if _, err := statMode(path + ".new"); err == nil {
			t.Errorf("mode %s: the temporary file is still there", mode)
		}
	}
	// the key's mode comes from umask, so it was never readable by anyone else, not even for an instant
	if !strings.HasPrefix(cmdWrite("/x", "0600"), "umask 077 && cat > ") || strings.Contains(cmdWrite("/x", "0600"), "chmod") {
		t.Errorf("a 0600 file: %s", cmdWrite("/x", "0600"))
	}
}

func TestParseUnameAndArch(t *testing.T) {
	for in, want := range map[string][2]string{
		"Linux x86_64\n": {"Linux", "x86_64"}, "Linux aarch64": {"Linux", "aarch64"}, "": {"", ""}, "Linux": {"", ""},
	} {
		if os, m := parseUname(in); os != want[0] || m != want[1] {
			t.Errorf("parseUname(%q) = %q, %q", in, os, m)
		}
	}
	for in, want := range map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64", "armv7l": "", "i686": "", "riscv64": "", "": ""} {
		if got, ok := archOf(in); got != want || ok != (want != "") {
			t.Errorf("archOf(%q) = %q, %v", in, got, ok)
		}
	}
}

func TestParseListeners(t *testing.T) {
	ss := `State   Recv-Q  Send-Q   Local Address:Port    Peer Address:Port  Process
LISTEN  0       128            0.0.0.0:22           0.0.0.0:*      users:(("sshd",pid=801,fd=3))
LISTEN  0       4096                 *:9090               *:*      users:(("nasnet-agent",pid=4242,fd=7))
LISTEN  0       511                  *:443                *:*      users:(("nginx",pid=77,fd=6),("nginx",pid=78,fd=6),("xray",pid=9,fd=4))
LISTEN  0       128               [::]:22              [::]:*      users:(("sshd",pid=801,fd=4))
LISTEN  0       4096         127.0.0.1:10085          0.0.0.0:*
LISTEN  0       4096    [fe80::1%eth0]:8080              [::]:*
ESTAB   0       0           10.0.0.5:22          10.0.0.9:55012
garbage
`
	got := parseListeners(ss)
	want := map[int][]string{22: {"sshd"}, 9090: {"nasnet-agent"}, 443: {"nginx", "xray"}, 10085: nil, 8080: nil}
	for p, names := range want {
		if g, ok := got[p]; !ok || !slices.Equal(g, names) {
			t.Errorf("port %d: %v (listed %v), want %v", p, g, ok, names)
		}
	}
	if len(got) != len(want) {
		t.Errorf("ports %v", slices.Sorted(maps.Keys(got)))
	}
	if len(parseListeners("")) != 0 {
		t.Error("an empty answer has ports")
	}
}

func TestParseProcNetReadsOnlyListeningPorts(t *testing.T) {
	out := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1 0000
   1: 0100007F:2765 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 2 1 0000
   2: 0500000A:0016 0900000A:D8E6 01 00000000:00000000 02:00000000 00000000     0        0 3 2 0000
   3: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 4 1 0000
`
	got := parseProcNet(out)
	for _, p := range []int{22, 10085, 443} {
		if _, ok := got[p]; !ok {
			t.Errorf("port %d is missing: %v", p, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("%v: an established connection counted as a listener", got)
	}
}

func TestParsePresentOnlyTakesWhatWasAskedAbout(t *testing.T) {
	got := parsePresent("/usr/local/bin/xray\n/etc/passwd\n  /usr/local/etc/xray/config.json \n\nnoise")
	if len(got) != 2 || !got[xrayBin] || !got[xrayConfPath] {
		t.Errorf("%v", got)
	}
}

func TestAllowedByUFW(t *testing.T) {
	status := `Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere
443                        ALLOW       Anywhere
9090/udp                   ALLOW       Anywhere
8000:8100/tcp              ALLOW       Anywhere
3306/tcp                   DENY        Anywhere
5432/tcp                   ALLOW       10.0.0.0/8
22/tcp (v6)                ALLOW       Anywhere (v6)
`
	got := allowedByUFW(status)
	slices.Sort(got)
	if want := []int{22, 443, 5432}; !slices.Equal(got, want) {
		t.Errorf("allowed %v, want %v", got, want)
	}
}

func TestTheMarkerRoundTripsAndIsPlain(t *testing.T) {
	in := &marker{Version: markerVersion, NodeID: 3, CASHA256: "ab", InstalledAt: time.Date(2026, 10, 30, 17, 0, 0, 0, time.UTC),
		AgentPort: 9090, EntryPort: 443, Agent: true, XrayBinary: true, Firewall: []int{443, 9090}}
	out, err := parseMarker(string(in.bytes()))
	if err != nil || out == nil || out.NodeID != 3 || !out.XrayBinary || out.XrayUnit || !slices.Equal(out.Firewall, []int{443, 9090}) || !out.InstalledAt.Equal(in.InstalledAt) {
		t.Fatalf("%+v, %v", out, err)
	}
	if m, err := parseMarker("  \n"); m != nil || err != nil {
		t.Errorf("an empty marker: %v, %v", m, err)
	}
	if _, err := parseMarker("{not json"); err == nil {
		t.Error("a broken marker was read")
	}
	if got := union([]int{9090, 443}, []int{443, 22}); !slices.Equal(got, []int{22, 443, 9090}) {
		t.Errorf("union %v", got)
	}
}

func TestSentenceAndTail(t *testing.T) {
	for in, want := range map[string]string{
		"couldn't write it": "Couldn't write it.", "Already a sentence.": "Already a sentence.", "ends with a question?": "Ends with a question?",
		"two\nlines": "Two\nlines", "": "", "  padded  ": "Padded.",
	} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q, want %q", in, got, want)
		}
	}
	if got := tail("a\nb\nc\nd\n", 5); got != "c\nd" {
		t.Errorf("tail = %q", got)
	}
	if got := tail("short", 50); got != "short" {
		t.Errorf("tail = %q", got)
	}
}
