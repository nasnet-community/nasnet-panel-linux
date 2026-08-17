// Package system is the privileged edge
// netlink, the networkd config dir, the nft table and sysctl..
package system

import "context"

// Rule is one routing policy rule. Zero FwMask -> no mark match
type Rule struct {
	Pref    int
	FwMark  uint32
	FwMask  uint32
	OifName string
	Table   int
	// Blackhole terminates a group (so traffic won't leak to any other uplink)
	Blackhole bool
	// SuppressPrefixLen mirrors suppress_prefixlength.
	SuppressPrefixLen int
	SuppressSet       bool
}

// Equal compares the whole spec. Pref alone is not identity — the pin rules and
// the group rules differ only in mark and action.
func (r Rule) Equal(o Rule) bool {
	return r.Pref == o.Pref && r.FwMark == o.FwMark && r.FwMask == o.FwMask &&
		r.OifName == o.OifName && r.Table == o.Table && r.Blackhole == o.Blackhole &&
		r.SuppressPrefixLen == o.SuppressPrefixLen && r.SuppressSet == o.SuppressSet
}

// Route is one route in one table. Dest "default" -> 0.0.0.0/0.
type Route struct {
	Table   int
	Dest    string
	Gateway string
	OifName string
	Scope   string // "" | "link"
	Metric  int
}

// Addr is one address on one interface, in CIDR form.
type Addr struct {
	IfName string
	CIDR   string
}

// Backend is every privileged kernel operation this feature needs
type Backend interface {
	Links(ctx context.Context) ([]string, error)
	Addrs(ctx context.Context) ([]Addr, error)

	RuleList(ctx context.Context) ([]Rule, error)
	// RuleAdd is idempotent: adding an existing rule is a no-op.
	RuleAdd(ctx context.Context, r Rule) error
	// RuleDel is idempotent: deleting an absent rule is a no-op.
	RuleDel(ctx context.Context, r Rule) error

	RouteList(ctx context.Context, table int) ([]Route, error)
	// RouteReplace overwrites the route for (Table, Dest).
	RouteReplace(ctx context.Context, r Route) error
	RouteDel(ctx context.Context, r Route) error
	// RouteGet asks the kernel where dst would go with this fwmark — the same
	// decision a real packet gets, rules and all.
	RouteGet(ctx context.Context, dst string, mark uint32) (*Route, error)

	SysctlSet(ctx context.Context, key, value string) error
	SysctlGet(ctx context.Context, key string) (string, error)
}
