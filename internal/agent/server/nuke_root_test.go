package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealRoot_RemoveAllPath(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "payload")
	if err := os.WriteFile(target, []byte("secrets"), 0644); err != nil {
		t.Fatal(err)
	}

	root := NewRootAt(base)
	bytes, files, err := root.RemoveAllPath("/payload")
	if err != nil {
		t.Fatalf("RemoveAllPath: %v", err)
	}
	if files != 1 {
		t.Fatalf("expected 1 file removed, got %d", files)
	}
	if bytes != int64(len("secrets")) {
		t.Fatalf("expected %d bytes, got %d", len("secrets"), bytes)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected target to be removed, stat err = %v", err)
	}
}

func TestRealRoot_DryRunCountsWithoutMutating(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "payload")
	if err := os.WriteFile(target, []byte("abcd"), 0644); err != nil {
		t.Fatal(err)
	}

	root := NewRootAt(base).WithDryRun(true)
	bytes, files, err := root.RemoveAllPath("/payload")
	if err != nil {
		t.Fatalf("dry-run RemoveAllPath: %v", err)
	}
	if files != 1 || bytes != 4 {
		t.Fatalf("expected 1 file / 4 bytes, got %d / %d", files, bytes)
	}

	if _, err := os.Stat(target); err != nil {
		t.Fatalf("dry-run must not mutate, stat err = %v", err)
	}
}

func TestRealRoot_RemoveGlobPath_RemovesAllMatches(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nasnet-a", "nasnet-b", "other"} {
		if err := os.WriteFile(filepath.Join(base, "tmp", name), []byte("xy"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	root := NewRootAt(base)
	bytes, files, err := root.RemoveGlobPath("/tmp/nasnet-*")
	if err != nil {
		t.Fatalf("RemoveGlobPath: %v", err)
	}
	if files != 2 || bytes != 4 {
		t.Fatalf("expected 2 files / 4 bytes, got %d / %d", files, bytes)
	}
	if _, err := os.Stat(filepath.Join(base, "tmp/other")); err != nil {
		t.Fatalf("non-matching file must remain, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "tmp/nasnet-a")); !os.IsNotExist(err) {
		t.Fatalf("nasnet-a must be removed, err=%v", err)
	}
}

func TestRealRoot_ShredGlobPath_RemovesMatches(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "etc/nasnet-agent/tls")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ca.crt", "client.key"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("PEM"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root := NewRootAt(base)
	bytes, files, err := root.ShredGlobPath("/etc/nasnet-agent/tls/*")
	if err != nil {
		t.Fatalf("ShredGlobPath: %v", err)
	}
	if files != 2 || bytes != 6 {
		t.Fatalf("expected 2 files / 6 bytes, got %d / %d", files, bytes)
	}
	// Parent dir must still exist (we shredded contents, not the dir)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("parent dir must still exist, got: %v", err)
	}
}
