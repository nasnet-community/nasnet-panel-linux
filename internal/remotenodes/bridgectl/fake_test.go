// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgectl

// The supervisor's tests run the test binary itself as the bridge: when
// BRIDGECTL_FAKE names a mode, TestMain plays that bridge and exits before any
// test starts. The modes are the ways a real one can go wrong or right.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("BRIDGECTL_FAKE"); mode != "" {
		os.Exit(fakeBridge(mode))
	}
	os.Exit(m.Run())
}

func fakeBridge(mode string) int {
	spec, err := bridgespec.Read(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake bridge: reading the spec:", err)
		return 2
	}
	tag := "no node"
	if len(spec.Nodes) > 0 {
		tag = spec.Nodes[0].Tag
	}
	switch {
	case mode == "serve":
		fmt.Println("fake bridge up:", tag)
		serveFakeStatus(spec)
		waitForStop()
		fmt.Println("fake bridge stopping")
	case mode == "noserve":
		fmt.Println("fake bridge up:", tag)
		waitForStop()
	case mode == "crash":
		fmt.Fprintln(os.Stderr, "boom")
		return 3
	case strings.HasPrefix(mode, "crash-after:"):
		d, _ := time.ParseDuration(strings.TrimPrefix(mode, "crash-after:"))
		fmt.Println("fake bridge up:", tag)
		time.Sleep(d)
		return 4
	case mode == "stubborn":
		// Neither the signal nor the closed stdin moves it.
		signal.Ignore(syscall.SIGTERM)
		fmt.Println("fake bridge up, and not leaving:", tag)
		time.Sleep(time.Hour)
	case mode == "spam":
		for i := 0; i < 50000; i++ {
			fmt.Printf("spam line %d\n", i)
		}
		fmt.Println("spam done")
		waitForStop()
	case mode == "env":
		for _, e := range os.Environ() {
			fmt.Println("env:", e)
		}
		waitForStop()
	case mode == "whoami":
		groups, _ := os.Getgroups()
		fmt.Printf("whoami: uid=%d gid=%d groups=%v\n", os.Getuid(), os.Getgid(), groups)
		waitForStop()
	default:
		fmt.Fprintln(os.Stderr, "fake bridge: no such mode", mode)
		return 2
	}
	return 0
}

// waitForStop returns on SIGTERM or when stdin closes: the two ways the real
// bridge is told to go.
func waitForStop() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	eof := make(chan struct{})
	go func() {
		io.Copy(io.Discard, os.Stdin)
		close(eof)
	}()
	select {
	case <-sig:
	case <-eof:
	}
}

func serveFakeStatus(spec *bridgespec.Spec) {
	now := time.Now()
	mux := http.NewServeMux()
	mux.HandleFunc(bridgespec.StatusPath, func(w http.ResponseWriter, r *http.Request) {
		nodes := "[]"
		if len(spec.Nodes) > 0 {
			nodes = fmt.Sprintf(`[{"tag":%q,"up":true,"since":%q,"bytes_up":7,"bytes_down":9,"connections":2}]`, spec.Nodes[0].Tag, now.Format(time.RFC3339))
		}
		fmt.Fprintf(w, `{"running":true,"started":%q,"xray":"fake","nodes":%s}`, now.Format(time.RFC3339), nodes)
	})
	if spec.Local != nil {
		// The entry's clients, as the real bridge takes them: the token, then
		// the set, answered with what changed. A client named "fail" is refused.
		have := map[string]bool{}
		for _, c := range spec.Local.Clients {
			have[c.Email] = true
		}
		mux.HandleFunc("PUT "+bridgespec.ClientsPath, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(bridgespec.TokenHeader) != spec.Local.Token {
				http.Error(w, "wrong or missing token", http.StatusForbidden)
				return
			}
			var req bridgespec.ClientsRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			res := bridgespec.ClientsResult{Added: []string{}, Removed: []string{}, Replaced: []string{}}
			want := map[string]bool{}
			for _, c := range req.Clients {
				if c.Email == "fail" {
					http.Error(w, "bad clients: clients[0].email: refused by the fake", http.StatusBadRequest)
					return
				}
				want[c.Email] = true
				if !have[c.Email] {
					res.Added = append(res.Added, c.Email)
				}
			}
			for email := range have {
				if !want[email] {
					res.Removed = append(res.Removed, email)
					res.Closed++
				}
			}
			have = want
			sort.Strings(res.Added)
			sort.Strings(res.Removed)
			json.NewEncoder(w).Encode(res)
		})
	}
	go http.ListenAndServe(spec.StatusAddr, mux)
}
