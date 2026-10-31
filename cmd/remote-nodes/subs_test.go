// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
)

// What the seeded hub holds that only a link or a backup may carry.
const (
	seedKey     = "SEED-REALITY-PRIVATE-KEY-MARKER"
	seedHostPin = "SHA256:SEED-HOST-KEY-MARKER"
)

// seedNode puts a node that is installed into a data folder before a hub starts
// on it, as an install would have left it. The hub can't reach it, since the
// address is in a range for documentation, which is what an unreachable node
// looks like.
func seedNode(t *testing.T, dir string) {
	t.Helper()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Update(func(s *model.State) error {
		_, err := s.AddNode(model.Node{
			Name: "Tehran", Host: "203.0.113.5", SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: model.NodeRegistered,
			CreatedAt: time.Now(), Arch: "arm64", AgentVersion: "1.3.0", XrayVersion: "26.10.21",
			RealityTarget: "www.example.com:443", RealitySNI: "www.example.com", RealityPublicKey: "PUBLIC-KEY-OF-THE-NODE",
			RealityShortID: "0a1b2c3d", RealityPrivateKey: seedKey, SSHHostKey: seedHostPin,
			BridgeUUID: "3d2c1b0a-9f8e-4d7c-8b6a-5f4e3d2c1b0a",
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// callHub makes a call the way the page does.
func callHub(t *testing.T, method, url, body string) (int, http.Header, []byte) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-RemoteNodes-UI", "1")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, b
}

func jsonOf(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, body)
	}
	return m
}

// linkParts takes a link apart the way a client does.
func linkParts(t *testing.T, vless string) (id, name string, q url.Values) {
	t.Helper()
	u, err := url.Parse(vless)
	if err != nil || u.Scheme != "vless" || u.User == nil {
		t.Fatalf("%q is not a vless link: %v", vless, err)
	}
	return u.User.Username(), u.Fragment, u.Query()
}

// The hub serves the subscription calls, over the real manager and the real
// store: a subscription is made, switched off and on, renamed, and removed, and
// the link keeps its client id through all of it but a rename of the label.
func TestTheHubServesTheSubscriptionCalls(t *testing.T) {
	for _, base := range []string{"", "/api/plugin/view/remote-nodes"} {
		dir := filepath.Join(t.TempDir(), "data")
		seedNode(t, dir)
		addr, stop := start(t, testSettings(t, dir, base))
		root := "http://" + addr + base
		bodies := map[string][]byte{}
		call := func(method, path, body string) (int, http.Header, []byte) {
			code, hdr, b := callHub(t, method, root+path, body)
			bodies[method+" "+path] = b
			return code, hdr, b
		}

		code, _, b := call("POST", "/api/subscriptions", `{"name":"علی","nodeIds":[1]}`)
		if code != http.StatusCreated {
			t.Fatalf("base %q: creating = %d %s", base, code, b)
		}
		v := jsonOf(t, b)
		nodes, _ := v["nodes"].([]any)
		if v["name"] != "علی" || v["id"] != 1.0 || len(nodes) != 1 || nodes[0].(map[string]any)["enabled"] != true {
			t.Errorf("created = %s", b)
		}

		code, hdr, b := call("GET", "/api/subscriptions/1/nodes/1/link", "")
		if code != http.StatusOK || hdr.Get("Cache-Control") != "no-store" {
			t.Fatalf("the link = %d, Cache-Control %q: %s", code, hdr.Get("Cache-Control"), b)
		}
		link := jsonOf(t, b)
		first, _ := link["vless"].(string)
		id, label, q := linkParts(t, first)
		if link["nodeId"] != 1.0 || link["nodeName"] != "Tehran" || label != "علی" || q.Get("pbk") != "PUBLIC-KEY-OF-THE-NODE" ||
			q.Get("sni") != "www.example.com" || !strings.Contains(first, "@203.0.113.5:443?") {
			t.Errorf("link = %s", b)
		}

		// off takes the link away, and on gives the very same one back
		if code, _, b := call("PUT", "/api/subscriptions/1/nodes/1", `{"enabled":false}`); code != http.StatusOK {
			t.Fatalf("switching off = %d %s", code, b)
		}
		code, _, b = call("GET", "/api/subscriptions/1/nodes/1/link", "")
		if got := jsonOf(t, b); code != http.StatusNotFound || got["code"] != "not_found" || !strings.Contains(got["error"].(string), "switched off") {
			t.Errorf("the link of a switch that is off = %d %s", code, b)
		}
		if code, _, b := call("PUT", "/api/subscriptions/1/nodes/1", `{"enabled":true}`); code != http.StatusOK {
			t.Fatalf("switching on = %d %s", code, b)
		}
		_, _, b = call("GET", "/api/subscriptions/1/nodes/1/link", "")
		if again, _ := jsonOf(t, b)["vless"].(string); again != first {
			t.Errorf("the link after off and on is\n%s\nwas\n%s", again, first)
		}

		// the node can't be reached, so a link is not replaced
		code, _, b = call("POST", "/api/subscriptions/1/nodes/1/replace-link", "")
		if got := jsonOf(t, b); code != http.StatusBadGateway || got["code"] != "node_unreachable" {
			t.Errorf("replacing the link of a node that can't be reached = %d %s", code, b)
		}
		_, _, b = call("GET", "/api/subscriptions/1/nodes/1/link", "")
		if again, _ := jsonOf(t, b)["vless"].(string); again != first {
			t.Error("the refused replacement changed the link")
		}

		// a rename changes the label and nothing else
		if code, _, b := call("PATCH", "/api/subscriptions/1", `{"name":"سارا"}`); code != http.StatusOK {
			t.Fatalf("renaming = %d %s", code, b)
		}
		_, _, b = call("GET", "/api/subscriptions/1/nodes/1/link", "")
		if renamed, _ := jsonOf(t, b)["vless"].(string); renamed == first {
			t.Error("the link kept the old label")
		} else if id2, label2, _ := linkParts(t, renamed); id2 != id || label2 != "سارا" {
			t.Errorf("renamed link: id %s label %s", id2, label2)
		}

		// a second subscription by the same name, in another case, is refused
		if code, _, b := call("POST", "/api/subscriptions", `{"name":"سارا","nodeIds":[]}`); code != http.StatusConflict || jsonOf(t, b)["code"] != "name_taken" {
			t.Errorf("a name that is taken = %d %s", code, b)
		}

		// the state keeps the client id, and nothing but the links shows it
		state, err := os.ReadFile(filepath.Join(dir, store.FileName))
		if err != nil || !bytes.Contains(state, []byte(id)) {
			t.Errorf("the state does not hold the client id: %v", err)
		}
		var held model.State
		if err := json.Unmarshal(state, &held); err != nil {
			t.Fatal(err)
		}
		secrets := []string{id, seedKey, seedHostPin, "3d2c1b0a-9f8e-4d7c-8b6a-5f4e3d2c1b0a", "BEGIN EC PRIVATE KEY", pemBodyOf(held.CAKey), pemBodyOf(held.CACert)}
		for _, path := range []string{"/api/config", "/api/nodes", "/api/nodes/1", "/api/nodes/1/subscriptions", "/api/subscriptions"} {
			_, _, bodies["GET "+path] = callHub(t, "GET", root+path, "")
		}
		for what, body := range bodies {
			if strings.HasSuffix(what, "/link") || strings.HasSuffix(what, "/replace-link") {
				continue
			}
			for _, s := range secrets {
				if s != "" && bytes.Contains(body, []byte(s)) {
					t.Errorf("base %q: %s carries %q", base, what, s)
				}
			}
		}

		if code, _, b := call("DELETE", "/api/subscriptions/1", ""); code != http.StatusOK || string(b) != `{"ok":true}`+"\n" {
			t.Errorf("deleting = %d %s", code, b)
		}
		if _, _, b := callHub(t, "GET", root+"/api/subscriptions", ""); string(b) != "[]\n" {
			t.Errorf("after the delete the list is %s", b)
		}
		if code, _, _ := call("GET", "/api/subscriptions/1/nodes/1/link", ""); code != http.StatusNotFound {
			t.Errorf("the link of a subscription that is gone = %d", code)
		}
		if err := stop(); err != nil {
			t.Fatalf("shutting down: %v", err)
		}
	}
}

// pemBodyOf is the base64 of a PEM block as one run of text, so that it is looked
// for whole and a header line on its own is not taken for it.
func pemBodyOf(pem string) string {
	var body []string
	for _, line := range strings.Split(pem, "\n") {
		if line != "" && !strings.HasPrefix(line, "-----") {
			body = append(body, line)
		}
	}
	return strings.Join(body, "")
}
