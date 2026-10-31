// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"crypto/rand"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// The checks on what a person types, for the operations. Each gives the value
// in the form the rest of the plugin keeps it in, or an *model.InvalidError,
// which the API answers as a bad request. A message names the field and never
// quotes a secret.

const (
	defaultSSHPort = 22
	defaultSSHUser = "root"

	maxPassword   = 1 << 10
	maxPassphrase = 1 << 10
	maxPrivateKey = 16 << 10
)

func invalidf(format string, args ...any) error {
	return &model.InvalidError{Msg: fmt.Sprintf(format, args...)}
}

// sshPortOf is the port to sign in on: the one given, or 22 when none is.
func sshPortOf(p int) (int, error) {
	if p == 0 {
		return defaultSSHPort, nil
	}
	if p < 1 || p > 65535 {
		return 0, invalidf("the SSH port must be between 1 and 65535")
	}
	return p, nil
}

// cleanUser is the login name, root when none is given. Printable characters
// without a space is all a name has to be, since it is sent to the SSH library
// and never to a shell.
func cleanUser(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return defaultSSHUser, nil
	}
	if len(s) > 64 {
		return "", invalidf("the SSH user name can have at most 64 characters")
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] >= 0x7f {
			return "", invalidf("the SSH user name must be printable characters without a space")
		}
	}
	return s, nil
}

// hostKeyRe is the fingerprint the SSH library prints for a host key: SHA256
// and the 32 bytes of the hash in unpadded base64.
var hostKeyRe = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

// login is a login as the operations use it: where, as whom, the host key the
// person agreed to, and the secrets.
type login struct {
	port    int
	user    string
	hostKey string
	creds   agentops.Login
}

// cleanLogin checks a login that came with a request. It needs a password or a
// private key, and the host key the person was shown: a login is never sent to
// a server nobody has vouched for.
func cleanLogin(l api.Login) (login, error) {
	port, err := sshPortOf(l.SSHPort)
	if err != nil {
		return login{}, err
	}
	user, err := cleanUser(l.SSHUser)
	if err != nil {
		return login{}, err
	}
	key := strings.TrimSpace(l.HostKey)
	switch {
	case !hostKeyRe.MatchString(key):
		return login{}, invalidf("confirm the server's host key first")
	case l.Password == "" && strings.TrimSpace(l.PrivateKey) == "":
		return login{}, invalidf("give a password or a private key")
	case len(l.Password) > maxPassword:
		return login{}, invalidf("the password is longer than %d characters", maxPassword)
	case len(l.Passphrase) > maxPassphrase:
		return login{}, invalidf("the passphrase is longer than %d characters", maxPassphrase)
	case len(l.PrivateKey) > maxPrivateKey:
		return login{}, invalidf("the private key is longer than %d characters", maxPrivateKey)
	}
	return login{
		port: port, user: user, hostKey: key,
		creds: agentops.Login{Password: l.Password, PrivateKey: l.PrivateKey, Passphrase: l.Passphrase},
	}, nil
}

// secrets are what a job must never show: each part of the login that was
// given, and whatever else the caller names.
func (l login) secrets(more ...string) []string {
	var out []string
	for _, s := range append([]string{l.creds.Password, l.creds.Passphrase, l.creds.PrivateKey}, more...) {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// cleanTarget reads a handshake target, host or host:port with 443 for the
// port, and gives it as host:port with the host in its canonical form.
func cleanTarget(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", invalidf("the handshake target is required")
	}
	if strings.Contains(s, "://") || strings.ContainsAny(s, "/ \t") {
		return "", invalidf("the handshake target must look like www.example.com:443")
	}
	host, portText, err := net.SplitHostPort(s)
	if err != nil {
		host, portText = strings.Trim(s, "[]"), "443"
	}
	if host, err = model.CleanHost(host); err != nil {
		return "", err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", invalidf("the handshake target has no valid port")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// hostOf is the host of a host:port.
func hostOf(target string) string {
	h, _, err := net.SplitHostPort(target)
	if err != nil {
		return ""
	}
	return h
}

// cleanSNI reads the name a client puts in its hello: a host name, never an
// address.
func cleanSNI(s string) (string, error) {
	h, err := model.CleanHost(s)
	if err != nil {
		return "", err
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return "", invalidf("the server name must be a host name, not an IP address")
	}
	return h, nil
}

// newUUID is a random version 4 id in the lower-case form a link and the
// state both use.
func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
