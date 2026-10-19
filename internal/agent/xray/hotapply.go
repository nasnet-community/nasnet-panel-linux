package xray

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// HotPlan is a config change the running Xray can take through its API, so
// nobody's connection drops for it.
type HotPlan struct {
	Users     []HotInbound             // users to add, per inbound
	Outbounds []map[string]interface{} // tier outbounds to add
	Retired   []string                 // tier outbounds no longer referenced
	Routing   map[string]interface{}   // replacement rules and balancers, nil when unchanged
}

// HotInbound is an inbound's JSON cut down to the clients to add. Emails lists
// them; any already present (a level change, or a user the hub hot-added
// earlier) are removed first so the config's version is the one that counts.
type HotInbound struct {
	Tag     string
	Inbound map[string]interface{}
	Emails  []string
}

// Protocols whose users `xray api adu` can add, and the Xray release that
// first could.
var hotUserProtocols = map[string]string{"vless": "", "vmess": "", "trojan": "", "shadowsocks": "", "hysteria": "26.9.30"}

// hotRoutingSince is the first Xray that swaps rules atomically on adrules;
// older cores rebuild the rule list while connections are being routed.
const hotRoutingSince = "26.9.8"

// XrayAtLeast reports whether an `xray version` line (or a bare version)
// is min or newer. An unreadable version counts as older.
func XrayAtLeast(version, min string) bool {
	if min == "" {
		return true
	}
	fields := strings.Fields(version)
	if len(fields) > 1 && strings.EqualFold(fields[0], "xray") {
		version = fields[1]
	} else if len(fields) > 0 {
		version = fields[0]
	}
	have, want := strings.Split(strings.TrimPrefix(version, "v"), "."), strings.Split(min, ".")
	for i := range want {
		w, _ := strconv.Atoi(want[i])
		if i >= len(have) {
			return w == 0
		}
		h, err := strconv.Atoi(strings.SplitN(have[i], "-", 2)[0])
		if err != nil {
			return false
		}
		if h != w {
			return h > w
		}
	}
	return true
}

// PlanHotApply compares the running config with the next one. It returns a
// plan when the only differences are added users, users whose level changed,
// and the routing that bandwidth tiers derive from them; otherwise it returns
// the reason a restart is needed. Removed or changed users always restart:
// Xray's API stops their new connections but not the ones already open.
// xrayVersion is the running core's `xray version` line.
func PlanHotApply(running, next []byte, xrayVersion string) (*HotPlan, string) {
	var cur, nxt map[string]interface{}
	if json.Unmarshal(running, &cur) != nil || json.Unmarshal(next, &nxt) != nil {
		return nil, "config is not a JSON object"
	}
	for _, key := range unionKeys(cur, nxt) {
		switch key {
		case "inbounds", "outbounds", "routing":
		default:
			if !reflect.DeepEqual(cur[key], nxt[key]) {
				return nil, key + " changed"
			}
		}
	}

	plan := &HotPlan{}
	if reason := planUsers(plan, list(cur["inbounds"]), list(nxt["inbounds"]), xrayVersion); reason != "" {
		return nil, reason
	}
	if reason := planOutbounds(plan, list(cur["outbounds"]), list(nxt["outbounds"])); reason != "" {
		return nil, reason
	}
	if !reflect.DeepEqual(cur["routing"], nxt["routing"]) {
		curRouting, nxtRouting := object(cur["routing"]), object(nxt["routing"])
		for _, key := range unionKeys(curRouting, nxtRouting) {
			if key != "rules" && key != "balancers" && !reflect.DeepEqual(curRouting[key], nxtRouting[key]) {
				return nil, "routing " + key + " changed"
			}
		}
		if !XrayAtLeast(xrayVersion, hotRoutingSince) {
			return nil, "routing changed and Xray is older than " + hotRoutingSince
		}
		plan.Routing = nxtRouting
	}
	return plan, ""
}

func planUsers(plan *HotPlan, cur, nxt []interface{}, xrayVersion string) string {
	if len(cur) != len(nxt) {
		return "inbounds changed"
	}
	for i := range nxt {
		curIn, nxtIn := object(cur[i]), object(nxt[i])
		tag, _ := nxtIn["tag"].(string)
		if !reflect.DeepEqual(withoutClients(curIn), withoutClients(nxtIn)) {
			return fmt.Sprintf("inbound %q changed", tag)
		}
		curClients, nxtClients := clients(curIn), clients(nxtIn)
		if reflect.DeepEqual(curClients, nxtClients) {
			continue
		}
		protocol, _ := nxtIn["protocol"].(string)
		if since, ok := hotUserProtocols[protocol]; !ok || !XrayAtLeast(xrayVersion, since) {
			return fmt.Sprintf("users of %s inbound %q changed", protocol, tag)
		}
		before, ok := byEmail(curClients)
		after, ok2 := byEmail(nxtClients)
		if !ok || !ok2 {
			return fmt.Sprintf("inbound %q has a client without an email", tag)
		}
		for email := range before {
			if _, kept := after[email]; !kept {
				return "users removed"
			}
		}
		var add []interface{}
		var emails []string
		for _, c := range nxtClients {
			client := object(c)
			email := client["email"].(string)
			if old, existed := before[email]; existed {
				if reflect.DeepEqual(old, client) {
					continue
				}
				if !reflect.DeepEqual(without(old, "level"), without(client, "level")) {
					return fmt.Sprintf("user %s changed", email)
				}
			}
			add = append(add, client)
			emails = append(emails, email)
		}
		if len(add) == 0 {
			continue
		}
		inbound := copyObject(nxtIn)
		settings := copyObject(object(inbound["settings"]))
		settings["clients"] = add
		inbound["settings"] = settings
		plan.Users = append(plan.Users, HotInbound{Tag: tag, Inbound: inbound, Emails: emails})
	}
	return ""
}

// planOutbounds allows only bandwidth tier copies to come and go. The first
// outbound is the default one and can't be replaced through the API.
func planOutbounds(plan *HotPlan, cur, nxt []interface{}) string {
	if len(cur) == 0 || len(nxt) == 0 || !reflect.DeepEqual(cur[0], nxt[0]) {
		return "default outbound changed"
	}
	before, after := map[string]interface{}{}, map[string]interface{}{}
	for _, out := range cur {
		tag, _ := object(out)["tag"].(string)
		before[tag] = out
	}
	for _, out := range nxt {
		tag, _ := object(out)["tag"].(string)
		after[tag] = out
	}
	for tag := range before {
		if _, kept := after[tag]; kept {
			continue
		}
		if !isTierTag(tag) {
			return fmt.Sprintf("outbound %q removed", tag)
		}
		plan.Retired = append(plan.Retired, tag)
	}
	for _, out := range nxt {
		tag, _ := object(out)["tag"].(string)
		old, existed := before[tag]
		switch {
		case existed && !reflect.DeepEqual(old, out):
			return fmt.Sprintf("outbound %q changed", tag)
		case existed:
		case !isTierTag(tag):
			return fmt.Sprintf("outbound %q added", tag)
		default:
			plan.Outbounds = append(plan.Outbounds, object(out))
		}
	}
	return ""
}

// isTierTag recognizes the handlers the hub generates for bandwidth tiers
// (pkg/xray bandwidthNamespace).
func isTierTag(tag string) bool {
	return strings.Contains(tag, "panel_bw/")
}

func clients(inbound map[string]interface{}) []interface{} {
	return list(object(inbound["settings"])["clients"])
}

func withoutClients(inbound map[string]interface{}) map[string]interface{} {
	out := copyObject(inbound)
	out["settings"] = without(object(inbound["settings"]), "clients")
	return out
}

func byEmail(list []interface{}) (map[string]map[string]interface{}, bool) {
	out := make(map[string]map[string]interface{}, len(list))
	for _, c := range list {
		client := object(c)
		email, _ := client["email"].(string)
		if email == "" {
			return nil, false
		}
		out[email] = client
	}
	return out, true
}

func without(m map[string]interface{}, key string) map[string]interface{} {
	out := copyObject(m)
	delete(out, key)
	return out
}

func copyObject(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func object(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	if m == nil {
		return map[string]interface{}{}
	}
	return m
}

func list(v interface{}) []interface{} {
	l, _ := v.([]interface{})
	return l
}

func unionKeys(a, b map[string]interface{}) []string {
	var keys []string
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	return keys
}

// CLI runs `xray api` commands with the binary and assets Xray runs with.
type CLI interface {
	API(ctx context.Context, args ...string) (string, error)
	ConfigPath() string
}

// Apply carries the plan out against the running Xray at addr. Tier outbounds
// exist before rules point at them and are retired only after, and users are
// in place before the rules that name them.
func (p *HotPlan) Apply(ctx context.Context, cli CLI, client *LocalClient, addr string) error {
	if len(p.Outbounds) > 0 {
		if _, err := apiWithFile(ctx, cli, "ado", addr, map[string]interface{}{"outbounds": p.Outbounds}); err != nil {
			return err
		}
	}
	for _, in := range p.Users {
		// Xray won't add an email it already has. A level change, or a user
		// the hub hot-added in the meantime, is re-added from the config.
		for _, email := range in.Emails {
			_ = client.RemoveUser(ctx, in.Tag, email)
		}
		out, err := apiWithFile(ctx, cli, "adu", addr, map[string]interface{}{"inbounds": []interface{}{in.Inbound}})
		if err != nil {
			return err
		}
		if !strings.Contains(out, fmt.Sprintf("Added %d user(s)", len(in.Emails))) {
			return fmt.Errorf("adding %d user(s) to %s: %s", len(in.Emails), in.Tag, strings.TrimSpace(out))
		}
	}
	if p.Routing != nil {
		if _, err := apiWithFile(ctx, cli, "adrules", addr, map[string]interface{}{"routing": p.Routing}); err != nil {
			return err
		}
	}
	for _, tag := range p.Retired {
		if err := client.RemoveOutbound(ctx, tag); err != nil {
			return err
		}
	}
	return nil
}

// apiWithFile runs an `xray api` command that reads its input from a config
// file. Building rules can load geodata, hence the generous timeout.
func apiWithFile(ctx context.Context, cli CLI, command, addr string, content interface{}) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(cli.ConfigPath()), "xray-api-*.json")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if err := json.NewEncoder(f).Encode(content); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return cli.API(ctx, command, "-s", addr, "-t", "30", f.Name())
}
