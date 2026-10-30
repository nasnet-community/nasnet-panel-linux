// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSaysItIsNotImplemented(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("a config the hub would send")
	if code := run(in, &out); code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
	if !strings.Contains(out.String(), "not implemented yet") {
		t.Fatalf("stderr says %q", out.String())
	}
	if in.Len() != 0 {
		t.Fatalf("%d bytes of stdin were left unread", in.Len())
	}
}

func TestNoStdinIsFine(t *testing.T) {
	var out bytes.Buffer
	if code := run(nil, &out); code != 1 || !strings.Contains(out.String(), "not implemented yet") {
		t.Fatalf("exit code %d, stderr %q", code, out.String())
	}
}
