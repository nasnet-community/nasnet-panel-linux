// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	// FileName is the history's file in the data folder, next to state.json.
	FileName = "usage.json"

	// SchemaVersion is the file's format. Bump it whenever a change would make
	// an older build misread the file.
	SchemaVersion = 1
)

// errNewer means a newer release wrote the file. Reading it would drop what
// that release added, and writing over it would lose it, so it is left alone.
var errNewer = errors.New("usage file was written by a newer version")

// The file is compact, not indented: it is rewritten on flash, and the days
// of a few series are most of what is in it.
type fileDoc struct {
	Version int        `json:"version"`
	Nodes   []fileNode `json:"nodes"`
	// Retired is what subscriptions moved through nodes that have since been
	// removed, so that taking a node away doesn't give a limit its bytes back.
	Retired []fileTotal `json:"retired,omitempty"`
}

type fileTotal struct {
	ID    uint   `json:"id"`
	Total Totals `json:"total"`
}

type fileNode struct {
	ID            uint              `json:"id"`
	AckedThrough  int64             `json:"acked_through"`
	Bridge        Totals            `json:"bridge"`
	Days          map[string]Totals `json:"days"`
	Subscriptions []fileSub         `json:"subscriptions"`
}

type fileSub struct {
	ID   uint              `json:"id"`
	Days map[string]Totals `json:"days"`
	// Total is what the subscription has moved through the node in all. A file
	// from before totals were kept has none, and the days stand in for it.
	Total *Totals `json:"total,omitempty"`
}

func dayStrings(series map[day]Totals) map[string]Totals {
	out := make(map[string]Totals, len(series))
	for d, t := range series {
		out[d.String()] = t
	}
	return out
}

// encode is the file's form of the nodes: the same bytes for the same history
// (the days are map keys, which the encoder sorts), so that a flush can tell
// whether anything changed by comparing them.
func encode(nodes map[uint]*nodeHist) ([]byte, error) { return encodeDoc(nodes, nil) }

func encodeDoc(nodes map[uint]*nodeHist, retired map[uint]Totals) ([]byte, error) {
	doc := fileDoc{Version: SchemaVersion, Nodes: []fileNode{}}
	for id, h := range nodes {
		if h.empty() {
			continue
		}
		fn := fileNode{ID: id, AckedThrough: h.through, Bridge: h.bridge, Days: dayStrings(h.days), Subscriptions: []fileSub{}}
		ids := map[uint]bool{}
		for sid := range h.subs {
			ids[sid] = true
		}
		for sid := range h.life {
			ids[sid] = true
		}
		for sid := range ids {
			total := h.life[sid]
			fn.Subscriptions = append(fn.Subscriptions, fileSub{ID: sid, Days: dayStrings(h.subs[sid]), Total: &total})
		}
		slices.SortFunc(fn.Subscriptions, func(a, b fileSub) int { return cmp.Compare(a.ID, b.ID) })
		doc.Nodes = append(doc.Nodes, fn)
	}
	slices.SortFunc(doc.Nodes, func(a, b fileNode) int { return cmp.Compare(a.ID, b.ID) })
	for id, t := range retired {
		if !t.isZero() {
			doc.Retired = append(doc.Retired, fileTotal{ID: id, Total: t})
		}
	}
	slices.SortFunc(doc.Retired, func(a, b fileTotal) int { return cmp.Compare(a.ID, b.ID) })
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// decode reads what encode wrote. errNewer means a newer release wrote it;
// any other error means the contents can't be used as they are.
func decode(b []byte) (map[uint]*nodeHist, error) {
	nodes, _, err := decodeDoc(b)
	return nodes, err
}

func decodeDoc(b []byte) (map[uint]*nodeHist, map[uint]Totals, error) {
	// the version first: a newer file may differ in ways the full decode would trip on
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, nil, fmt.Errorf("unreadable: %w", err)
	}
	switch {
	case head.Version > SchemaVersion:
		return nil, nil, fmt.Errorf("%w: the file is version %d and this build reads up to %d", errNewer, head.Version, SchemaVersion)
	case head.Version < 1:
		return nil, nil, errors.New("unreadable: the file has no version")
	}
	var doc fileDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, nil, fmt.Errorf("unreadable: %w", err)
	}

	out := make(map[uint]*nodeHist, len(doc.Nodes))
	for _, fn := range doc.Nodes {
		if fn.ID == 0 || out[fn.ID] != nil {
			return nil, nil, fmt.Errorf("unreadable: node id %d is zero or repeated", fn.ID)
		}
		if fn.AckedThrough < 0 {
			return nil, nil, fmt.Errorf("unreadable: node %d has a negative mark", fn.ID)
		}
		h := newNodeHist()
		h.through, h.bridge = fn.AckedThrough, fn.Bridge
		var err error
		if h.days, err = parseDays(fn.Days); err != nil {
			return nil, nil, fmt.Errorf("unreadable: node %d: %w", fn.ID, err)
		}
		seen := map[uint]bool{}
		for _, sub := range fn.Subscriptions {
			if sub.ID == 0 || seen[sub.ID] {
				return nil, nil, fmt.Errorf("unreadable: node %d: subscription id %d is zero or repeated", fn.ID, sub.ID)
			}
			seen[sub.ID] = true
			series, err := parseDays(sub.Days)
			if err != nil {
				return nil, nil, fmt.Errorf("unreadable: node %d, subscription %d: %w", fn.ID, sub.ID, err)
			}
			if len(series) > 0 {
				h.subs[sub.ID] = series
			}
			switch {
			case sub.Total != nil:
				h.life[sub.ID] = *sub.Total
			case len(series) > 0:
				h.life[sub.ID] = span(series, math.MinInt64, math.MaxInt64)
			}
		}
		out[fn.ID] = h
	}
	retired := make(map[uint]Totals, len(doc.Retired))
	for _, r := range doc.Retired {
		if r.ID == 0 {
			return nil, nil, errors.New("unreadable: a retired total has no subscription id")
		}
		retired[r.ID] = retired[r.ID].plus(r.Total)
	}
	return out, retired, nil
}

func parseDays(in map[string]Totals) (map[day]Totals, error) {
	out := make(map[day]Totals, len(in))
	for s, t := range in {
		d, err := parseDay(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not a day", s)
		}
		out[d] = t
	}
	return out, nil
}

// dropStrayTemps removes what a crash left between creating a temp file and
// renaming it.
func dropStrayTemps(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "."+FileName+"-") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// setAside moves a file that can't be used next to itself, as
// usage.json.corrupt-<UTC time>, so the next write doesn't replace the only
// copy. The history is a counter, not the CA: losing it is a pity, and the
// collector starts again from empty rather than stop.
func (c *Collector) setAside(cause error) {
	stamp := c.now().UTC().Format("20060102T150405Z")
	for i := 0; i < 100; i++ {
		to := c.path + ".corrupt-" + stamp
		if i > 0 {
			to += "-" + strconv.Itoa(i) // two files in one second
		}
		if _, err := os.Lstat(to); err == nil || !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := os.Rename(c.path, to); err != nil {
			c.logf("usage: %s can't be used (%v) and can't be moved aside (%v); starting empty", c.path, cause, err)
			return
		}
		c.logf("usage: %s can't be used (%v); it is kept as %s and the history starts empty", c.path, cause, to)
		return
	}
	c.logf("usage: %s can't be used (%v); starting empty", c.path, cause)
}
