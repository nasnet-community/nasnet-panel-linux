package usecase

import (
	"context"
	"sort"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
)

// NodeXray is one node's Xray, for the Other parts card.
type NodeXray struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Local   bool   `json:"local,omitempty"`
}

// Nodes is what the card reads from the node side, without asking agents.
type Nodes interface {
	AgentUpdates(ctx context.Context) (map[uint]bool, error)
	XrayVersions(ctx context.Context) ([]NodeXray, error)
}

// Parts are what updates outside Settings: every row links to its page,
// none runs an update from here.
type Parts struct {
	Agents   *AgentsPart  `json:"agents,omitempty"`
	Xray     *XrayPart    `json:"xray,omitempty"`
	Geofiles GeofilesPart `json:"geofiles"`
}

type AgentsPart struct {
	Total int    `json:"total"` // remote nodes whose agent the panel knows
	Due   []uint `json:"due"`
}

// XrayPart compares every node, the built-in one too, with the version
// this panel was tested with. Newer ones are only flagged; nothing lowers
// them.
type XrayPart struct {
	Tested  string     `json:"tested"`
	Current int        `json:"current"`
	Older   []NodeXray `json:"older"`
	Newer   []NodeXray `json:"newer"`
}

// GeofilesPart: the geofiles ship inside the panel binary.
type GeofilesPart struct {
	BuiltAt *time.Time `json:"built_at,omitempty"`
}

func (u *Usecase) parts(ctx context.Context) Parts {
	var p Parts
	if !u.deps.BuiltAt.IsZero() {
		t := u.deps.BuiltAt
		p.Geofiles.BuiltAt = &t
	}
	nodes := u.deps.Nodes
	if nodes == nil {
		return p
	}
	if due, err := nodes.AgentUpdates(ctx); err == nil {
		a := &AgentsPart{Total: len(due), Due: []uint{}}
		for id, d := range due {
			if d {
				a.Due = append(a.Due, id)
			}
		}
		sort.Slice(a.Due, func(i, j int) bool { return a.Due[i] < a.Due[j] })
		p.Agents = a
	}
	if xs, err := nodes.XrayVersions(ctx); err == nil {
		p.Xray = compareXray(xs, pin.Version)
	}
	return p
}

func compareXray(xs []NodeXray, tested string) *XrayPart {
	x := &XrayPart{Tested: tested, Older: []NodeXray{}, Newer: []NodeXray{}}
	want, err := release.Parse("v" + tested)
	if err != nil {
		return x
	}
	for _, n := range xs {
		v, err := release.Parse("v" + n.Version)
		switch {
		case err != nil:
			continue
		case v.Compare(want) < 0:
			x.Older = append(x.Older, n)
		case v.Compare(want) > 0:
			x.Newer = append(x.Newer, n)
		default:
			x.Current++
		}
	}
	byName := func(s []NodeXray) {
		sort.Slice(s, func(i, j int) bool { return s[i].Name < s[j].Name })
	}
	byName(x.Older)
	byName(x.Newer)
	return x
}
