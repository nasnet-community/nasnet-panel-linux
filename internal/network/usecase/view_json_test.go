package usecase

import (
	"encoding/json"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// The TS type in web-panel/src/lib/types/network.ts reads these exact keys, and
// the embedded NetInterface would marshal as PascalCase without json tags.
func TestInterfaceView_JSONKeysMatchTheFrontend(t *testing.T) {
	b, err := json.Marshal(InterfaceView{
		ID:           7,
		NetInterface: agent.NetInterface{IfName: "enp1s0", Key: "k", SpeedMbit: 1000},
		Role:         "wan",
		Slot:         "domestic",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{
		"id", "if_name", "perm_mac", "id_path", "key", "key_kind", "source",
		"confidence", "driver", "carrier", "oper_state", "speed_mbit", "mtu",
		"usb_speed_mbit", "assignable", "addrs", "role", "slot", "label",
		"present", "healthy",
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q; got %v", k, keys(got))
		}
	}
	if got["id"] != float64(7) {
		t.Errorf("id = %v, want 7 — the UI needs it for ChangeRequest.InterfaceID", got["id"])
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
