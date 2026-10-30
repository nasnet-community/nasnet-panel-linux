// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// realityKeys makes a REALITY key pair, the way a node's agent would.
func realityKeys(t testing.TB) (private, public string) {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(key.Bytes()), enc.EncodeToString(key.PublicKey().Bytes())
}

// testSpec is a spec for the builder and parity tests, on the addresses the
// router would give the bridge: two nodes, the default blocked list.
func testSpec() *bridgespec.Spec {
	pub := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	node := func(i int, addr string) bridgespec.Node {
		return bridgespec.Node{
			Tag:        fmt.Sprintf("rvs-link-%d", i),
			Addr:       addr,
			Port:       uint16(443 + i),
			UUID:       fmt.Sprintf("7f8b1a9e-3c2d-4b5a-9e6f-0a1b2c3d4e5%d", i),
			PublicKey:  pub,
			ShortID:    "0123456789abcdef",
			ServerName: "www.example.com",
			Flow:       bridgespec.VisionFlow,
			ReverseTag: fmt.Sprintf("rvs-from-n%d", i),
		}
	}
	return &bridgespec.Spec{
		Version:    bridgespec.Version,
		Nodes:      []bridgespec.Node{node(1, "203.0.113.10"), node(2, "198.51.100.20")},
		DialAddr:   "192.168.50.18",
		Exit:       bridgespec.Exit{Addr: "192.168.50.17"},
		DNS:        bridgespec.DNS{Servers: []string{"1.1.1.1", "8.8.8.8:53"}},
		StatusAddr: "127.0.0.1:18080",
	}
}

// withoutLoopback is the default blocked list minus 127.0.0.0/8, for tests
// whose "internet" is a server on loopback. ::/127 stays, so ::1 is still out.
func withoutLoopback() []string {
	return slices.DeleteFunc(bridgespec.DefaultBlocked(), func(c string) bool { return c == "127.0.0.0/8" })
}

// freePort returns a loopback TCP port nothing holds right now.
func freePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// eventually polls cond until it holds or the time is up.
func eventually(t testing.TB, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", within, what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func newUUID() string { return uuid.NewString() }
