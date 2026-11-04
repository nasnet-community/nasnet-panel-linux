// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

// MaxNameRunes is the longest node or subscription name.
const MaxNameRunes = 64

// InvalidError is a value someone typed that can't be used. The API answers
// bad_request with its message.
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &InvalidError{Msg: fmt.Sprintf(format, a...)} }

// ValidName takes any UTF-8 text of 1 to MaxNameRunes characters without
// control characters. Persian names work as typed, zero-width non-joiners
// included.
func ValidName(s string) error {
	switch {
	case strings.TrimSpace(s) == "":
		return invalid("the name is required")
	case !utf8.ValidString(s):
		return invalid("the name is not valid UTF-8")
	case utf8.RuneCountInString(s) > MaxNameRunes:
		return invalid("the name can have at most %d characters", MaxNameRunes)
	case strings.TrimSpace(s) != s:
		return invalid("the name must not start or end with a space")
	}
	for _, r := range s {
		// U+2028 and U+2029 break lines but are not in the control category
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return invalid("the name must not contain control characters or line breaks")
		}
	}
	return nil
}

// CleanName trims what a person might paste around a name, then checks it.
func CleanName(s string) (string, error) {
	s = strings.TrimSpace(s)
	return s, ValidName(s)
}

// CleanHost checks that s is an IP address or a host name and returns it in
// one canonical form: addresses the way netip prints them, names in lower
// case. Names are letters, digits and hyphens in dot-separated labels, with no
// trailing dot, so an IDN has to come as punycode.
func CleanHost(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", invalid("the host is required")
	}
	if a, err := netip.ParseAddr(s); err == nil {
		if a.Zone() != "" || a.IsUnspecified() {
			return "", invalid("%q is not an address a server can have", s)
		}
		return a.String(), nil
	}
	labels := strings.Split(s, ".")
	ok := len(s) <= 253
	for _, l := range labels {
		ok = ok && validLabel(l)
	}
	// a last label of digits is not a name: it is a mistyped address like 1.2.3 or 256.1.1.1
	if !ok || allDigits(labels[len(labels)-1]) {
		return "", invalid("%q is not an IP address or a host name", s)
	}
	return strings.ToLower(s), nil
}

func validLabel(l string) bool {
	if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
		return false
	}
	for i := 0; i < len(l); i++ {
		c := l[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

func ValidPort(p int) error {
	if p < 1 || p > 65535 {
		return invalid("a port must be between 1 and 65535")
	}
	return nil
}

func validTarget(s string) error {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return invalid("%q must look like host:port", s)
	}
	if _, err := CleanHost(host); err != nil {
		return err
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return invalid("%q has no valid port", s)
	}
	return ValidPort(p)
}

func validSNI(s string) error {
	h, err := CleanHost(s)
	if err != nil {
		return err
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return invalid("the SNI must be a host name, not an IP address")
	}
	return nil
}

// plain keeps what a node reports, or the hub generates, short and printable.
// The message names the field and never its value, which may be a key.
func plain(what, s string, limit int) error {
	if len(s) > limit {
		return invalid("%s is longer than %d characters", what, limit)
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return invalid("%s has a character it can't hold", what)
		}
	}
	return nil
}

// validUUID wants the canonical lower-case form, which is also what goes into
// a link.
func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
				return false
			}
		}
	}
	return true
}

func (n Node) Validate() error {
	if n.ID == 0 {
		return invalid("a node needs an id")
	}
	if err := ValidName(n.Name); err != nil {
		return err
	}
	host, err := CleanHost(n.Host)
	if err != nil {
		return err
	}
	if host != n.Host {
		return invalid("the host must be written %q", host)
	}
	for _, p := range []struct {
		what string
		port int
	}{{"ssh port", n.SSHPort}, {"agent port", n.AgentPort}, {"entry port", n.EntryPort}} {
		if err := ValidPort(p.port); err != nil {
			return fmt.Errorf("%s: %w", p.what, err)
		}
	}
	if n.SSHPort == n.AgentPort || n.SSHPort == n.EntryPort || n.AgentPort == n.EntryPort {
		return invalid("the ssh, agent and entry ports must all differ")
	}
	if n.State != NodeInstalling && n.State != NodeRegistered {
		return invalid("unknown node state %q", n.State)
	}
	if n.CreatedAt.IsZero() {
		return invalid("a node needs a creation time")
	}
	for _, f := range []struct {
		what, val string
		limit     int
	}{
		{"the ssh user", n.SSHUser, 64},
		{"the arch", n.Arch, 16},
		{"the agent version", n.AgentVersion, 64},
		{"the xray version", n.XrayVersion, 64},
		{"the ssh host key", n.SSHHostKey, 128},
		{"the REALITY public key", n.RealityPublicKey, 128},
		{"the REALITY private key", n.RealityPrivateKey, 128},
		{"the REALITY short id", n.RealityShortID, 32},
	} {
		if err := plain(f.what, f.val, f.limit); err != nil {
			return err
		}
	}
	if n.RealityTarget != "" {
		if err := validTarget(n.RealityTarget); err != nil {
			return fmt.Errorf("Reality target: %w", err)
		}
	}
	if n.RealitySNI != "" {
		if err := validSNI(n.RealitySNI); err != nil {
			return fmt.Errorf("sni: %w", err)
		}
	}
	if !n.CertNotBefore.IsZero() && !n.CertNotAfter.IsZero() && !n.CertNotAfter.After(n.CertNotBefore) {
		return invalid("the certificate ends before it starts")
	}
	// none is fine: a node the install hasn't finished with has no bridge client yet
	if n.BridgeUUID != "" && !validUUID(n.BridgeUUID) {
		return invalid("the bridge client id is not a lower-case UUID")
	}
	if n.Failure != (InstallFailure{}) {
		if n.State != NodeInstalling {
			return invalid("only an install that didn't finish has a failure on record")
		}
		if err := n.Failure.Validate(); err != nil {
			return fmt.Errorf("install failure: %w", err)
		}
	}
	return nil
}

// Validate keeps what a failure says to text the page can show: valid UTF-8,
// and no longer than a job's own texts.
func (f InstallFailure) Validate() error {
	for _, t := range []struct {
		what, val string
		limit     int
	}{{"the step", f.Step, 64}, {"the step key", f.StepKey, 64}, {"the error", f.Error, 2000}, {"the cleanup error", f.Cleanup, 2000}} {
		if len(t.val) > t.limit {
			return invalid("%s is longer than %d bytes", t.what, t.limit)
		}
		if !utf8.ValidString(t.val) {
			return invalid("%s is not UTF-8", t.what)
		}
	}
	if strings.TrimSpace(f.Error) == "" {
		return invalid("a failure needs an error")
	}
	return nil
}

func (s Subscription) Validate() error {
	if s.ID == 0 {
		return invalid("a subscription needs an id")
	}
	if err := ValidName(s.Name); err != nil {
		return err
	}
	if s.CreatedAt.IsZero() {
		return invalid("a subscription needs a creation time")
	}
	if s.TrafficLimit > MaxTrafficLimit {
		return invalid("the traffic limit is too large")
	}
	switch s.Stopped {
	case "", StopLimit, StopExpired:
	default:
		return invalid("a subscription is stopped for a reason this build doesn't know")
	}
	return nil
}

func (e Enablement) Validate() error {
	if e.SubscriptionID == 0 || e.NodeID == 0 {
		return invalid("an enablement needs a subscription and a node")
	}
	if !validUUID(e.ClientUUID) {
		return invalid("the client id is not a lower-case UUID")
	}
	return nil
}

// validCA takes both halves of the CA or neither, and a pair that pki can
// load. The message says what is wrong and never quotes the key.
func validCA(cert, key string) error {
	switch {
	case cert == "" && key == "":
		return nil
	case cert == "" || key == "":
		return invalid("the CA needs both its certificate and its key")
	}
	if _, err := pki.Load([]byte(cert), []byte(key)); err != nil {
		return invalid("%v", err)
	}
	return nil
}

func (s Settings) Validate() error {
	for _, p := range []struct {
		what string
		port int
	}{{"agent port", s.AgentPort}, {"entry port", s.EntryPort}} {
		if err := ValidPort(p.port); err != nil {
			return fmt.Errorf("%s: %w", p.what, err)
		}
	}
	if s.AgentPort == s.EntryPort {
		return invalid("the agent and entry ports must differ")
	}
	return nil
}

// Validate checks every record and how they refer to each other. It leaves
// out the caps: those only refuse growth, which needs the state before (see
// CheckEdit).
func (s State) Validate() error {
	if s.Version != SchemaVersion {
		return invalid("the state version is %d, expected %d", s.Version, SchemaVersion)
	}
	if err := s.Settings.Validate(); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	if s.NextNodeID == 0 || s.NextSubscriptionID == 0 {
		return invalid("an id counter is zero")
	}
	if err := validCA(s.CACert, s.CAKey); err != nil {
		return err
	}
	nodes := map[uint]bool{}
	for _, n := range s.Nodes {
		if err := n.Validate(); err != nil {
			return fmt.Errorf("node %d: %w", n.ID, err)
		}
		if nodes[n.ID] {
			return invalid("node id %d is used twice", n.ID)
		}
		if n.ID >= s.NextNodeID {
			return invalid("node id %d is not below the id counter, so it could be handed out again", n.ID)
		}
		nodes[n.ID] = true
	}
	subs := map[uint]bool{}
	for _, x := range s.Subscriptions {
		if err := x.Validate(); err != nil {
			return fmt.Errorf("subscription %d: %w", x.ID, err)
		}
		if subs[x.ID] {
			return invalid("subscription id %d is used twice", x.ID)
		}
		if x.ID >= s.NextSubscriptionID {
			return invalid("subscription id %d is not below the id counter, so it could be handed out again", x.ID)
		}
		if s.nameTaken(x.Name, x.ID) {
			return fmt.Errorf("%w: %q", ErrNameTaken, x.Name)
		}
		subs[x.ID] = true
	}
	// A client id is one login on one node's entry, so no two logins may share
	// one: not two bridges, and not a bridge and a subscription.
	pairs, uuids := map[[2]uint]bool{}, map[string]bool{}
	for _, n := range s.Nodes {
		if n.BridgeUUID == "" {
			continue
		}
		if uuids[n.BridgeUUID] {
			return invalid("node %d shares its bridge client id with another node", n.ID)
		}
		uuids[n.BridgeUUID] = true
	}
	for _, e := range s.Enablements {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("subscription %d on node %d: %w", e.SubscriptionID, e.NodeID, err)
		}
		pair := [2]uint{e.SubscriptionID, e.NodeID}
		switch {
		case !subs[e.SubscriptionID] || !nodes[e.NodeID]:
			return invalid("subscription %d on node %d points at one that doesn't exist", e.SubscriptionID, e.NodeID)
		case pairs[pair]:
			return invalid("subscription %d is on node %d twice", e.SubscriptionID, e.NodeID)
		case uuids[e.ClientUUID]:
			return invalid("subscription %d on node %d shares its client id with another login", e.SubscriptionID, e.NodeID)
		}
		pairs[pair], uuids[e.ClientUUID] = true, true
	}
	return nil
}
