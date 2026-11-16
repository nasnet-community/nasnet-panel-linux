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
	if len(os.Args) > 1 && os.Args[1] == bridgespec.ProbeArg {
		return fakeProbe(mode)
	}
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

// fakeProbe plays the bridge program asked to try one outbound: it reads the
// request and answers in the mode's way. BRIDGECTL_PROBE_LOG, when set, names
// a file it notes its pid and when it ran in, for the tests that look at how
// probes are run.
func fakeProbe(mode string) int {
	var req bridgespec.ProbeRequest
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fmt.Fprintln(os.Stderr, "fake probe: reading the request:", err)
		return 2
	}
	note := func(what string) {
		if path := os.Getenv("BRIDGECTL_PROBE_LOG"); path != "" {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err == nil {
				fmt.Fprintf(f, "%s %d %d\n", what, os.Getpid(), time.Now().UnixNano())
				f.Close()
			}
		}
	}
	note("start")
	defer note("end")
	answer := func(res bridgespec.ProbeResult) { json.NewEncoder(os.Stdout).Encode(res) }
	switch mode {
	case "probe-ok":
		if req.Connect {
			answer(bridgespec.ProbeResult{OK: true, DelayMS: 42, Status: 204})
		} else {
			answer(bridgespec.ProbeResult{OK: true})
		}
	case "probe-echo":
		// what arrived, said back in the error so the test can read it
		answer(bridgespec.ProbeResult{Error: fmt.Sprintf("tag=%q addr=%s port=%d from=%q connect=%v url=%q timeout=%d memlimit=%s",
			req.Outbound.Tag, req.Outbound.Addr, req.Outbound.Port, req.From, req.Connect, req.URL, req.TimeoutSeconds, os.Getenv("GOMEMLIMIT"))})
	case "probe-refuse":
		fmt.Println("2026/11/18 10:00:00 [Warning] a log line before the result")
		answer(bridgespec.ProbeResult{Error: "Xray refuses the outbound: invalid user id"})
		return 1
	case "probe-garbage":
		fmt.Println("not the result")
	case "probe-crash":
		fmt.Fprintln(os.Stderr, "starting\npanic: boom")
		return 3
	case "probe-slow":
		time.Sleep(200 * time.Millisecond)
		answer(bridgespec.ProbeResult{OK: true})
	case "probe-hang":
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Hour)
	default:
		fmt.Fprintln(os.Stderr, "fake probe: no such mode", mode)
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
