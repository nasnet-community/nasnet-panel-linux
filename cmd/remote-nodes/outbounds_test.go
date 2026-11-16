// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// call is a request from the page, with a body.
func call(t *testing.T, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("X-RemoteNodes-UI", "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var got map[string]any
	if len(b) > 0 {
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("%s %s = %d, not JSON: %s", method, url, resp.StatusCode, b)
		}
	}
	return resp.StatusCode, got
}

// The hub serves the outbounds with what it runs on. A machine without the
// bridge program keeps an outbound on the hub's own checks, and says a test
// can't be done here.
func TestTheHubKeepsOutboundsWithoutABridgeProgram(t *testing.T) {
	dir := t.TempDir()
	addr, _ := start(t, testSettings(t, filepath.Join(dir, "data"), "", "BRIDGE_BIN", filepath.Join(dir, "remote-nodes-bridge")))
	api := "http://" + addr + "/api/"
	settings := `{"protocol":"trojan","host":"198.51.100.9","port":443,"password":"a-password","security":"tls","serverName":"t.example.com"}`

	code, got := call(t, "POST", api+"outbounds", `{"name":"Frankfurt","settings":`+settings+`}`)
	if code != http.StatusCreated || got["id"] != 1.0 || got["network"] != "tcp" || got["secretHint"] != "" {
		t.Fatalf("created: %d %v", code, got)
	}
	if code, got = call(t, "POST", api+"outbounds/1/test", ""); code != http.StatusServiceUnavailable || got["code"] != "test_unavailable" {
		t.Errorf("a test with no program: %d %v", code, got)
	}
	if code, got = call(t, "GET", api+"outbounds", ""); code != http.StatusOK || got["limit"] != 16.0 || len(got["outbounds"].([]any)) != 1 {
		t.Errorf("the list: %d %v", code, got)
	}
	if code, got = call(t, "PUT", api+"local/route", `{"outboundId":1}`); code != http.StatusOK || got["outboundId"] != 1.0 {
		t.Errorf("the router through it: %d %v", code, got)
	}
	if code, got = call(t, "DELETE", api+"outbounds/1", ""); code != http.StatusConflict || got["code"] != "outbound_in_use" {
		t.Errorf("removing it while the router leaves through it: %d %v", code, got)
	}
	call(t, "PUT", api+"local/route", `{"outboundId":0}`)
	if code, _ = call(t, "DELETE", api+"outbounds/1", ""); code != http.StatusNoContent {
		t.Errorf("removing it: %d", code)
	}
}
