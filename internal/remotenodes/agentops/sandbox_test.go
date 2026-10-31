// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nasnet-community/mtproto-remote-plugin/installer"
)

// A sandbox is a server for the install to work on: a directory that stands
// for its root, and the tools a command meets there. The commands are the
// install's own, run by a real shell, so quoting, redirections, umask, moves
// and the unpacking of the upload are the real ones. Only what a test can't
// have is a stand-in: systemctl, ss, sudo, ufw, uname and the like are
// scripts that answer from files in the sandbox.
//
// The absolute paths a command names are rewritten into the sandbox. What a
// command is sent as is what gets recorded, so a test says what ran on the
// server.
type sandbox struct {
	t    *testing.T
	root string
	bin  string
	env  map[string]string
	// the prefixes of the system's folders, each moved to the sandbox's
	rewrite *strings.Replacer

	mu   sync.Mutex
	runs []run
}

// run is one command as the server got it.
type run struct {
	Cmd   string
	Stdin []byte // up to 64 KiB of it
	Root  bool   // run through Root, not Run
	Code  int
}

const recordedStdin = 64 << 10

// the sandbox rewrites these prefixes, which is every place a command goes
var systemPrefixes = []string{"/usr/local/", "/etc/", "/var/lib/", "/var/log/", "/run/", "/proc/"}

// scripts are the tools that aren't the machine's own. $ROOT is the sandbox.
var scripts = map[string]string{
	"uname": `case "$*" in
 "-sm") echo "${FAKE_OS:-Linux} ${FAKE_ARCH:-x86_64}";;
 "-m") echo "${FAKE_ARCH:-x86_64}";;
 "-s") echo "${FAKE_OS:-Linux}";;
 *) command -p uname "$@";;
esac`,
	"id": `case "$*" in "-u") echo "${FAKE_UID:-0}";; *) command -p id "$@";; esac`,
	// like the real one: with NOPASSWD it never reads stdin, even with -S or -k
	"sudo": `nopw=; stdinpw=; k=
while [ $# -gt 0 ]; do case "$1" in -n) nopw=n; shift;; -S) stdinpw=S; shift;; -k) k=k; shift;; -p) shift 2;; *) break;; esac; done
echo "sudo -$nopw$stdinpw$k" >> "$ROOT/sudo.log"
if [ -n "$FAKE_SUDO_NOPASSWD" ]; then :
elif [ -n "$nopw" ]; then echo "sudo: a password is required" >&2; exit 1
elif [ -n "$stdinpw" ]; then read -r pw; [ "$pw" = "$FAKE_SUDO_PW" ] || { echo "sudo: incorrect password attempt" >&2; exit 1; }
else echo "sudo: a terminal is required to read the password" >&2; exit 1
fi
FAKE_UID=0 exec "$@"`,
	"systemctl": `echo "$*" >> "$ROOT/systemctl.log"
unit() { echo "${1%.service}"; }
down() { grep -v "$1" "$ROOT/listening" > "$ROOT/listening.tmp" 2>/dev/null; mv "$ROOT/listening.tmp" "$ROOT/listening"; }
# the processes a unit runs end with it, and so do the sockets they held; starting it
# again brings them back, so a test can tell a server that was put back from one that wasn't
reap() { mkdir -p "$ROOT/reaped"; for d in "$ROOT"/proc/[0-9]*; do [ -e "$d/unit" ] && [ "$(cat "$d/unit")" = "$1" ] && { pid=${d##*/}; grep "pid=$pid," "$ROOT/listening" >> "$ROOT/reaped/$1.listen" 2>/dev/null; down "pid=$pid,"; mv "$d" "$ROOT/reaped/$1.$pid"; }; done; }
revive() { for f in "$ROOT"/reaped/"$1".[0-9]*; do [ -e "$f" ] && mv "$f" "$ROOT/proc/${f##*.}"; done; [ -e "$ROOT/reaped/$1.listen" ] && { cat "$ROOT/reaped/$1.listen" >> "$ROOT/listening"; rm -f "$ROOT/reaped/$1.listen"; }; return 0; }
case "$1" in
 is-active) u=$(unit "$2"); if [ -e "$ROOT/active/$u" ]; then echo active; else echo inactive; exit 3; fi;;
 cat)
   u=$(unit "$2")
   # like systemd: a comment with where the unit is, then the unit
   for d in etc/systemd/system run/systemd/system lib/systemd/system usr/lib/systemd/system; do
     [ -e "$ROOT/$d/$u.service" ] && { echo "# /$d/$u.service"; cat "$ROOT/$d/$u.service"; exit 0; }
   done
   echo "No files found for $u.service." >&2; exit 1;;
 enable|daemon-reload) ;;
 stop)
   [ -n "$FAKE_STOP_FAILS" ] && { echo "Failed to stop $2: Access denied" >&2; exit 1; }
   u=$(unit "$2")
   rm -f "$ROOT/active/$u"; reap "$u"
   [ "$u" = nasnet-agent ] && down nasnet-agent
   exit 0;;
 restart|start)
   u=$(unit "$2")
   [ -n "$FAKE_START_FAILS" ] && { echo "Failed to start $2: Job failed" >&2; exit 1; }
   [ -n "$FAKE_NO_START" ] && exit 0
   [ -e "$ROOT/etc/systemd/system/$u.service" ] || [ -e "$ROOT/lib/systemd/system/$u.service" ] || { echo "Failed to restart $u.service: Unit $u.service not found." >&2; exit 5; }
   mkdir -p "$ROOT/active"; touch "$ROOT/active/$u"; revive "$u"
   if [ "$u" = nasnet-agent ]; then
     port=$(sed -n 's/^listen_addr: *"*:\([0-9]*\)"*.*/\1/p' "$ROOT/etc/nasnet-agent/config.yaml" | head -1)
     down nasnet-agent
     echo "LISTEN 0      4096         *:$port      *:*    users:((\"nasnet-agent\",pid=4242,fd=7))" >> "$ROOT/listening"
   fi;;
 disable)
   if [ "$2" = "--now" ]; then u=$(unit "$3"); else u=$(unit "$2"); fi
   [ "$2" = "--now" ] && { rm -f "$ROOT/active/$u"; reap "$u"; }
   [ "$u" = nasnet-agent ] && [ "$2" = "--now" ] && down nasnet-agent
   exit 0;;
esac
exit 0`,
	"ss": `echo "State  Recv-Q Send-Q Local Address:Port Peer Address:Port Process"
cat "$ROOT/listening" 2>/dev/null
exit 0`,
	"sha256sum": `[ -n "$FAKE_BAD_SHA" ] && { echo "0000000000000000000000000000000000000000000000000000000000000000  $1"; exit 0; }
command -p sha256sum "$@" 2>/dev/null && exit 0
exec shasum -a 256 "$@"`,
	"journalctl": `echo "$FAKE_JOURNAL"`,
	"ufw": `case "$1" in
 status) if [ -e "$ROOT/ufw.active" ]; then echo "Status: active"; echo; echo "To                         Action      From"; echo "--                         ------      ----"; cat "$ROOT/ufw.rules" 2>/dev/null; else echo "Status: inactive"; fi;;
 allow) echo "$*" >> "$ROOT/ufw.log"; echo "$2                   ALLOW       Anywhere" >> "$ROOT/ufw.rules";;
 delete) echo "$*" >> "$ROOT/ufw.log"; grep -v "^$3 " "$ROOT/ufw.rules" > "$ROOT/ufw.tmp"; mv "$ROOT/ufw.tmp" "$ROOT/ufw.rules";;
esac
exit 0`,
}

// the machine's own tools a command may use
var realTools = []string{"cat", "chmod", "mv", "rm", "rmdir", "mkdir", "install", "gzip", "gunzip", "sleep", "sh", "env", "head", "tail", "tr", "sed", "grep", "dirname", "basename", "ls", "touch", "printf", "expr", "cp"}

func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	root, err := os.MkdirTemp("", "node")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	sb := &sandbox{t: t, root: root, bin: filepath.Join(root, "fakebin"), env: map[string]string{"FAKE_UID": "0", "FAKE_SUDO_PW": "sudopw"}}
	for _, d := range []string{"fakebin", "etc/systemd/system", "etc/sysctl.d", "usr/local/bin", "var/lib", "var/log", "run/systemd/system", "proc/net", "proc/sys/net/ipv4", "active"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range scripts {
		if name == "ufw" {
			continue // a server has it when a test says so
		}
		sb.addScript(name, body)
	}
	for _, name := range realTools {
		if p, err := exec.LookPath(name); err == nil {
			os.Symlink(p, filepath.Join(sb.bin, name))
		}
	}
	sb.put("/etc/os-release", "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\n", 0o644)
	// the kernel's own default, as a server that nobody has tuned has it
	sb.put("/proc/sys/net/ipv4/tcp_retries2", "15\n", 0o644)
	var pairs []string
	for _, p := range systemPrefixes {
		pairs = append(pairs, p, root+p)
	}
	sb.rewrite = strings.NewReplacer(pairs...)
	return sb
}

func (sb *sandbox) addScript(name, body string) {
	sb.t.Helper()
	if err := os.WriteFile(filepath.Join(sb.bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		sb.t.Fatal(err)
	}
}

// withUFW gives the server ufw, active or not, with rules that allow these ports.
func (sb *sandbox) withUFW(active bool, allowed ...int) {
	sb.t.Helper()
	sb.addScript("ufw", scripts["ufw"])
	if active {
		sb.put("/ufw.active", "", 0o644)
	}
	var rules strings.Builder
	for _, p := range allowed {
		fmt.Fprintf(&rules, "%d/tcp                   ALLOW       Anywhere\n", p)
	}
	sb.put("/ufw.rules", rules.String(), 0o644)
}

// without takes tools off the server.
func (sb *sandbox) without(names ...string) {
	for _, n := range names {
		os.Remove(filepath.Join(sb.bin, n))
	}
}

// listening makes something hold a port, as ss shows it.
func (sb *sandbox) listening(port int, process string) {
	sb.t.Helper()
	line := fmt.Sprintf("LISTEN 0      511          0.0.0.0:%d      0.0.0.0:*    users:((\"%s\",pid=77,fd=6))\n", port, process)
	if process == "" {
		line = fmt.Sprintf("LISTEN 0      511          0.0.0.0:%d      0.0.0.0:*\n", port)
	}
	f, err := os.OpenFile(sb.path("/listening"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		sb.t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(line)
}

// path is where a file the server calls p is in the sandbox.
func (sb *sandbox) path(p string) string { return filepath.Join(sb.root, p) }

func (sb *sandbox) put(p, content string, mode os.FileMode) {
	sb.t.Helper()
	full := sb.path(p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		sb.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), mode); err != nil {
		sb.t.Fatal(err)
	}
	os.Chmod(full, mode)
}

func (sb *sandbox) read(p string) string {
	b, err := os.ReadFile(sb.path(p))
	if err != nil {
		return ""
	}
	return string(b)
}

func (sb *sandbox) exists(p string) bool {
	_, err := os.Lstat(sb.path(p))
	return err == nil
}

func (sb *sandbox) mode(p string) os.FileMode {
	fi, err := os.Stat(sb.path(p))
	if err != nil {
		sb.t.Fatalf("%s: %v", p, err)
	}
	return fi.Mode().Perm()
}

// tree lists every file and folder in the sandbox that a test has anything to say about,
// relative to its root, so a test can say what the install left.
func (sb *sandbox) tree(prefixes ...string) []string {
	var out []string
	filepath.Walk(sb.root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(sb.root, p)
		rel = "/" + filepath.ToSlash(rel)
		for _, pre := range prefixes {
			if rel == pre || strings.HasPrefix(rel, pre+"/") {
				out = append(out, rel)
				break
			}
		}
		return nil
	})
	return out
}

// exec runs a command as the server would, and records it. It returns the
// exit status.
func (sb *sandbox) exec(rooted bool, cmd string, stdin io.Reader, stdout, stderr io.Writer) int {
	rec := &bytes.Buffer{}
	if stdin != nil {
		stdin = io.TeeReader(stdin, &capped{w: rec, left: recordedStdin})
	}
	c := exec.Command("sh", "-c", sb.rewrite.Replace(cmd))
	c.Env = append(os.Environ(), "PATH="+sb.bin, "ROOT="+sb.root)
	for k, v := range sb.env {
		c.Env = append(c.Env, k+"="+v)
	}
	// what a command prints about a path names the sandbox's, which the server's own wouldn't
	var out, errb bytes.Buffer
	c.Stdin, c.Stdout, c.Stderr = stdin, &out, &errb
	code := 0
	err := c.Run()
	stdout.Write([]byte(strings.ReplaceAll(out.String(), sb.root, "")))
	stderr.Write([]byte(strings.ReplaceAll(errb.String(), sb.root, "")))
	if err != nil {
		code = 1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	sb.mu.Lock()
	sb.runs = append(sb.runs, run{Cmd: cmd, Stdin: rec.Bytes(), Root: rooted, Code: code})
	sb.mu.Unlock()
	return code
}

type capped struct {
	w    io.Writer
	left int
}

func (c *capped) Write(p []byte) (int, error) {
	if c.left > 0 {
		n := min(len(p), c.left)
		c.w.Write(p[:n])
		c.left -= n
	}
	return len(p), nil
}

// commands is what ran, in order.
func (sb *sandbox) commands() []run {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return append([]run(nil), sb.runs...)
}

// forget drops what was recorded, so a test can look at what comes next.
func (sb *sandbox) forget() {
	sb.mu.Lock()
	sb.runs = nil
	sb.mu.Unlock()
}

// fakeShell is a Shell on a sandbox, where an *installer.Conn would be on a
// server: Root goes through sudo with the password on stdin, as the real one
// does, so what the server is sent is what a real server would be sent.
type fakeShell struct {
	sb       *sandbox
	root     bool
	password string // the login's, for sudo
	nopasswd bool

	mu     sync.Mutex
	failOn func(cmd string) error // a failure to give before a command runs
	closed bool
}

func (f *fakeShell) IsRoot() bool { return f.root }

func (f *fakeShell) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeShell) Run(ctx context.Context, cmd string, stdin io.Reader) (string, error) {
	return f.do(ctx, false, cmd, stdin, nil)
}

func (f *fakeShell) Root(ctx context.Context, cmd string, stdin io.Reader) (string, error) {
	switch {
	case f.root:
		return f.do(ctx, true, cmd, stdin, nil)
	case f.password != "" && !f.nopasswd:
		return f.do(ctx, true, "sudo -k -S -p '' sh -c "+shq(cmd), stdin, []byte(f.password+"\n"))
	}
	return f.do(ctx, true, "sudo -n sh -c "+shq(cmd), stdin, nil)
}

func (f *fakeShell) do(ctx context.Context, rooted bool, cmd string, stdin io.Reader, prefix []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.mu.Lock()
	hook := f.failOn
	f.mu.Unlock()
	if hook != nil {
		if err := hook(cmd); err != nil {
			return "", err
		}
	}
	switch {
	case stdin != nil && prefix != nil:
		stdin = io.MultiReader(bytes.NewReader(prefix), stdin)
	case prefix != nil:
		stdin = bytes.NewReader(prefix)
	}
	var out, errb bytes.Buffer
	if code := f.sb.exec(rooted, cmd, stdin, &out, &errb); code != 0 {
		return out.String(), &installer.CommandError{Cmd: cmd, Stderr: errb.String(), Err: fmt.Errorf("Process exited with status %d", code)}
	}
	return out.String(), nil
}

// snapshot is every file and folder under the places an install writes to,
// with the mode and a hash of the contents of each file, so that a test can
// tell whether the server is as it was. The kernel's settings (/proc) are
// among them. What the stand-in tools keep for themselves (their logs, which
// services run) sits outside these folders.
func (sb *sandbox) snapshot() map[string]string {
	out := map[string]string{}
	filepath.Walk(sb.root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(sb.root, p)
		rel = "/" + filepath.ToSlash(rel)
		keep := false
		for _, pre := range []string{"/etc", "/usr", "/var", "/run", "/proc"} {
			keep = keep || rel == pre || strings.HasPrefix(rel, pre+"/")
		}
		switch {
		case !keep || rel == "/usr/local/bin/sh":
		case fi.IsDir():
			out[rel] = "dir"
		case fi.Mode().IsRegular():
			b, _ := os.ReadFile(p)
			out[rel] = fmt.Sprintf("%04o %s", fi.Mode().Perm(), sum256(b))
		}
		return nil
	})
	return out
}
