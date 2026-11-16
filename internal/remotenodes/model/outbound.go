// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

const (
	// MaxOutbounds is as many as the bridge takes.
	MaxOutbounds = bridgespec.MaxOutbounds
	// MaxOutboundNameRunes is the longest name of an outbound. The page puts
	// it in a menu beside every node, so it is shorter than a node's.
	MaxOutboundNameRunes = 40
)

// Outbound is a server that a node's traffic, or the router's own entry's,
// can leave through instead of the router's VPN. The bridge reaches it from
// the router's Foreign link.
type Outbound struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	// Fallback sends what would leave through it out through the router's
	// VPN for as long as it doesn't answer.
	Fallback  bool             `json:"fallback,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	Settings  OutboundSettings `json:"settings"`
}

// OutboundSettings is how to reach an outbound's server, what a share link or
// an Xray outbound says of it. The id and the password are its login, and are
// hub only like a node's private key.
type OutboundSettings struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"` // an IP address or host name, in the form CleanHost returns
	Port     int    `json:"port"`
	ID       string `json:"id,omitempty"`       // VLESS and VMess. Hub only
	Password string `json:"password,omitempty"` // Trojan, Shadowsocks, and a SOCKS or HTTP login. Hub only
	Username string `json:"username,omitempty"`
	Method   string `json:"method,omitempty"`
	Flow     string `json:"flow,omitempty"`

	// Network is the transport, and Path, HostHeader and Mode its settings.
	Network    string `json:"network,omitempty"`
	Path       string `json:"path,omitempty"`
	HostHeader string `json:"host_header,omitempty"`
	Mode       string `json:"mode,omitempty"`

	Security    string `json:"security,omitempty"`
	ServerName  string `json:"server_name,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	// ALPN is the one list in the state's records, so State.Clone copies it
	// by hand.
	ALPN      []string `json:"alpn,omitempty"`
	PublicKey string   `json:"public_key,omitempty"`
	ShortID   string   `json:"short_id,omitempty"`
	SpiderX   string   `json:"spider_x,omitempty"`
}

// BridgeOutbound is the settings as the bridge's spec has them, without the
// tag and the address: only a spec gives an outbound those.
func (s OutboundSettings) BridgeOutbound() bridgespec.Outbound {
	port := uint16(0)
	if s.Port > 0 && s.Port <= 65535 {
		port = uint16(s.Port)
	}
	return bridgespec.Outbound{
		Protocol: s.Protocol, Port: port,
		ID: s.ID, Flow: s.Flow, Username: s.Username, Password: s.Password, Method: s.Method,
		Network: s.Network, Path: s.Path, Host: s.HostHeader, Mode: s.Mode,
		Security: s.Security, ServerName: s.ServerName, Fingerprint: s.Fingerprint, ALPN: slices.Clone(s.ALPN),
		PublicKey: s.PublicKey, ShortID: s.ShortID, SpiderX: s.SpiderX,
	}
}

// HostIsName says whether the server is given by a name, which the hub looks
// up before the bridge can have it.
func (s OutboundSettings) HostIsName() bool {
	host, err := CleanHost(s.Host)
	if err != nil {
		return false
	}
	_, err = netip.ParseAddr(host)
	return err != nil
}

// fieldNames are bridgespec's names for an outbound's fields, as the page
// sends them. The transport's host is the page's hostHeader, since its host is
// the server's.
var fieldNames = map[string]string{
	"host": "hostHeader", "server_name": "serverName", "public_key": "publicKey",
	"short_id": "shortId", "spider_x": "spiderX",
}

// pageField puts the page's name of the field in place of bridgespec's at the
// start of one of its messages.
func pageField(msg string) string {
	field, rest, ok := strings.Cut(msg, ": ")
	if !ok {
		return msg
	}
	if name, renamed := fieldNames[field]; renamed {
		field = name
	}
	return field + ": " + rest
}

// Validate checks the settings the way the bridge will use them. The message
// names the field as the page sends it, under settings.
func (s OutboundSettings) Validate() error {
	host, err := CleanHost(s.Host)
	switch {
	case err != nil:
		return invalid("settings.host: %v", err)
	case host != s.Host:
		return invalid("settings.host: it must be written %q", host)
	}
	if err := ValidPort(s.Port); err != nil {
		return invalid("settings.port: %v", err)
	}
	if err := bridgespec.CheckOutbound(s.BridgeOutbound()); err != nil {
		return invalid("settings.%s", pageField(err.Error()))
	}
	return nil
}

// CleanOutboundSettings trims the host, fills in the transport and the
// security when they were left out, then checks the result.
func CleanOutboundSettings(s OutboundSettings) (OutboundSettings, error) {
	s.Host = strings.TrimSpace(s.Host)
	if host, err := CleanHost(s.Host); err == nil {
		s.Host = host
	}
	if s.Network == "" {
		s.Network = bridgespec.NetTCP
	}
	if s.Security == "" {
		s.Security = bridgespec.SecNone
	}
	if len(s.ALPN) == 0 {
		s.ALPN = nil
	} else {
		s.ALPN = slices.Clone(s.ALPN)
	}
	return s, s.Validate()
}

func validOutboundName(s string) error {
	if err := ValidName(s); err != nil {
		return err
	}
	if utf8.RuneCountInString(s) > MaxOutboundNameRunes {
		return invalid("the name can have at most %d characters", MaxOutboundNameRunes)
	}
	return nil
}

// CleanOutboundName trims what a person might paste around a name, then
// checks it.
func CleanOutboundName(s string) (string, error) {
	s = strings.TrimSpace(s)
	return s, validOutboundName(s)
}

func (o Outbound) Validate() error {
	if o.ID == 0 {
		return invalid("an outbound needs an id")
	}
	if err := validOutboundName(o.Name); err != nil {
		return err
	}
	if o.CreatedAt.IsZero() {
		return invalid("an outbound needs a creation time")
	}
	return o.Settings.Validate()
}

// FindOutbound points into s.Outbounds, so it is good only until the slice
// changes.
func (s *State) FindOutbound(id uint) *Outbound {
	if i := slices.IndexFunc(s.Outbounds, func(o Outbound) bool { return o.ID == id }); i >= 0 {
		return &s.Outbounds[i]
	}
	return nil
}

// LeavingThrough is who names an outbound as the way out: the nodes, by id in
// the state's order, and whether the router's own entry does. It says nothing
// of whether any of them carries traffic now.
func (s State) LeavingThrough(id uint) (nodes []uint, router bool) {
	if id == 0 {
		return nil, false
	}
	for _, n := range s.Nodes {
		if n.OutboundID == id {
			nodes = append(nodes, n.ID)
		}
	}
	return nodes, s.Local.OutboundID == id
}

// CarriedThrough says whether anything that carries traffic leaves through an
// outbound: a node that is set up, or the router's own entry while it is on.
func (s State) CarriedThrough(id uint) bool {
	if id == 0 {
		return false
	}
	if s.Local.Enabled && s.Local.OutboundID == id {
		return true
	}
	return slices.ContainsFunc(s.Nodes, func(n Node) bool { return n.State == NodeRegistered && n.OutboundID == id })
}

// outboundNameTaken compares as nameTaken does, among the outbounds.
func (s State) outboundNameTaken(name string, except uint) bool {
	return slices.ContainsFunc(s.Outbounds, func(o Outbound) bool {
		return o.ID != except && strings.EqualFold(o.Name, name)
	})
}

// RenameOutbound gives an outbound another name, or fails with ErrNotFound or
// ErrNameTaken.
func (s *State) RenameOutbound(id uint, name string) error {
	o := s.FindOutbound(id)
	if o == nil {
		return fmt.Errorf("%w: outbound %d", ErrNotFound, id)
	}
	if err := validOutboundName(name); err != nil {
		return err
	}
	if s.outboundNameTaken(name, id) {
		return fmt.Errorf("%w: %q", ErrNameTaken, name)
	}
	o.Name = name
	return nil
}

// AddOutbound gives o the next id and appends it, or fails with
// ErrOutboundLimit or ErrNameTaken.
func (s *State) AddOutbound(o Outbound) (Outbound, error) {
	if len(s.Outbounds) >= MaxOutbounds {
		return Outbound{}, fmt.Errorf("%w: at most %d", ErrOutboundLimit, MaxOutbounds)
	}
	o.ID = s.NextOutboundID
	o.CreatedAt = o.CreatedAt.UTC().Truncate(time.Second)
	o.Settings.ALPN = slices.Clone(o.Settings.ALPN)
	if err := o.Validate(); err != nil {
		return Outbound{}, err
	}
	if s.outboundNameTaken(o.Name, 0) {
		return Outbound{}, fmt.Errorf("%w: %q", ErrNameTaken, o.Name)
	}
	s.NextOutboundID++
	s.Outbounds = append(s.Outbounds, o)
	return o, nil
}

// RemoveOutbound drops an outbound nothing leaves through. One that a node or
// the router's own entry names is ErrOutboundInUse: their way out would be
// gone, and which way to send them instead is a person's choice.
func (s *State) RemoveOutbound(id uint) error {
	i := slices.IndexFunc(s.Outbounds, func(o Outbound) bool { return o.ID == id })
	if i < 0 {
		return fmt.Errorf("%w: outbound %d", ErrNotFound, id)
	}
	if nodes, router := s.LeavingThrough(id); len(nodes) > 0 || router {
		return fmt.Errorf("%w: outbound %d", ErrOutboundInUse, id)
	}
	s.Outbounds = slices.Delete(s.Outbounds, i, i+1)
	return nil
}

// validateOutbounds checks the outbounds and the routes that name them.
func (s State) validateOutbounds() error {
	if s.NextOutboundID == 0 {
		return invalid("an id counter is zero")
	}
	ids := map[uint]bool{}
	for _, o := range s.Outbounds {
		if err := o.Validate(); err != nil {
			return fmt.Errorf("outbound %d: %w", o.ID, err)
		}
		if ids[o.ID] {
			return invalid("outbound id %d is used twice", o.ID)
		}
		if o.ID >= s.NextOutboundID {
			return invalid("outbound id %d is not below the id counter, so it could be handed out again", o.ID)
		}
		if s.outboundNameTaken(o.Name, o.ID) {
			return fmt.Errorf("%w: %q", ErrNameTaken, o.Name)
		}
		ids[o.ID] = true
	}
	for _, n := range s.Nodes {
		if n.OutboundID != 0 && !ids[n.OutboundID] {
			return invalid("node %d leaves through outbound %d, which doesn't exist", n.ID, n.OutboundID)
		}
	}
	if s.Local.OutboundID != 0 && !ids[s.Local.OutboundID] {
		return invalid("this router leaves through outbound %d, which doesn't exist", s.Local.OutboundID)
	}
	if err := bridgespec.CheckProbeURL(s.ProbeURL); err != nil {
		return invalid("the outbounds' check: %v", err)
	}
	return nil
}
