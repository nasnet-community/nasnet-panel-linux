// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
)

func stateOf(t *testing.T, dir string) model.State {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, store.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var st model.State
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// A hub on a fresh volume takes the backup of another. Its nodes, subscriptions,
// links and CA are the other's, what the page says of the last backup is too,
// and the file that did it holds none of it in the clear.
func TestABackupMovesAHubOntoAFreshVolume(t *testing.T) {
	const pass = "correct horse battery staple"
	base := "/api/plugin/view/remote-nodes"
	dirA := filepath.Join(t.TempDir(), "data")
	seedNode(t, dirA)
	addrA, stopA := start(t, testSettings(t, dirA, base))
	rootA := "http://" + addrA + base

	code, _, b := callHub(t, "POST", rootA+"/api/subscriptions", `{"name":"علی","nodeIds":[1]}`)
	if code != http.StatusCreated {
		t.Fatalf("creating a subscription = %d %s", code, b)
	}
	_, _, b = callHub(t, "GET", rootA+"/api/subscriptions/1/nodes/1/link", "")
	link, _ := jsonOf(t, b)["vless"].(string)
	id, _, _ := linkParts(t, link)

	// before any backup the page has none to show
	_, _, b = callHub(t, "GET", rootA+"/api/config", "")
	if got := jsonOf(t, b)["backup"].(map[string]any)["lastExportAt"]; got != nil {
		t.Errorf("a backup before any was made: %v", got)
	}
	// a passphrase that is too short makes no file and no record of one
	code, _, b = callHub(t, "POST", rootA+"/api/backup/export", `{"passphrase":"short"}`)
	if got := jsonOf(t, b); code != http.StatusBadRequest || got["code"] != "bad_request" || !strings.Contains(got["error"].(string), "8 characters") {
		t.Errorf("a short passphrase = %d %s", code, b)
	}
	_, _, b = callHub(t, "GET", rootA+"/api/config", "")
	if got := jsonOf(t, b)["backup"].(map[string]any)["lastExportAt"]; got != nil {
		t.Errorf("a refused export was recorded: %v", got)
	}

	code, hdr, file := callHub(t, "POST", rootA+"/api/backup/export", `{"passphrase":"`+pass+`"}`)
	if code != http.StatusOK || hdr.Get("Content-Type") != "application/octet-stream" || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("export = %d %q, Cache-Control %q", code, hdr.Get("Content-Type"), hdr.Get("Cache-Control"))
	}
	kind, params, err := mime.ParseMediaType(hdr.Get("Content-Disposition"))
	if err != nil || kind != "attachment" || !regexp.MustCompile(`^remote-nodes-backup-\d{4}-\d{2}-\d{2}\.rnbackup$`).MatchString(params["filename"]) {
		t.Errorf("Content-Disposition = %q (%v)", hdr.Get("Content-Disposition"), err)
	}
	if !bytes.HasPrefix(file, []byte("RNBACKUP")) {
		t.Errorf("the file starts %q", file[:min(len(file), 12)])
	}
	stateA := stateOf(t, dirA)
	for _, secret := range []string{id, seedKey, seedHostPin, "BEGIN EC PRIVATE KEY", pemBodyOf(stateA.CAKey), pemBodyOf(stateA.CACert), "Tehran", "PUBLIC-KEY-OF-THE-NODE"} {
		if bytes.Contains(file, []byte(secret)) {
			t.Errorf("the backup file holds %q in the clear", secret)
		}
	}
	// the page now shows when, and the state has it too
	_, _, b = callHub(t, "GET", rootA+"/api/config", "")
	made, _ := jsonOf(t, b)["backup"].(map[string]any)["lastExportAt"].(string)
	if at, err := time.Parse(time.RFC3339, made); err != nil || time.Since(at) > time.Minute || !strings.HasSuffix(made, "Z") {
		t.Errorf("lastExportAt = %q (%v)", made, err)
	}
	if err := stopA(); err != nil {
		t.Fatalf("shutting down: %v", err)
	}

	// the other hub starts on nothing, with a CA of its own
	dirB := filepath.Join(t.TempDir(), "data")
	addrB, stopB := start(t, testSettings(t, dirB, base))
	rootB := "http://" + addrB + base
	if own := stateOf(t, dirB); own.CACert == "" || own.CACert == stateA.CACert {
		t.Fatal("the fresh hub did not make a CA of its own")
	}
	if _, _, b := callHub(t, "GET", rootB+"/api/nodes", ""); string(b) != "[]\n" {
		t.Fatalf("the fresh hub has nodes: %s", b)
	}

	imp := func(pass string, data []byte) (int, []byte) {
		code, _, b := callHub(t, "POST", rootB+"/api/backup/import", `{"passphrase":"`+pass+`","data":"`+base64.StdEncoding.EncodeToString(data)+`"}`)
		return code, b
	}
	// a wrong passphrase, a file that isn't one and one that was cut short change nothing
	if code, b := imp(pass+"!", file); code != http.StatusBadRequest || jsonOf(t, b)["code"] != "backup_bad_passphrase" {
		t.Errorf("a wrong passphrase = %d %s", code, b)
	}
	if code, b := imp(pass, []byte("this is some text, long enough to have a header, and it is not a backup")); code != http.StatusBadRequest || jsonOf(t, b)["code"] != "backup_bad_file" {
		t.Errorf("a file that isn't one = %d %s", code, b)
	}
	if code, b := imp(pass, file[:30]); code != http.StatusBadRequest || jsonOf(t, b)["code"] != "backup_bad_file" {
		t.Errorf("a file cut short = %d %s", code, b)
	}
	if _, _, b := callHub(t, "GET", rootB+"/api/nodes", ""); string(b) != "[]\n" {
		t.Fatalf("a refused import changed the hub: %s", b)
	}

	code, b = imp(pass, file)
	if got := jsonOf(t, b); code != http.StatusOK || got["ok"] != true || got["nodes"] != 1.0 || len(got) != 2 {
		t.Fatalf("the import = %d %s", code, b)
	}

	// the node, the subscription and the link are the other hub's
	_, _, b = callHub(t, "GET", rootB+"/api/nodes", "")
	var nodes []map[string]any
	if err := json.Unmarshal(b, &nodes); err != nil || len(nodes) != 1 || nodes[0]["name"] != "Tehran" || nodes[0]["host"] != "203.0.113.5" {
		t.Errorf("nodes after the import: %s (%v)", b, err)
	}
	_, _, b = callHub(t, "GET", rootB+"/api/subscriptions", "")
	var subs []map[string]any
	if err := json.Unmarshal(b, &subs); err != nil || len(subs) != 1 || subs[0]["name"] != "علی" {
		t.Errorf("subscriptions after the import: %s (%v)", b, err)
	}
	_, _, b = callHub(t, "GET", rootB+"/api/subscriptions/1/nodes/1/link", "")
	if again, _ := jsonOf(t, b)["vless"].(string); again != link {
		t.Errorf("the link after the import is\n%s\nwas\n%s", again, link)
	}
	stateB := stateOf(t, dirB)
	if stateB.CACert != stateA.CACert || stateB.CAKey != stateA.CAKey {
		t.Error("the CA after the import is not the backup's")
	}
	_, _, b = callHub(t, "GET", rootB+"/api/config", "")
	if got, _ := jsonOf(t, b)["backup"].(map[string]any)["lastExportAt"].(string); got != made {
		t.Errorf("the restored hub says its last backup was %q, want %q", got, made)
	}

	// and now that it has nodes it takes no more
	if code, b := imp(pass, file); code != http.StatusBadRequest || jsonOf(t, b)["code"] != "bad_request" || !strings.Contains(string(b), "already has nodes") {
		t.Errorf("a second import = %d %s", code, b)
	}
	if err := stopB(); err != nil {
		t.Fatalf("shutting down: %v", err)
	}
}
