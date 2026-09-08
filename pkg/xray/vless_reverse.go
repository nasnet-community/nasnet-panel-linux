package xray

import "slices"

func (b *FullConfigBuilder) applyVLESSReverse(c *OrderedConfig) {
	portals := make(map[string]string)
	for _, rp := range b.reverseProxies {
		if rp.Type == "bridge" {
			for _, out := range c.Outbounds {
				if out["tag"] != rp.InterconnectionTag || out["protocol"] != "vless" {
					continue
				}
				// VLESS Reverse requires the simplified outbound settings shape.
				settings := out["settings"].(map[string]interface{})
				server := settings["vnext"].([]map[string]interface{})[0]
				user := deepCopyMap(server["users"].([]map[string]interface{})[0])
				user["address"], user["port"] = server["address"], server["port"]
				user["reverse"] = map[string]interface{}{"tag": rp.Tag}
				out["settings"] = user
			}
		} else if rp.Type == "portal" {
			for _, in := range c.Inbounds {
				tag, _ := in["tag"].(string)
				if in["protocol"] != "vless" || !slices.Contains(rp.InterconnectionTags, tag) {
					continue
				}
				portals[tag] = rp.Tag
				settings := in["settings"].(map[string]interface{})
				for _, client := range settings["clients"].([]interface{}) {
					client.(map[string]interface{})["reverse"] = map[string]interface{}{"tag": rp.Tag}
				}
			}
		}
	}
	if len(portals) > 0 {
		// Agent metadata also applies this permission to users added through
		// the live API, including when the inbound initially has no clients.
		c.XCoreHub = map[string]interface{}{"reversePortals": portals}
	}
}
