// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg_test

import (
	"encoding/json"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

const (
	// 32 bytes, 0x01 to 0x20: a fixture, not a secret.
	testKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA"

	uuidSub1   = "2f8e3f0b-3a38-4d27-9b0e-5c1d6a7e8f01"
	uuidSub2   = "9c4b7a52-61de-4f3a-a8c7-0d2e1b3f4a65"
	uuidBridge = "d0b6f1c8-7e25-4a9d-b4f3-8a1c2e5d6b70"
)

// sample is a valid input with a short domestic list, so the golden file
// stays readable. The default list has its own test.
func sample() nodecfg.Input {
	return nodecfg.Input{
		EntryPort:   8443,
		RealityDest: "www.example.com:443",
		ServerNames: []string{"www.example.com", "example.com"},
		PrivateKey:  testKey,
		ShortIDs:    []string{"a1b2c3d4e5f60718", "0f1e"},
		Clients: []nodecfg.Client{
			{UUID: uuidSub1, Email: nodecfg.SubscriptionEmail(1)},
			{UUID: uuidSub2, Email: nodecfg.SubscriptionEmail(2)},
		},
		Bridge: nodecfg.Bridge{
			UUID:       uuidBridge,
			Email:      nodecfg.BridgeEmail(7),
			ReverseTag: "rvs-to-node-7",
		},
		DomesticCIDRs: []string{"5.22.0.0/17", "2.176.0.0/12", "2a01:5ec0::/32"},
	}
}

// decode builds in and returns the config as generic JSON.
func decode(t *testing.T, in nodecfg.Input) map[string]any {
	t.Helper()
	raw, err := nodecfg.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Build output isn't JSON: %v", err)
	}
	return out
}

// at digs a value out of generic JSON by key and index.
func at(t *testing.T, v any, path ...any) any {
	t.Helper()
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("%v: not an object at %q", path, k)
			}
			if v, ok = m[k]; !ok {
				t.Fatalf("%v: no key %q", path, k)
			}
		case int:
			s, ok := v.([]any)
			if !ok || k >= len(s) {
				t.Fatalf("%v: no element %d", path, k)
			}
			v = s[k]
		}
	}
	return v
}

func toStrings(t *testing.T, v any) []string {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("%v is not a list", v)
	}
	out := make([]string, len(list))
	for i, e := range list {
		if out[i], ok = e.(string); !ok {
			t.Fatalf("element %d of %v is not a string", i, list)
		}
	}
	return out
}
