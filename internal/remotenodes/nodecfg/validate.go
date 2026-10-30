// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// ErrInvalid is wrapped by every complaint about an Input.
var ErrInvalid = errors.New("invalid node config input")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// validate reports the first problem, in the order of Input's fields. A bad
// key or a doubled email would otherwise only show up when the node restarts.
func (in Input) validate() error {
	if !validPort(in.EntryPort) {
		return invalid("entry port %d is out of range (1-65535)", in.EntryPort)
	}
	api := in.apiPort()
	if !validPort(api) {
		return invalid("API port %d is out of range (1-65535)", api)
	}
	if api == in.EntryPort {
		return invalid("the entry and the API can't both use port %d", api)
	}

	if err := validDest(in.RealityDest); err != nil {
		return err
	}
	if len(in.ServerNames) == 0 {
		return invalid("at least one REALITY server name is required")
	}
	for _, n := range in.ServerNames {
		if !validName(n) {
			return invalid("REALITY server name %q is not a plain host name", n)
		}
	}
	if k, err := base64.RawURLEncoding.DecodeString(in.PrivateKey); err != nil || len(k) != 32 {
		return invalid("the REALITY private key must be 32 bytes in base64url without padding")
	}
	if len(in.ShortIDs) == 0 {
		return invalid("at least one REALITY short id is required")
	}
	for _, s := range in.ShortIDs {
		if !validShortID(s) {
			return invalid("REALITY short id %q must be 2 to 16 hex digits, an even number of them", s)
		}
	}

	uuids, emails := map[string]bool{}, map[string]bool{}
	check := func(who, id, email string) error {
		if !validUUID(id) {
			return invalid("%s: %q is not a UUID", who, id)
		}
		if !validEmail(email) {
			return invalid("%s: email %q must be printable ASCII with no spaces or %q", who, email, ">>>")
		}
		// Xray compares both without regard to case.
		lid, lemail := strings.ToLower(id), strings.ToLower(email)
		if uuids[lid] {
			return invalid("%s: UUID %s is used twice", who, id)
		}
		if emails[lemail] {
			return invalid("%s: email %s is used twice", who, email)
		}
		uuids[lid], emails[lemail] = true, true
		return nil
	}
	for i, c := range in.Clients {
		if err := check(fmt.Sprintf("client %d", i+1), c.UUID, c.Email); err != nil {
			return err
		}
	}
	if err := check("bridge", in.Bridge.UUID, in.Bridge.Email); err != nil {
		return err
	}
	if !validTag(in.Bridge.ReverseTag) {
		return invalid("bridge reverse tag %q must be 1 to 64 letters, digits, '.', '_' or '-', starting with a letter or digit, and none of %s",
			in.Bridge.ReverseTag, strings.Join(reservedTags, ", "))
	}

	switch in.logLevel() {
	case "debug", "info", "warning", "error", "none":
	default:
		return invalid("log level %q is not debug, info, warning, error or none", in.LogLevel)
	}
	return nil
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }

func validDest(dest string) error {
	host, port, err := net.SplitHostPort(dest)
	if err != nil || host == "" {
		return invalid("REALITY dest %q must look like host:port", dest)
	}
	if p, err := strconv.Atoi(port); err != nil || !validPort(p) {
		return invalid("REALITY dest %q has no usable port", dest)
	}
	if _, err := netip.ParseAddr(host); err != nil && !validName(host) {
		return invalid("REALITY dest %q has no usable host", dest)
	}
	return nil
}

// validName accepts a dotted host name: letters, digits, '-' and '_' in labels
// of up to 63 characters. Wildcards aren't one, REALITY compares names whole.
func validName(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			default:
				return false
			}
		}
	}
	return true
}

func validShortID(s string) bool {
	if len(s) == 0 || len(s) > 16 || len(s)%2 != 0 {
		return false
	}
	return isHex(s)
}

func isHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// validUUID wants the 8-4-4-4-12 form. Xray would also take any short string
// and hash it into an id, which the hub never means to do.
func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			if !isHex(s[i : i+1]) {
				return false
			}
		}
	}
	return true
}

// validEmail keeps stats names parseable: they join the email to the counter
// with ">>>".
func validEmail(s string) bool {
	if s == "" || len(s) > 255 || strings.Contains(s, ">>>") {
		return false
	}
	for _, r := range s {
		if r < '!' || r > '~' {
			return false
		}
	}
	return true
}

// reservedTags are the other tags in the config; a reverse tag that took one
// would shadow it.
var reservedTags = []string{apiTag, EntryTag, directTag, blockedTag}

func validTag(s string) bool {
	if s == "" || len(s) > 64 || slices.Contains(reservedTags, s) {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
