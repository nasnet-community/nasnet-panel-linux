package system

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/oui"
)

// LeasePath is where dnsmasq writes leases. Pinned in the render rather than
// left to the compiled-in default, so the reader and the writer cannot drift.
const LeasePath = "/var/lib/misc/dnsmasq.leases"

// DefaultBridgeAgeingSeconds is the kernel's stock bridge ageing time, used
// only when the real one cannot be read. The bridge's own value always wins.
const DefaultBridgeAgeingSeconds = 300

// Lease is one dnsmasq DHCP lease.
type Lease struct {
	Expiry   time.Time
	MAC      string
	IP       string
	Hostname string
}

// Neighbour is one IPv4 ARP entry. IPv6 is dropped: link-local survives on the
// bridge even with IPv6 off, and it is never the address a client is reached on.
type Neighbour struct {
	IP    string
	MAC   string
	State string
}

// FDBEntry is one MAC learned on a bridge member port.
type FDBEntry struct {
	MAC string
	// Port is the member the MAC was learned on, not the bridge.
	Port string
	// Updated is seconds since the entry was last refreshed. This is the
	// liveness signal: the kernel ages it out deterministically, unlike a
	// neighbour state.
	Updated int
}

// ParseLeases reads the dnsmasq lease file: expiry, MAC, IP, hostname,
// client-id. A malformed line is skipped — a device list must not fail because
// one client wrote something strange.
func ParseLeases(r io.Reader) []Lease {
	var out []Lease
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		mac := oui.Normalize(f[1])
		if mac == "" {
			continue
		}
		secs, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, Lease{
			Expiry:   time.Unix(secs, 0).UTC(),
			MAC:      mac,
			IP:       f[2],
			Hostname: SanitizeHostname(f[3]),
		})
	}
	return out
}

// ParseNeigh reads `ip -j neigh`. state is a JSON array, not a string.
func ParseNeigh(data []byte) ([]Neighbour, error) {
	var raw []struct {
		Dst    string   `json:"dst"`
		LLAddr string   `json:"lladdr"`
		State  []string `json:"state"`
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil // no neighbours is not an error
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse neighbours: %w", err)
	}

	var out []Neighbour
	for _, e := range raw {
		mac := oui.Normalize(e.LLAddr)
		// No lladdr means the address never resolved, so it names no device.
		if mac == "" || strings.Contains(e.Dst, ":") {
			continue
		}
		st := ""
		if len(e.State) > 0 {
			st = e.State[0]
		}
		if st == "FAILED" || st == "INCOMPLETE" {
			continue
		}
		out = append(out, Neighbour{IP: e.Dst, MAC: mac, State: st})
	}
	return out, nil
}

// ParseFDB reads `bridge -s -j fdb` and keeps only learned client MACs.
//
// Three kinds of noise have to go, and the obvious filter catches only the
// first: the bridge's own multicast subscriptions (flags "self"), the member
// ports' own MACs (flags empty, state "permanent" — so filtering on "self"
// alone misses them), and a duplicate of every entry carrying a vlan.
func ParseFDB(data []byte, bridge string) ([]FDBEntry, error) {
	var raw []struct {
		MAC     string   `json:"mac"`
		IfName  string   `json:"ifname"`
		Flags   []string `json:"flags"`
		Master  string   `json:"master"`
		State   string   `json:"state"`
		Updated *int     `json:"updated"`
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse bridge fdb: %w", err)
	}

	seen := map[string]int{}
	var out []FDBEntry
	for _, e := range raw {
		mac := oui.Normalize(e.MAC)
		switch {
		case mac == "" || oui.IsGroup(mac):
		case e.Master != bridge || e.IfName == bridge:
		case e.State == "permanent" || slices.Contains(e.Flags, "self"):
		case e.Updated == nil:
		default:
			// Keep the freshest sighting: the vlan duplicate can lag.
			if i, dup := seen[mac]; dup {
				if *e.Updated < out[i].Updated {
					out[i].Updated = *e.Updated
				}
				continue
			}
			seen[mac] = len(out)
			out = append(out, FDBEntry{MAC: mac, Port: e.IfName, Updated: *e.Updated})
		}
	}
	return out, nil
}

// SanitizeHostname keeps RFC-1123 label characters and nothing else.
//
// The value is client-supplied. React escapes it, so the risk is not XSS but
// spoofing: a homograph, an RTL override or a control character renders as a
// name the operator trusts. Rejecting outright beats stripping, because a
// mangled name still looks legitimate.
func SanitizeHostname(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "*" || len(s) > 63 {
		return ""
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_'
		if !ok {
			return ""
		}
	}
	return s
}

// BridgeAgeingSeconds reads the bridge's own ageing time. An FDB entry older
// than this is gone, so it is the offline threshold — read, never hardcoded.
func BridgeAgeingSeconds(bridge string) (int, error) {
	b, err := os.ReadFile(filepath.Join("/sys/class/net", bridge, "bridge/ageing_time"))
	if err != nil {
		return 0, err
	}
	return parseAgeingTicks(string(b))
}

// parseAgeingTicks converts what sysfs reports. USER_HZ is 100, so the stock
// 30000 is 300 seconds.
func parseAgeingTicks(s string) (int, error) {
	ticks, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("parse ageing_time: %w", err)
	}
	if ticks <= 0 {
		// 0 disables ageing: entries never expire, so nothing could go offline.
		return 0, fmt.Errorf("ageing_time is %d; the bridge is not ageing entries", ticks)
	}
	return ticks / 100, nil
}

// DeviceSource is the read-only view of what is on the bridge. Deliberately not
// part of Backend: that is the apply/rollback seam, threaded through the
// two-phase machinery, and observation has no business there.
type DeviceSource interface {
	Leases(ctx context.Context) ([]Lease, error)
	Neighbours(ctx context.Context, bridge string) ([]Neighbour, error)
	FDB(ctx context.Context, bridge string) ([]FDBEntry, error)
	AgeingSeconds(ctx context.Context, bridge string) (int, error)
}

// LiveDeviceSource reads the running system.
type LiveDeviceSource struct {
	LeaseFile string
}

func NewDeviceSource() *LiveDeviceSource {
	return &LiveDeviceSource{LeaseFile: LeasePath}
}

func (s *LiveDeviceSource) Leases(context.Context) ([]Lease, error) {
	path := s.LeaseFile
	if path == "" {
		path = LeasePath
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseLeases(f), nil
}

func (s *LiveDeviceSource) Neighbours(ctx context.Context, bridge string) ([]Neighbour, error) {
	out, err := exec.CommandContext(ctx, "ip", "-j", "neigh", "show", "dev", bridge).Output()
	if err != nil {
		return nil, fmt.Errorf("ip neigh: %w", err)
	}
	return ParseNeigh(out)
}

func (s *LiveDeviceSource) FDB(ctx context.Context, bridge string) ([]FDBEntry, error) {
	out, err := exec.CommandContext(ctx, "bridge", "-s", "-j", "fdb", "show", "br", bridge).Output()
	if err != nil {
		return nil, fmt.Errorf("bridge fdb: %w", err)
	}
	return ParseFDB(out, bridge)
}

func (s *LiveDeviceSource) AgeingSeconds(_ context.Context, bridge string) (int, error) {
	return BridgeAgeingSeconds(bridge)
}
