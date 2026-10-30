// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// sshRig is an Env with no stand-in for SSH, so what it signs in with is the
// real installer, and a server it reaches through a real connection.
type sshRig struct {
	*installRig
	srv *sshd
}

func newSSHRig(t *testing.T, user, password string) *sshRig {
	t.Helper()
	r := newInstallRig(t)
	r.env.connectSSH, r.env.probeSSH = nil, nil
	r.env.Local = netip.MustParseAddr("127.0.0.1")
	srv := newSSHD(t, r.sb, user, password)
	r.node.Host, r.node.SSHPort, r.node.SSHUser, r.node.SSHHostKey = "127.0.0.1", srv.port(), user, srv.fp
	r.login = Login{Password: password}
	return &sshRig{r, srv}
}

func TestProbeReadsTheHostKeyAndSendsNoLogin(t *testing.T) {
	r := newSSHRig(t, "root", "hunter2")
	fp, err := r.env.Probe(context.Background(), "127.0.0.1", r.srv.port())
	if err != nil {
		t.Fatal(err)
	}
	if fp != r.srv.fp || !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("fingerprint %q, the server's is %q", fp, r.srv.fp)
	}
	if n := r.srv.attempts.Load(); n != 0 {
		t.Errorf("the probe tried to log in %d times", n)
	}
}

func TestProbeOfWhatIsNotSSH(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			c.Close()
		}
	}()
	env := &Env{Local: netip.MustParseAddr("127.0.0.1")}
	port := l.Addr().(*net.TCPAddr).Port
	_, err = env.Probe(context.Background(), "127.0.0.1", port)
	var u *UnreachableError
	if !errors.Is(err, ErrUnreachable) || !errors.As(err, &u) || u.What != "ssh" || u.Port != port {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "SSH") || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("the message doesn't name what it tried: %q", err)
	}

	// a closed port is the same
	l.Close()
	if _, err = env.Probe(context.Background(), "127.0.0.1", port); !errors.Is(err, ErrUnreachable) {
		t.Errorf("a closed port: %v", err)
	}
}

func TestProbeChecksWhatItIsAskedAbout(t *testing.T) {
	env := &Env{}
	for name, c := range map[string]struct {
		host string
		port int
	}{"no host": {"", 22}, "a bad host": {"not a host", 22}, "a bad port": {"203.0.113.7", 70000}, "no port": {"203.0.113.7", -1}} {
		var inv *model.InvalidError
		if _, err := env.Probe(context.Background(), c.host, c.port); !errors.As(err, &inv) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Every SSH connection leaves from the bound address. One the machine doesn't
// have can't be bound, which shows on any system that it is tried, for the
// probe and for the install.
func TestSSHLeavesFromTheLocalAddress(t *testing.T) {
	r := newSSHRig(t, "root", "hunter2")
	r.env.Local = netip.MustParseAddr("192.0.2.1") // TEST-NET-1, nobody's

	_, err := r.env.Probe(context.Background(), "127.0.0.1", r.srv.port())
	if !errors.Is(err, ErrUnreachable) || !errors.Is(err, syscall.EADDRNOTAVAIL) {
		t.Errorf("probe: %v", err)
	}
	res, err := r.install()
	if !errors.Is(err, ErrUnreachable) || !errors.Is(err, syscall.EADDRNOTAVAIL) {
		t.Errorf("install: %v", err)
	}
	if res.Changed || r.srv.attempts.Load() != 0 {
		t.Error("the server heard from a hub that couldn't use its address")
	}
}

func TestAHostNameIsReachedThroughTheSameDialer(t *testing.T) {
	r := newSSHRig(t, "root", "hunter2")
	r.node.Host = "localhost"
	fp, err := r.env.Probe(context.Background(), "localhost", r.srv.port())
	if err != nil || fp != r.srv.fp {
		t.Fatalf("probe by name: %q, %v", fp, err)
	}
	// the certificate is for the name, which is what the node's host is
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.sb.read("/etc/nasnet-agent/server.crt"), "BEGIN CERTIFICATE") {
		t.Error("no certificate")
	}
}

func TestTheSourceAddressIsSeenByTheServer(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("this system has no 127.0.0.2 on its loopback: %v", err)
	}
	probe.Close()
	r := newSSHRig(t, "root", "hunter2")
	r.env.Local = netip.MustParseAddr("127.0.0.2")
	if _, err := r.env.Probe(context.Background(), "127.0.0.1", r.srv.port()); err != nil {
		t.Fatal(err)
	}
}

// The real thing, signed in as root: all of the install, through an SSH
// session for each command.
func TestAnInstallOverRealSSHAsRoot(t *testing.T) {
	r := newSSHRig(t, "root", "hunter2")
	res, err := r.install()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Arch != "amd64" || res.AgentVersion != "v1.3.0" {
		t.Errorf("result %+v", res)
	}
	for _, p := range []string{"/usr/local/bin/nasnet-agent", "/etc/nasnet-agent/server.key", "/etc/nasnet-agent/config.yaml", "/etc/systemd/system/nasnet-agent.service"} {
		if !r.sb.exists(p) {
			t.Errorf("%s is missing", p)
		}
	}
	if r.srv.attempts.Load() == 0 {
		t.Error("nothing logged in")
	}
	// the login only ever crosses in the handshake
	for _, c := range r.sb.commands() {
		if strings.Contains(c.Cmd, "hunter2") || strings.Contains(string(c.Stdin), "hunter2") {
			t.Errorf("the password reached a command: %.100s", c.Cmd)
		}
	}
}

// Through the real Conn.Root: sudo with the password as the first line of stdin.
func TestAnInstallOverRealSSHAsAUserWithSudo(t *testing.T) {
	r := newSSHRig(t, "deploy", "s3cret-sudo")
	r.sb.env["FAKE_UID"], r.sb.env["FAKE_SUDO_PW"] = "1000", "s3cret-sudo"
	res, err := r.install()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || !r.sb.exists("/usr/local/bin/nasnet-agent") {
		t.Fatalf("result %+v", res)
	}
	var sudoed int
	for _, c := range r.sb.commands() {
		if strings.Contains(c.Cmd, "s3cret-sudo") {
			t.Errorf("the password is on a command line: %.100s", c.Cmd)
		}
		if strings.HasPrefix(c.Cmd, "sudo -k -S -p ''") {
			sudoed++
			if !strings.HasPrefix(string(c.Stdin), "s3cret-sudo\n") {
				t.Errorf("sudo wasn't given the password on stdin: %q", c.Stdin)
			}
		} else if strings.Contains(string(c.Stdin), "s3cret-sudo") {
			t.Errorf("the password went to %.100s", c.Cmd)
		}
	}
	if sudoed == 0 {
		t.Error("nothing ran through sudo")
	}
}

func TestAWrongPasswordOverRealSSH(t *testing.T) {
	r := newSSHRig(t, "root", "right-password")
	r.login = Login{Password: "wrong-password"}
	res, err := r.install()
	if !errors.Is(err, ErrLoginFailed) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "wrong-password") || strings.Contains(err.Error(), "right-password") {
		t.Errorf("the error carries a password: %q", err)
	}
	if res.Changed {
		t.Error("changed")
	}
	if !strings.Contains(err.Error(), "password or key") {
		t.Errorf("the message doesn't say what to check: %q", err)
	}
}

func TestAChangedHostKeyOverRealSSHSendsNoLogin(t *testing.T) {
	r := newSSHRig(t, "root", "hunter2")
	r.node.SSHHostKey = "SHA256:aDifferentKeyThanTheOneTheServerHasAtAll"
	_, err := r.install()
	var hk *HostKeyChangedError
	if !errors.Is(err, ErrHostKeyChanged) || !errors.As(err, &hk) {
		t.Fatalf("err = %v", err)
	}
	if hk.Got != r.srv.fp || hk.Pinned != r.node.SSHHostKey {
		t.Errorf("fingerprints: %+v, the server's is %s", hk, r.srv.fp)
	}
	if n := r.srv.attempts.Load(); n != 0 {
		t.Errorf("the password was offered %d times to a server whose key isn't the pinned one", n)
	}
}

func TestAPrivateKeyThatCannotBeReadIsSaidSo(t *testing.T) {
	r := newSSHRig(t, "root", "hunter2")
	r.login = Login{PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nnot a key\n-----END OPENSSH PRIVATE KEY-----\n"}
	_, err := r.install()
	if !errors.Is(err, ErrLoginFailed) || !strings.Contains(err.Error(), "private key") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "not a key") {
		t.Errorf("the key's text is in the error: %q", err)
	}
}

// The cause of a failure that isn't the login or the network is a command
// that failed on the server, and says what it was doing.
func TestACommandThatFailsOnTheServerSaysWhatItWasDoing(t *testing.T) {
	r := newSSHRig(t, "root", "hunter2")
	os.Chmod(r.sb.path("/etc"), 0o555) // nothing can be created in /etc
	defer os.Chmod(r.sb.path("/etc"), 0o755)
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	res, err := r.install()
	if err == nil {
		t.Fatal("an install into a folder nothing can be created in")
	}
	if errors.Is(err, ErrUnreachable) || errors.Is(err, ErrLoginFailed) {
		t.Errorf("a failed command was taken for a network or a login problem: %v", err)
	}
	if !strings.Contains(err.Error(), "/etc/nasnet-agent") {
		t.Errorf("the message doesn't say what it was writing: %q", err)
	}
	if !res.Changed {
		t.Error("the marker is in /var/lib, which was written")
	}
}
