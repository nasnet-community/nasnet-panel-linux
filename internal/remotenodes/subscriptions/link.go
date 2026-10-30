// SPDX-License-Identifier: AGPL-3.0-or-later

package subscriptions

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Defaults for a REALITY entry on a node.
const (
	DefaultFlow        = "xtls-rprx-vision"
	DefaultFingerprint = "chrome"
)

// LinkParams is what a vless:// link for one subscription on one node needs.
type LinkParams struct {
	UUID        string // the subscription's client id on this node
	Host        string // the node's public address
	Port        int
	ServerName  string // the REALITY server name (SNI)
	PublicKey   string
	ShortID     string
	Fingerprint string // empty means DefaultFingerprint
	Flow        string // empty means DefaultFlow
	Name        string // what the app shows; any UTF-8, Persian included
}

var (
	uuidRe    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	shortIDRe = regexp.MustCompile(`^([0-9a-fA-F]{2}){1,8}$`)
)

// BuildLink returns the vless:// link for one subscription on one node.
//
// The layout is the one the panel's own link generator writes (sorted query,
// path-escaped name), so a client that imports the panel's links imports these.
func BuildLink(p LinkParams) (string, error) {
	if !uuidRe.MatchString(p.UUID) {
		return "", errors.New("link: the client id is not a UUID")
	}
	host := strings.TrimSpace(p.Host)
	if host == "" {
		return "", errors.New("link: the node address is empty")
	}
	if p.Port < 1 || p.Port > 65535 {
		return "", fmt.Errorf("link: port %d is out of range", p.Port)
	}
	if p.ServerName == "" || p.PublicKey == "" {
		return "", errors.New("link: the server name and public key are required")
	}
	if !shortIDRe.MatchString(p.ShortID) {
		return "", errors.New("link: the short id must be 1 to 8 bytes of hex")
	}
	if strings.TrimSpace(p.Name) == "" {
		return "", errors.New("link: the name is empty")
	}
	fp, flow := p.Fingerprint, p.Flow
	if fp == "" {
		fp = DefaultFingerprint
	}
	if flow == "" {
		flow = DefaultFlow
	}

	q := url.Values{}
	q.Set("security", "reality")
	q.Set("type", "tcp")
	q.Set("encryption", "none")
	q.Set("flow", flow)
	q.Set("sni", p.ServerName)
	q.Set("pbk", p.PublicKey)
	q.Set("sid", p.ShortID)
	q.Set("fp", fp)

	// JoinHostPort brackets an IPv6 literal, which a URL needs.
	hostPort := net.JoinHostPort(host, strconv.Itoa(p.Port))
	return fmt.Sprintf("vless://%s@%s?%s#%s", p.UUID, hostPort, q.Encode(), url.PathEscape(p.Name)), nil
}
