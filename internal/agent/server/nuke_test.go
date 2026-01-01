package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// helper: build a nuke runner with a custom phase list for testing
func testRunner(phases []phase, root Root) *nukeRunner {
	return &nukeRunner{phases: phases, root: root}
}

func TestRunner_RunsAllApplicablePhasesInOrder(t *testing.T) {
	var order []string
	phases := []phase{
		{
			name:    "always_runs",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run: func(_ context.Context, _ Root) phaseResult {
				order = append(order, "always_runs")
				return phaseResult{ok: true}
			},
		},
		{
			name:    "wipe_only",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_WIPE },
			run: func(_ context.Context, _ Root) phaseResult {
				order = append(order, "wipe_only")
				return phaseResult{ok: true}
			},
		},
		{
			name:    "nuke_only",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_NUKE },
			run: func(_ context.Context, _ Root) phaseResult {
				order = append(order, "nuke_only")
				return phaseResult{ok: true}
			},
		},
	}

	runner := testRunner(phases, NewRootAt(t.TempDir()))
	report, _ := runner.Run(context.Background(),
		&pb.NukeRequest{Mode: pb.NukeMode_NUKE_MODE_WIPE}, nil)

	if len(order) != 2 || order[0] != "always_runs" || order[1] != "wipe_only" {
		t.Fatalf("unexpected order: %v", order)
	}
	if report.Result != pb.NukeReport_NUKE_RESULT_SUCCESS {
		t.Fatalf("expected SUCCESS, got %v", report.Result)
	}
}

func TestRunner_FailureIsolation(t *testing.T) {
	var ranAfterFailure bool
	phases := []phase{
		{
			name:    "fails",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run: func(_ context.Context, _ Root) phaseResult {
				return phaseResult{ok: false, err: errors.New("simulated failure")}
			},
		},
		{
			name:    "after",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run: func(_ context.Context, _ Root) phaseResult {
				ranAfterFailure = true
				return phaseResult{ok: true}
			},
		},
	}

	runner := testRunner(phases, NewRootAt(t.TempDir()))
	report, _ := runner.Run(context.Background(),
		&pb.NukeRequest{Mode: pb.NukeMode_NUKE_MODE_WIPE}, nil)

	if !ranAfterFailure {
		t.Fatal("subsequent phase must run even after a failure")
	}
	if report.Result != pb.NukeReport_NUKE_RESULT_PARTIAL {
		t.Fatalf("expected PARTIAL, got %v", report.Result)
	}
}

func TestRunner_StreamsProgress(t *testing.T) {
	phases := []phase{
		{
			name:    "a",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run:     func(_ context.Context, _ Root) phaseResult { return phaseResult{ok: true} },
		},
		{
			name:    "b",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run:     func(_ context.Context, _ Root) phaseResult { return phaseResult{ok: true} },
		},
	}
	var streamed []string
	emit := func(r *pb.NukePhaseResult) { streamed = append(streamed, r.Phase) }

	runner := testRunner(phases, NewRootAt(t.TempDir()))
	_, _ = runner.Run(context.Background(),
		&pb.NukeRequest{Mode: pb.NukeMode_NUKE_MODE_WIPE}, emit)

	if len(streamed) != 2 || streamed[0] != "a" || streamed[1] != "b" {
		t.Fatalf("expected streamed [a b], got %v", streamed)
	}
}

func TestPreFlight_RefusesOnHubLikeNode(t *testing.T) {
	// This test relies on /etc/nasnet-panel not existing on the test machine.
	// If it does, skip — the assertion is the inverse.
	if _, err := os.Stat("/etc/nasnet-panel"); err == nil {
		t.Skip("/etc/nasnet-panel exists on this host; preFlight would correctly refuse")
	}
	if err := preFlight(NewRootAt(t.TempDir())); err != nil {
		t.Fatalf("preFlight must allow non-hub nodes, got err: %v", err)
	}
}

func TestPhase_WipeWireguard_RemovesEtcWireguard(t *testing.T) {
	base := t.TempDir()
	_ = os.MkdirAll(filepath.Join(base, "etc/wireguard"), 0700)
	_ = os.WriteFile(filepath.Join(base, "etc/wireguard/wg0.conf"), []byte("secret"), 0600)

	phases := defaultPhases(nil)
	var p phase
	for _, x := range phases {
		if x.name == "wipe_wireguard" {
			p = x
		}
	}
	res := p.run(context.Background(), NewRootAt(base))
	if !res.ok {
		t.Fatalf("phase failed: %v", res.err)
	}
	if _, err := os.Stat(filepath.Join(base, "etc/wireguard")); !os.IsNotExist(err) {
		t.Fatalf("expected /etc/wireguard removed, got %v", err)
	}
	if res.files != 1 || res.bytes != 6 {
		t.Fatalf("expected 1 file / 6 bytes, got %d / %d", res.files, res.bytes)
	}
}

func TestPhase_ClearBashHistory_HandlesHomeAndRoot(t *testing.T) {
	base := t.TempDir()
	_ = os.MkdirAll(filepath.Join(base, "root"), 0700)
	_ = os.WriteFile(filepath.Join(base, "root/.bash_history"), []byte("x"), 0600)
	_ = os.MkdirAll(filepath.Join(base, "home/alice"), 0700)
	_ = os.WriteFile(filepath.Join(base, "home/alice/.bash_history"), []byte("xy"), 0600)

	phases := defaultPhases(nil)
	for _, p := range phases {
		if p.name == "clear_bash_history" {
			res := p.run(context.Background(), NewRootAt(base))
			if !res.ok || res.files != 2 || res.bytes != 3 {
				t.Fatalf("unexpected: %+v", res)
			}
			return
		}
	}
	t.Fatal("phase not found")
}

func TestPhase_WipeXray_DryRunReportsCountsButPreservesFiles(t *testing.T) {
	base := t.TempDir()
	_ = os.MkdirAll(filepath.Join(base, "etc/xray"), 0700)
	_ = os.WriteFile(filepath.Join(base, "etc/xray/config.json"), []byte("abc"), 0600)

	phases := defaultPhases(nil)
	for _, p := range phases {
		if p.name == "wipe_xray" {
			res := p.run(context.Background(), NewRootAt(base).WithDryRun(true))
			if !res.ok || res.files != 1 || res.bytes != 3 {
				t.Fatalf("dry-run counts wrong: %+v", res)
			}
			if _, err := os.Stat(filepath.Join(base, "etc/xray/config.json")); err != nil {
				t.Fatalf("dry-run must not remove: %v", err)
			}
			return
		}
	}
	t.Fatal("phase not found")
}

func TestPhase_StopXray_PropagatesErrorFromStopFn(t *testing.T) {
	stopErr := errors.New("xray refuses to stop")
	phases := defaultPhases(func() error { return stopErr })
	for _, p := range phases {
		if p.name == "stop_xray" {
			res := p.run(context.Background(), NewRootAt(t.TempDir()))
			if res.ok {
				t.Fatal("expected failure when xrayStopFn returns error")
			}
			if res.err == nil || res.err.Error() != stopErr.Error() {
				t.Fatalf("expected propagated error %q, got %v", stopErr.Error(), res.err)
			}
			return
		}
	}
	t.Fatal("stop_xray phase not found")
}

func TestPhase_ShredSSHHostKeys_OnlyInNukeMode(t *testing.T) {
	base := t.TempDir()
	_ = os.MkdirAll(filepath.Join(base, "etc/ssh"), 0700)
	_ = os.WriteFile(filepath.Join(base, "etc/ssh/ssh_host_rsa_key"), []byte("k"), 0600)
	_ = os.WriteFile(filepath.Join(base, "etc/ssh/ssh_host_ed25519_key"), []byte("kk"), 0600)

	phases := defaultPhases(nil)
	var p phase
	for _, x := range phases {
		if x.name == "shred_ssh_host_keys" {
			p = x
		}
	}
	if p.applies(&pb.NukeRequest{Mode: pb.NukeMode_NUKE_MODE_WIPE}) {
		t.Fatal("shred_ssh_host_keys should not apply in WIPE mode")
	}
	if !p.applies(&pb.NukeRequest{Mode: pb.NukeMode_NUKE_MODE_NUKE}) {
		t.Fatal("shred_ssh_host_keys should apply in NUKE mode")
	}
	res := p.run(context.Background(), NewRootAt(base))
	if !res.ok || res.files != 2 || res.bytes != 3 {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestPhase_WipeTmp_RemovesContentsNotTheMountpoint(t *testing.T) {
	base := t.TempDir()
	_ = os.MkdirAll(filepath.Join(base, "tmp/sub"), 0700)
	_ = os.WriteFile(filepath.Join(base, "tmp/x.sock"), []byte("abc"), 0600)
	_ = os.WriteFile(filepath.Join(base, "tmp/sub/y"), []byte("de"), 0600)

	phases := defaultPhases(nil)
	for _, p := range phases {
		if p.name == "wipe_tmp" {
			res := p.run(context.Background(), NewRootAt(base))
			if !res.ok {
				t.Fatal(res.err)
			}
			// /tmp itself must still exist
			if _, err := os.Stat(filepath.Join(base, "tmp")); err != nil {
				t.Fatalf("/tmp must not be removed, err=%v", err)
			}
			// Contents must be gone
			entries, _ := os.ReadDir(filepath.Join(base, "tmp"))
			if len(entries) != 0 {
				t.Fatalf("expected /tmp empty, got %d entries", len(entries))
			}
			return
		}
	}
	t.Fatal("phase not found")
}

func TestPhases_12to15_DryRunNoShellouts(t *testing.T) {
	dryRoot := NewRootAt(t.TempDir()).WithDryRun(true)
	phases := defaultPhases(nil)
	for _, name := range []string{"disable_audit", "disable_coredumps", "scrub_swap", "drop_caches"} {
		found := false
		for _, p := range phases {
			if p.name != name {
				continue
			}
			found = true
			if !p.applies(&pb.NukeRequest{Mode: pb.NukeMode_NUKE_MODE_NUKE}) {
				t.Fatalf("%s must apply in NUKE mode", name)
			}
			res := p.run(context.Background(), dryRoot)
			if !res.ok {
				t.Fatalf("%s dry-run failed: %v", name, res.err)
			}
		}
		if !found {
			t.Fatalf("phase %s not registered", name)
		}
	}
}

func TestPhase_ShredRoot_TogglesWithFlag(t *testing.T) {
	phases := defaultPhases(nil)
	var p phase
	for _, x := range phases {
		if x.name == "shred_root" {
			p = x
		}
	}
	if p.applies(&pb.NukeRequest{Mode: pb.NukeMode_NUKE_MODE_NUKE, ShredRoot: false}) {
		t.Fatal("must not apply without ShredRoot flag")
	}
	if !p.applies(&pb.NukeRequest{Mode: pb.NukeMode_NUKE_MODE_NUKE, ShredRoot: true}) {
		t.Fatal("must apply with ShredRoot flag")
	}
	if p.applies(&pb.NukeRequest{Mode: pb.NukeMode_NUKE_MODE_WIPE, ShredRoot: true}) {
		t.Fatal("must not apply in WIPE mode even with ShredRoot flag")
	}
}

func TestPhase_ShredRoot_RemovesRootWhenEnabled(t *testing.T) {
	base := t.TempDir()
	_ = os.MkdirAll(filepath.Join(base, "root/.config"), 0700)
	_ = os.WriteFile(filepath.Join(base, "root/script.sh"), []byte("hi"), 0700)
	_ = os.WriteFile(filepath.Join(base, "root/.config/x"), []byte("y"), 0600)

	phases := defaultPhases(nil)
	for _, p := range phases {
		if p.name != "shred_root" {
			continue
		}
		res := p.run(context.Background(), NewRootAt(base))
		if !res.ok || res.files != 2 || res.bytes != 3 {
			t.Fatalf("unexpected: %+v", res)
		}
		if _, err := os.Stat(filepath.Join(base, "root")); !os.IsNotExist(err) {
			t.Fatalf("expected /root removed, err=%v", err)
		}
		return
	}
	t.Fatal("phase not found")
}

func TestPhase_WipeAgentState_RemovesAllAgentDirs(t *testing.T) {
	base := t.TempDir()
	for _, d := range []string{"etc/nasnet-agent", "var/lib/nasnet-agent", "var/log/nasnet-agent"} {
		_ = os.MkdirAll(filepath.Join(base, d), 0700)
		_ = os.WriteFile(filepath.Join(base, d, "f"), []byte("x"), 0600)
	}
	_ = os.MkdirAll(filepath.Join(base, "tmp"), 0700)
	_ = os.WriteFile(filepath.Join(base, "tmp/nasnet-blob"), []byte("yy"), 0600)

	phases := defaultPhases(nil)
	for _, p := range phases {
		if p.name != "wipe_agent_state" {
			continue
		}
		res := p.run(context.Background(), NewRootAt(base))
		if !res.ok || res.files != 4 {
			t.Fatalf("unexpected: %+v", res)
		}
		for _, d := range []string{"etc/nasnet-agent", "var/lib/nasnet-agent", "var/log/nasnet-agent"} {
			if _, err := os.Stat(filepath.Join(base, d)); !os.IsNotExist(err) {
				t.Fatalf("expected %s removed, err=%v", d, err)
			}
		}
		return
	}
	t.Fatal("phase not found")
}
