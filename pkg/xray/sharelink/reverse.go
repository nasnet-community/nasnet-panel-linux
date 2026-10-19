package sharelink

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/xtls/xray-core/infra/conf"
)

// ReverseBridgeOutbound turns the vless:// link a bridge dials into its
// outbound: tagged tag, in the flat settings shape Xray requires for reverse,
// with reverse.tag naming the inbound the tunnel's traffic arrives on. It
// carries no email: the bridge would count the tunnel's traffic under it as
// well, and only the portal's count is the tunnel's.
func ReverseBridgeOutbound(raw, tag, reverseTag string) (map[string]any, error) {
	uri, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || uri.Scheme != "vless" {
		return nil, fmt.Errorf("a reverse tunnel needs a vless:// link")
	}
	out, _, _, _, err := ManagedOutbound(raw)
	if err != nil {
		return nil, err
	}
	data, err := EncodeManaged(out)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err = json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	settings, _ := config["settings"].(map[string]any)
	if settings == nil {
		return nil, fmt.Errorf("the link has no VLESS settings")
	}
	flat := map[string]any{}
	if vnext, ok := settings["vnext"].([]any); ok && len(vnext) == 1 {
		server, _ := vnext[0].(map[string]any)
		users, _ := server["users"].([]any)
		if len(users) != 1 {
			return nil, fmt.Errorf("the link has no VLESS user")
		}
		user, _ := users[0].(map[string]any)
		for _, key := range []string{"id", "encryption", "flow"} {
			if v, ok := user[key]; ok && v != "" {
				flat[key] = v
			}
		}
		flat["address"], flat["port"] = server["address"], server["port"]
	} else {
		for _, key := range []string{"address", "port", "id", "encryption", "flow"} {
			if v, ok := settings[key]; ok && v != "" {
				flat[key] = v
			}
		}
	}
	if flat["id"] == nil || flat["address"] == nil {
		return nil, fmt.Errorf("the link has no VLESS server")
	}
	flat["reverse"] = map[string]any{"tag": reverseTag}
	config["settings"] = flat
	config["tag"] = tag
	delete(config, "mux")

	data, err = json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var check conf.OutboundDetourConfig
	if err = json.Unmarshal(data, &check); err != nil {
		return nil, err
	}
	if _, err = check.Build(); err != nil {
		return nil, fmt.Errorf("reverse outbound rejected by Xray: %w", err)
	}
	return config, nil
}
