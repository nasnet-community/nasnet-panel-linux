package bandwidth

import "fmt"

// Each tier is one pipe per node, shared by its users. The tier's outbound
// copies set its socket priority, which the agent shapes on; replies match by
// the tier the agent keeps in the conntrack mark. SO_MARK stays free for
// dual-WAN routing.
const (
	PriorityMajor = 0xb0 // tc major of the SO_PRIORITY on tier sockets (b0:N)
	// ClassMajor is the HTB handle on the agent's IFBs. Redirected packets are
	// re-tagged b1:N, so a tunnel's outer packet isn't counted again.
	ClassMajor = 0xb1
	// The tier level sits in the conntrack mark's top nibble, the field
	// pkg/netmark reserves; the router's save/restore never carries it.
	ConnMarkShift        = 28
	ConnMarkMask  uint32 = 0xf0000000
)

// Tier represents a bandwidth tier with its Xray level.
type Tier struct {
	Level    uint32 // Xray user Level, also the tier's index (1-15)
	RateMbit int    // Rate in Mbps (0 = unlimited)
	CeilMbit int    // Ceiling in Mbps (burst ceiling)
	Burst    string // HTB burst parameter (e.g. "256k")
}

// SocketPriority is the SO_PRIORITY set on the tier's outbound sockets.
func (t Tier) SocketPriority() uint32 {
	return PriorityMajor<<16 | t.Level
}

// ClassID is the tier's HTB class on the agent's IFB devices.
func (t Tier) ClassID() string {
	return fmt.Sprintf("%x:%x", ClassMajor, t.Level)
}

// ConnMark is the conntrack mark bits that identify the tier's connections.
func (t Tier) ConnMark() uint32 {
	return t.Level << ConnMarkShift
}

// predefined tiers — keep this small (3-5 tiers cover most plans)
var tiers = []Tier{
	{Level: 0, RateMbit: 0, CeilMbit: 0, Burst: ""},       // unlimited (default)
	{Level: 1, RateMbit: 10, CeilMbit: 12, Burst: "256k"}, // 10 Mbps
	{Level: 2, RateMbit: 30, CeilMbit: 35, Burst: "384k"}, // 30 Mbps
	{Level: 3, RateMbit: 50, CeilMbit: 55, Burst: "512k"}, // 50 Mbps
	{Level: 4, RateMbit: 100, CeilMbit: 110, Burst: "1m"}, // 100 Mbps
	{Level: 5, RateMbit: 200, CeilMbit: 220, Burst: "2m"}, // 200 Mbps
	{Level: 6, RateMbit: 500, CeilMbit: 550, Burst: "4m"}, // 500 Mbps
}

// AllTiers returns all defined bandwidth tiers
func AllTiers() []Tier {
	return tiers
}

// RateLimitedTiers returns only tiers with actual rate limits (excludes unlimited)
func RateLimitedTiers() []Tier {
	var result []Tier
	for _, t := range tiers {
		if t.RateMbit > 0 {
			result = append(result, t)
		}
	}
	return result
}

// GetTier returns the appropriate bandwidth tier for a given Mbps limit.
// If bandwidthMbps is 0, returns the unlimited tier.
// Otherwise, returns the closest tier that is >= the requested bandwidth.
// If the requested bandwidth exceeds all tiers, returns the highest tier.
func GetTier(bandwidthMbps int) Tier {
	if bandwidthMbps <= 0 {
		return tiers[0] // unlimited
	}

	// Find the closest tier >= requested bandwidth
	for _, t := range tiers[1:] {
		if t.RateMbit >= bandwidthMbps {
			return t
		}
	}

	// Requested bandwidth exceeds all defined tiers — cap at the highest tier
	return tiers[len(tiers)-1]
}
