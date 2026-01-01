package bandwidth

import "fmt"

// Tier represents a bandwidth tier with its Xray level and firewall mark
type Tier struct {
	Level    uint32 // Xray user Level
	Mark     uint32 // nftables/iptables fwmark for TC classification
	RateMbit int    // Rate in Mbps (0 = unlimited)
	CeilMbit int    // Ceiling in Mbps (burst ceiling)
	Burst    string // HTB burst parameter (e.g. "256k")
}

// OutboundTag returns the Xray outbound tag for this tier
func (t Tier) OutboundTag() string {
	if t.RateMbit == 0 {
		return "direct"
	}
	return fmt.Sprintf("direct-bw%d", t.RateMbit)
}

// TCClassID returns the TC class ID (1:mark) for this tier
func (t Tier) TCClassID() string {
	if t.Mark == 0 {
		return "1:99" // default class
	}
	return fmt.Sprintf("1:%d", t.Mark)
}

// predefined tiers — keep this small (3-5 tiers cover most plans)
var tiers = []Tier{
	{Level: 0, Mark: 0, RateMbit: 0, CeilMbit: 0, Burst: ""},         // unlimited (default)
	{Level: 1, Mark: 10, RateMbit: 10, CeilMbit: 12, Burst: "256k"},  // 10 Mbps
	{Level: 2, Mark: 30, RateMbit: 30, CeilMbit: 35, Burst: "384k"},  // 30 Mbps
	{Level: 3, Mark: 50, RateMbit: 50, CeilMbit: 55, Burst: "512k"},  // 50 Mbps
	{Level: 4, Mark: 100, RateMbit: 100, CeilMbit: 110, Burst: "1m"}, // 100 Mbps
	{Level: 5, Mark: 200, RateMbit: 200, CeilMbit: 220, Burst: "2m"}, // 200 Mbps
	{Level: 6, Mark: 500, RateMbit: 500, CeilMbit: 550, Burst: "4m"}, // 500 Mbps
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
