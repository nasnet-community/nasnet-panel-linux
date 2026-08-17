package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

// NftCounter is one named counter's live reading.
type NftCounter struct {
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

// NftObjects is what actually exists in the kernel's copy of our table.
type NftObjects struct {
	Chains   []string
	Sets     []string
	Counters map[string]NftCounter
}

// NftReader reads live kernel nftables state. The manager's Snapshot is only
// the desired state — a debugging page needs what the kernel really holds.
type NftReader interface {
	ListRuleset(ctx context.Context) (string, error)
	LiveObjects(ctx context.Context) (*NftObjects, error)
	SetContains(ctx context.Context, set, element string) (bool, error)
}

type LiveNft struct{ Bin string }

func NewLiveNft() *LiveNft { return &LiveNft{Bin: "nft"} }

func (l *LiveNft) ListRuleset(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, l.Bin, "list", "table", nft.TableFamily, nft.TableName).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("nft list table: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (l *LiveNft) LiveObjects(ctx context.Context) (*NftObjects, error) {
	out, err := exec.CommandContext(ctx, l.Bin, "-j", "list", "table", nft.TableFamily, nft.TableName).Output()
	if err != nil {
		return nil, fmt.Errorf("nft -j list table: %w", err)
	}
	return parseNftObjects(out)
}

func parseNftObjects(raw []byte) (*NftObjects, error) {
	var doc struct {
		Nftables []struct {
			Chain *struct {
				Family, Table, Name string
			} `json:"chain"`
			Set *struct {
				Family, Table, Name string
			} `json:"set"`
			Counter *struct {
				Family, Table, Name string
				Packets             uint64 `json:"packets"`
				Bytes               uint64 `json:"bytes"`
			} `json:"counter"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse nft json: %w", err)
	}
	obj := &NftObjects{Counters: map[string]NftCounter{}}
	ours := func(family, table string) bool { return family == nft.TableFamily && table == nft.TableName }
	for _, e := range doc.Nftables {
		switch {
		case e.Chain != nil && ours(e.Chain.Family, e.Chain.Table):
			obj.Chains = append(obj.Chains, e.Chain.Name)
		case e.Set != nil && ours(e.Set.Family, e.Set.Table):
			obj.Sets = append(obj.Sets, e.Set.Name)
		case e.Counter != nil && ours(e.Counter.Family, e.Counter.Table):
			obj.Counters[e.Counter.Name] = NftCounter{Packets: e.Counter.Packets, Bytes: e.Counter.Bytes}
		}
	}
	return obj, nil
}

// SetContains asks the kernel, so interval sets answer correctly for any IP
// inside a CIDR — something no in-process copy of the elements can do cheaply.
func (l *LiveNft) SetContains(ctx context.Context, set, element string) (bool, error) {
	if net.ParseIP(element) == nil {
		return false, fmt.Errorf("not an IP: %q", element)
	}
	err := exec.CommandContext(ctx, l.Bin, "get", "element", nft.TableFamily, nft.TableName,
		set, "{ "+element+" }").Run()
	if err == nil {
		return true, nil
	}
	// Non-zero exit means "not in the set" (or the set is missing — the
	// mismatch checks surface that); only a failure to run is an error.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, err
}
