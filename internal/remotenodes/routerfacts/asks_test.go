// SPDX-License-Identifier: AGPL-3.0-or-later

package routerfacts

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readAsk(t *testing.T, dataDir, name string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dataDir, "router", name))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

// The files sit beside the script's report, one line each, and go away when
// there is nothing to ask.
func TestAsksLeaveTheScriptOneLineFiles(t *testing.T) {
	dir := t.TempDir() // no router folder yet: the first ask makes it
	a := NewAsks(Path(dir))
	if filepath.Dir(Path(dir)) != filepath.Join(dir, "router") {
		t.Fatalf("the report is at %s", Path(dir))
	}

	if err := a.SetLocal(true); err != nil {
		t.Fatal(err)
	}
	if got, ok := readAsk(t, dir, "local"); !ok || got != "on\n" {
		t.Fatalf("local: %q %v", got, ok)
	}
	if err := a.SetHost("router.example.com"); err != nil {
		t.Fatal(err)
	}
	if got, ok := readAsk(t, dir, "host"); !ok || got != "router.example.com\n" {
		t.Fatalf("host: %q %v", got, ok)
	}
	if err := a.NewPort(); err != nil {
		t.Fatal(err)
	}
	if got, ok := readAsk(t, dir, "request"); !ok || got != "new-port\n" {
		t.Fatalf("request: %q %v", got, ok)
	}

	if err := a.SetLocal(false); err != nil {
		t.Fatal(err)
	}
	if _, ok := readAsk(t, dir, "local"); ok {
		t.Error("local is still there with the entry off")
	}
	if err := a.SetHost(""); err != nil {
		t.Fatal(err)
	}
	if _, ok := readAsk(t, dir, "host"); ok {
		t.Error("host is still there with no name set")
	}
	// taking away what isn't there is no error
	if err := a.SetLocal(false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetHost("two\nlines"); err == nil {
		t.Error("a name with a line break was written")
	}

	// nothing is left behind but the files themselves
	entries, err := os.ReadDir(filepath.Join(dir, "router"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "request" {
		t.Errorf("the folder holds %v", entries)
	}
}

// Saying the same again writes nothing: the folder is on the router's flash.
func TestAsksWriteOnlyWhatChanged(t *testing.T) {
	dir := t.TempDir()
	a := NewAsks(Path(dir))
	if err := a.SetLocal(true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "router", "local")
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := a.SetLocal(true); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(old) {
		t.Fatal("the same ask was written again")
	}

	// the script takes a request away once it has acted; the next one is written anew
	if err := a.NewPort(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "router", "request")); err != nil {
		t.Fatal(err)
	}
	if err := a.NewPort(); err != nil {
		t.Fatal(err)
	}
	if _, ok := readAsk(t, dir, "request"); !ok {
		t.Fatal("a second request after the script took the first was not written")
	}
}
