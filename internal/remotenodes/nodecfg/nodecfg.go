// SPDX-License-Identifier: AGPL-3.0-or-later

// Package nodecfg builds the Xray config of a remote node. The node is the
// portal of a reverse tunnel: one REALITY inbound serves the subscriptions and
// the router's bridge, private ranges are blocked, domestic ones leave from
// the node itself and the rest goes into the tunnel. The hub pushes the result
// with PushConfig, so this package sticks to the standard library: no Xray and
// no geoip data.
package nodecfg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

// What the hub has to name when it talks to a node's agent.
const (
	// EntryTag is the inbound AddUser and RemoveUser address.
	EntryTag = "entry"
	// Flow is the flow of every client on the entry, the bridge's included.
	Flow = "xtls-rprx-vision"
	// DefaultAPIPort is where the agent looks for the Xray API.
	DefaultAPIPort = 10085
)

// Input is what differs between nodes; the rest of the config is fixed.
type Input struct {
	EntryPort   int      // public port of the entry inbound
	RealityDest string   // host:port that gets any handshake REALITY doesn't recognise
	ServerNames []string // names a client may send in its hello
	PrivateKey  string   // x25519, base64url without padding
	ShortIDs    []string // hex, even length, up to 16 characters
	Clients     []Client // one per enabled subscription
	Bridge      Bridge
	APIPort     int    // loopback port of the Xray API, 0 for DefaultAPIPort
	LogLevel    string // "" for warning
	// DomesticCIDRs replaces the embedded domestic list when it isn't empty.
	DomesticCIDRs []string
}

// Client is a subscription's login on the entry.
type Client struct {
	UUID  string
	Email string // see SubscriptionEmail
}

// Bridge is the client the router's bridge logs in as. Xray lets only a client
// with a reverse tag register the tunnel, and lets that client do nothing else.
type Bridge struct {
	UUID       string
	Email      string // see BridgeEmail
	ReverseTag string // the portal's outbound; unique on the node
}

// Build returns the node's config.json as the agent's PushConfig takes it. The
// same input gives the same bytes.
func Build(in Input) ([]byte, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	domestic, err := in.domestic()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(assemble(in, domestic)); err != nil {
		return nil, fmt.Errorf("nodecfg: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func (in Input) apiPort() int {
	if in.APIPort == 0 {
		return DefaultAPIPort
	}
	return in.APIPort
}

func (in Input) logLevel() string {
	if in.LogLevel == "" {
		return "warning"
	}
	return strings.ToLower(in.LogLevel)
}

// domestic is the list that leaves from the node: the override, normalised, or
// the embedded one.
func (in Input) domestic() ([]string, error) {
	if len(in.DomesticCIDRs) == 0 {
		return DomesticCIDRs(), nil
	}
	out := make([]string, len(in.DomesticCIDRs))
	for i, s := range in.DomesticCIDRs {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, invalid("domestic range %q is not a CIDR", s)
		}
		if p.Addr().Is4In6() {
			return nil, invalid("domestic range %q is an IPv4-mapped IPv6 prefix; write it as IPv4", s)
		}
		out[i] = p.Masked().String()
	}
	return out, nil
}
