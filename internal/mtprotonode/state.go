package mtprotonode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/nasnet-community/mtproto-remote-plugin/ctrl"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// Extras are the parts of an agent's state that do not come from the tunnel row.
type Extras struct {
	Prefixes   []netip.Prefix // the datacentre allowlist the capture rules redirect
	MaxStreams int
}

// StateFromPayload reuses nodebridge's payload so subscription policy lives in one place.
func StateFromPayload(p *pb.CoreStatePayload, x Extras) (ctrl.State, error) {
	if p.GetCoreType() != "mtproto" {
		return ctrl.State{}, fmt.Errorf("MTProto nodes only run MTProto, not %q", p.GetCoreType())
	}
	var s struct {
		TLSDomain string `json:"tls_domain"`
	}
	if len(p.GetSettings()) > 0 {
		if err := json.Unmarshal(p.GetSettings(), &s); err != nil {
			return ctrl.State{}, fmt.Errorf("tunnel settings: %w", err)
		}
	}
	domain := strings.ToLower(strings.TrimSpace(s.TLSDomain))
	if domain == "" {
		return ctrl.State{}, fmt.Errorf("an MTProto node's tunnel needs a cover site (tls_domain)")
	}
	st := ctrl.State{
		Port: int(p.GetPort()), Listen: p.GetListen(), Disguise: domain,
		MaxStreams: x.MaxStreams,
	}
	if st.Port == 0 {
		st.Port = 443
	}
	for _, pf := range x.Prefixes {
		st.DCNets = append(st.DCNets, pf.String())
	}
	if len(p.GetSettings()) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(p.GetSettings(), &raw); err != nil {
			return ctrl.State{}, err
		}
		st.Telemt = raw
	}
	// no users stops telemt but keeps the redirect, so nothing leaves the node directly
	if p.GetEnabled() {
		for _, u := range p.GetUsers() {
			name := u.GetEmail()
			if name == "" {
				name = u.GetUsername()
			}
			cu := ctrl.User{
				Name: name, Secret: u.GetSecret(), Enabled: u.GetEnabled(), MaxIPs: int(u.GetKLimit()),
				RateUpBps: u.GetRateLimitUpBps(), RateDownBps: u.GetRateLimitDownBps(),
			}
			if sec := u.GetExpiresAtUnix(); sec > 0 {
				cu.ExpiresAt = time.Unix(sec, 0).UTC()
			}
			st.Users = append(st.Users, cu)
		}
	}
	sort.Slice(st.Users, func(i, j int) bool { return st.Users[i].Name < st.Users[j].Name })
	st.Rev = Rev(st)
	return st, nil
}

// Rev is a content hash, so the agent's reported revision shows drift.
func Rev(s ctrl.State) string {
	s.Rev = ""
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}
