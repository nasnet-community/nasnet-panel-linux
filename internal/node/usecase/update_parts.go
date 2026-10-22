package usecase

import (
	"context"
	"strings"
)

// NodeXrayVersion is one node's Xray as the stats sweep last saw it.
type NodeXrayVersion struct {
	ID      uint
	Name    string
	Version string
	Local   bool
}

// XrayVersions lists the nodes the last stats sweep reached and the Xray
// each one runs, without asking any agent. MTProto nodes run no Xray.
func (u *nodeUsecase) XrayVersions(ctx context.Context) ([]NodeXrayVersion, error) {
	nodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	var out []NodeXrayVersion
	for _, n := range nodes {
		if n.IsLite() {
			continue
		}
		snap, _, ok := u.liveStats.get(n.ID)
		if !ok {
			continue
		}
		if v := XrayVersionNumber(snap.XrayVersion); v != "" {
			out = append(out, NodeXrayVersion{ID: n.ID, Name: n.Name, Version: v, Local: n.IsLocal()})
		}
	}
	return out, nil
}

// XrayVersionNumber reads "Xray 26.10.10 (Xray, Penetrates Everything.) …"
// as 26.10.10.
func XrayVersionNumber(reported string) string {
	fields := strings.Fields(reported)
	if len(fields) > 1 && strings.EqualFold(fields[0], "Xray") {
		return strings.TrimPrefix(fields[1], "v")
	}
	if len(fields) == 1 && fields[0] != "unknown" {
		return strings.TrimPrefix(fields[0], "v")
	}
	return ""
}
