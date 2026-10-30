// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// histories builds the same two nodes in whatever order it is asked to, so a
// test can tell whether the bytes depend on it.
func histories(reverse bool) map[uint]*nodeHist {
	build := func(id uint) *nodeHist {
		h := newNodeHist()
		for i := 0; i < 3; i++ {
			k := uint64(i)
			if reverse {
				k = uint64(2 - i)
			}
			h.add(unix(2026, 10, 28+int(k), 10, 0, 0), subs(1, 10+k, 100+k, 12, 3+k, 4+k, 3, 1, 1))
		}
		h.bridge = Totals{Up: 77 + uint64(id), Down: 88}
		h.through = unix(2026, 10, 30, 10, 0, 0)
		return h
	}
	out := map[uint]*nodeHist{}
	ids := []uint{1, 3}
	if reverse {
		ids = []uint{3, 1}
	}
	for _, id := range ids {
		out[id] = build(id)
	}
	return out
}

func TestEncodeGivesTheSameBytesForTheSameHistory(t *testing.T) {
	a, err := encode(histories(false))
	must(t, err)
	b, err := encode(histories(true))
	must(t, err)
	if !bytes.Equal(a, b) {
		t.Fatalf("the bytes depend on the order things were added:\n%s\n%s", a, b)
	}
	if !bytes.HasSuffix(a, []byte("}\n")) || bytes.Contains(a, []byte("\n  ")) {
		t.Errorf("the file is meant to be compact, one line: %s", a)
	}
}

func TestTheFileHasItsDocumentedShape(t *testing.T) {
	b, err := encode(histories(false))
	must(t, err)
	var doc map[string]any
	must(t, json.Unmarshal(b, &doc))
	if doc["version"] != float64(SchemaVersion) {
		t.Errorf("version = %v", doc["version"])
	}
	nodes := doc["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("nodes = %v", nodes)
	}
	first := nodes[0].(map[string]any)
	for _, key := range []string{"id", "acked_through", "bridge", "days", "subscriptions"} {
		if _, ok := first[key]; !ok {
			t.Errorf("a node has no %q: %v", key, first)
		}
	}
	if first["id"] != float64(1) || nodes[1].(map[string]any)["id"] != float64(3) {
		t.Errorf("nodes are not in order of id: %v", nodes)
	}
	day := first["days"].(map[string]any)["2026-10-28"].(map[string]any)
	if day["up"] == nil || day["down"] == nil {
		t.Errorf("a day is %v", day)
	}
	subsList := first["subscriptions"].([]any)
	if len(subsList) != 3 || subsList[0].(map[string]any)["id"] != float64(1) || subsList[2].(map[string]any)["id"] != float64(12) {
		t.Errorf("subscriptions are not in order of id: %v", subsList)
	}
}

func TestAnEmptyHistoryIsAnEmptyList(t *testing.T) {
	b, err := encode(map[uint]*nodeHist{})
	must(t, err)
	if string(b) != `{"version":1,"nodes":[]}`+"\n" {
		t.Fatalf("empty history is %s", b)
	}
	// a node with nothing in it is not worth a place; one with only a mark is
	b, err = encode(map[uint]*nodeHist{1: newNodeHist(), 2: {days: map[day]Totals{}, subs: map[uint]map[day]Totals{}, through: 9}})
	must(t, err)
	if !strings.Contains(string(b), `"id":2`) || strings.Contains(string(b), `"id":1`) {
		t.Fatalf("file = %s", b)
	}
	if !strings.Contains(string(b), `"days":{}`) || !strings.Contains(string(b), `"subscriptions":[]`) {
		t.Fatalf("empties are not written as {} and []: %s", b)
	}
}

func TestTheFileRoundTrips(t *testing.T) {
	want := histories(false)
	b, err := encode(want)
	must(t, err)
	got, err := decode(b)
	must(t, err)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decode(encode(h)) differs:\n got %+v\nwant %+v", got, want)
	}
	again, err := encode(got)
	must(t, err)
	if !bytes.Equal(b, again) {
		t.Fatal("a second encode wrote other bytes")
	}
}

func TestDecodeRefusesWhatItCannotUse(t *testing.T) {
	good := `{"version":1,"nodes":[{"id":1,"acked_through":5,"bridge":{"up":0,"down":0},"days":{"2026-10-30":{"up":1,"down":2}},"subscriptions":[{"id":2,"days":{"2026-10-30":{"up":1,"down":2}}}]}]}`
	if _, err := decode([]byte(good)); err != nil {
		t.Fatalf("the sample is not valid: %v", err)
	}
	for name, doc := range map[string]string{
		"not json":          `not json`,
		"empty":             ``,
		"an array":          `[]`,
		"no version":        `{"nodes":[]}`,
		"version zero":      `{"version":0,"nodes":[]}`,
		"negative version":  `{"version":-3,"nodes":[]}`,
		"truncated":         good[:len(good)/2],
		"node id zero":      `{"version":1,"nodes":[{"id":0,"days":{},"subscriptions":[]}]}`,
		"node id repeated":  `{"version":1,"nodes":[{"id":1,"days":{},"subscriptions":[]},{"id":1,"days":{},"subscriptions":[]}]}`,
		"negative mark":     `{"version":1,"nodes":[{"id":1,"acked_through":-1,"days":{},"subscriptions":[]}]}`,
		"bad day":           `{"version":1,"nodes":[{"id":1,"days":{"2026-13-40":{"up":1,"down":1}},"subscriptions":[]}]}`,
		"day as a number":   `{"version":1,"nodes":[{"id":1,"days":{"20261030":{"up":1,"down":1}},"subscriptions":[]}]}`,
		"negative bytes":    `{"version":1,"nodes":[{"id":1,"days":{"2026-10-30":{"up":-1,"down":1}},"subscriptions":[]}]}`,
		"fractional bytes":  `{"version":1,"nodes":[{"id":1,"days":{"2026-10-30":{"up":1.5,"down":1}},"subscriptions":[]}]}`,
		"sub id zero":       `{"version":1,"nodes":[{"id":1,"days":{},"subscriptions":[{"id":0,"days":{}}]}]}`,
		"sub id repeated":   `{"version":1,"nodes":[{"id":1,"days":{},"subscriptions":[{"id":3,"days":{}},{"id":3,"days":{}}]}]}`,
		"bad day in a sub":  `{"version":1,"nodes":[{"id":1,"days":{},"subscriptions":[{"id":3,"days":{"soon":{"up":1,"down":1}}}]}]}`,
		"nodes not a list":  `{"version":1,"nodes":{"1":{}}}`,
		"version as string": `{"version":"1","nodes":[]}`,
	} {
		_, err := decode([]byte(doc))
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if errors.Is(err, errNewer) {
			t.Errorf("%s: taken for a newer file: %v", name, err)
		}
	}
}

func TestANewerFileIsRecognisedWhateverElseIsInIt(t *testing.T) {
	for _, doc := range []string{
		`{"version":2,"nodes":[]}`,
		`{"version":99,"nodes":"something this build has never seen","extra":{"a":[1,2,3]}}`,
		`{"version":2}`,
	} {
		_, err := decode([]byte(doc))
		if !errors.Is(err, errNewer) {
			t.Errorf("%s: err = %v", doc, err)
		}
	}
}

func TestDecodeIgnoresFieldsItDoesNotKnow(t *testing.T) {
	doc := `{"version":1,"added_later":true,"nodes":[{"id":1,"acked_through":3,"colour":"red","days":{},"subscriptions":[]}]}`
	h, err := decode([]byte(doc))
	must(t, err)
	if h[1] == nil || h[1].through != 3 {
		t.Fatalf("decoded %+v", h)
	}
}
