package usecase

import (
	"context"
	"testing"
)

type fakeNodes struct{}

func (fakeNodes) AgentUpdates(context.Context) (map[uint]bool, error) {
	return map[uint]bool{3: true, 1: false, 2: true}, nil
}

func (fakeNodes) XrayVersions(context.Context) ([]NodeXray, error) {
	return []NodeXray{
		{ID: 1, Name: "this router", Version: "26.10.10", Local: true},
		{ID: 2, Name: "Warsaw", Version: "26.9.30"},
		{ID: 3, Name: "Helsinki", Version: "26.11.2"},
		{ID: 4, Name: "Berlin", Version: "26.5.9-wg1"},
		{ID: 5, Name: "Odd", Version: "custom"},
	}, nil
}

func TestPartsSortNodesAgainstWhatThisPanelShips(t *testing.T) {
	u := New(Deps{Paths: Paths{Root: t.TempDir()}, Version: "v1.0.0", Nodes: fakeNodes{}})
	p := u.parts(context.Background())
	if p.Agents == nil || p.Agents.Total != 3 || len(p.Agents.Due) != 2 || p.Agents.Due[0] != 2 {
		t.Fatalf("agents = %+v", p.Agents)
	}
	x := compareXray(func() []NodeXray { xs, _ := fakeNodes{}.XrayVersions(context.Background()); return xs }(), "26.10.10")
	if x.Current != 1 || len(x.Older) != 2 || x.Older[0].Name != "Berlin" || len(x.Newer) != 1 || x.Newer[0].Name != "Helsinki" {
		t.Fatalf("xray = %+v", x)
	}
	if p.Geofiles.BuiltAt != nil {
		t.Fatal("a build date without one")
	}
}
