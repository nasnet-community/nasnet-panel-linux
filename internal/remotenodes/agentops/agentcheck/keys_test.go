// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"encoding/json"
	"testing"

	"github.com/xtls/xray-core/infra/conf"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

// The keys the hub makes end up in a config that a node's Xray reads. This
// loads that config the way the Xray binary does, with its own loader, so a key
// in a form Xray doesn't take fails here and not when a node restarts.
func TestXrayLoadsTheConfigBuiltFromKeysTheHubMade(t *testing.T) {
	build := func(k agentops.RealityKeys) ([]byte, error) {
		return nodecfg.Build(nodecfg.Input{
			EntryPort: 443, RealityDest: "www.example.com:443", ServerNames: []string{"www.example.com"},
			PrivateKey: k.PrivateKey, ShortIDs: []string{k.ShortID},
			Bridge: nodecfg.Bridge{UUID: "11111111-2222-4333-8444-555555555555", Email: nodecfg.BridgeEmail(1), ReverseTag: "r1"},
		})
	}
	load := func(raw []byte) error {
		var parsed conf.Config
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return err
		}
		_, err := parsed.Build()
		return err
	}

	for range 20 {
		k, err := agentops.NewRealityKeys()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := build(k)
		if err != nil {
			t.Fatal(err)
		}
		if err := load(raw); err != nil {
			t.Fatalf("Xray refuses a config with the keys the hub made: %v", err)
		}
	}

	// the loader notices a key that isn't one, so passing it above shows something
	k, _ := agentops.NewRealityKeys()
	raw, err := build(k)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	json.Unmarshal(raw, &doc)
	reality := doc["inbounds"].([]any)[1].(map[string]any)["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
	reality["privateKey"] = k.PrivateKey[:20]
	broken, _ := json.Marshal(doc)
	if err := load(broken); err == nil {
		t.Error("Xray's loader took a private key that is too short, so this test shows nothing")
	}
}
