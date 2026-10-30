package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
	"github.com/spf13/cobra"
)

func TestIsNewerVersion(t *testing.T) {
	for _, c := range []struct {
		current, candidate string
		want               bool
	}{
		{"v1.2.0", "v1.3.0", true},
		{"1.2.0", "1.2.1", true},
		{"v1.2.0", "v2.0.0", true},
		{"1.2.0", "1.2.0", false},
		{"1.3.0", "1.2.9", false},
		{"2.0.0", "1.9.9", false},
		{"1.2.1", "1.2.1-rc1", false}, // a suffix doesn't count
		{"1.2.0", "1.2.1-rc1", true},
		// either side unreadable allows the update
		{"dev", "1.2.3", true},
		{"1.2.3", "dev", true},
		{"1.2", "1.3", true},
		{"v1.2.x", "1.2.3", true},
	} {
		if got := isNewerVersion(c.current, c.candidate); got != c.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", c.current, c.candidate, got, c.want)
		}
	}
}

func TestReportedVersion(t *testing.T) {
	for in, want := range map[string]string{
		"nasnet-agent version v1.3.0\n":                   "v1.3.0",
		"nasnet-agent version 1.3.0":                      "1.3.0",
		"\n\n  nasnet-agent version v1.3.0-rc1  \nmore\n": "v1.3.0-rc1",
		"v1.3.0":   "v1.3.0",
		"":         "",
		"  \n\t\n": "",
	} {
		if got := reportedVersion(in); got != want {
			t.Errorf("reportedVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// The real agent prints its version through cobra; if cobra's wording ever
// moves the version off the end of the first line, this fails before a managed
// agent starts refusing every update.
func TestReportedVersionReadsWhatCobraPrints(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{Use: "nasnet-agent", Version: "v1.3.0", Run: func(*cobra.Command, []string) {}}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := reportedVersion(out.String()); got != "v1.3.0" {
		t.Fatalf("read %q from %q", got, out.String())
	}
}

func TestRefuseOlderBuild(t *testing.T) {
	says := func(v string) string { return "nasnet-agent version " + v + "\n" }
	for _, c := range []struct {
		name, current, output string
		refused               bool
	}{
		{"older minor", "v1.3.0", says("v1.2.0"), true},
		{"older patch", "v1.3.1", says("v1.3.0"), true},
		{"older major", "v2.0.0", says("v1.9.9"), true},
		{"much older", "v1.3.0", says("v0.9.9"), true},
		{"same", "v1.3.0", says("v1.3.0"), false},
		{"same without the v", "v1.3.0", says("1.3.0"), false},
		{"same with a suffix", "v1.3.0", says("v1.3.0-rc1"), false},
		{"newer patch", "v1.3.0", says("v1.3.1"), false},
		{"newer minor", "v1.3.0", says("v1.4.0"), false},
		{"newer major", "v1.3.0", says("v2.0.0"), false},
		{"reports nothing", "v1.3.0", "", true},
		{"reports a name", "v1.3.0", says("dev"), true},
		{"reports a hash", "v1.3.0", says("3f2a9c1"), true},
		{"running a dev build", "dev", says("v0.0.1"), false},
		{"running a build with no number", "3f2a9c1", says("v0.0.1"), false},
	} {
		err := refuseOlderBuild(c.current, c.output)
		if (err != nil) != c.refused {
			t.Errorf("%s: refuseOlderBuild(%q, %q) = %v, refused want %v", c.name, c.current, c.output, err, c.refused)
		}
		if err != nil && !strings.Contains(err.Error(), "managed profile") {
			t.Errorf("%s: the message doesn't say why: %v", c.name, err)
		}
	}
}

// updateRig is a server that updates a stand-in for its own binary.
type updateRig struct {
	s       *Server
	current string // the stand-in "running" binary
	priv    ed25519.PrivateKey
}

func newUpdateRig(t *testing.T, profile config.Profile, running string) *updateRig {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(current, agentScript("running"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldVersion, oldPath, oldKeys := Version, selfPath, signing.PublicKeys
	Version = running
	selfPath = func() (string, error) { return current, nil }
	signing.PublicKeys = []ed25519.PublicKey{pub}
	t.Cleanup(func() { Version, selfPath, signing.PublicKeys = oldVersion, oldPath, oldKeys })

	return &updateRig{s: &Server{profile: profile}, current: current, priv: priv}
}

// agentScript is a build that says which version it is, as the agent does.
func agentScript(version string) []byte {
	return []byte("#!/bin/sh\necho 'nasnet-agent version " + version + "'\n")
}

// update sends a signed build, claiming claimed as its version.
func (r *updateRig) update(t *testing.T, build []byte, claimed string, force bool) *pb.UpdateResponse {
	t.Helper()
	resp, err := r.s.SelfUpdate(context.Background(), &pb.UpdateRequest{
		BinaryContent: build,
		Version:       claimed,
		Force:         force,
		Signature:     signing.Sign(build, r.priv),
	})
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (r *updateRig) untouched(t *testing.T) {
	t.Helper()
	got, err := os.ReadFile(r.current)
	if err != nil || !bytes.Equal(got, agentScript("running")) {
		t.Fatalf("the running binary changed: %q, %v", got, err)
	}
	if _, err := os.Stat(r.current + ".new"); !os.IsNotExist(err) {
		t.Fatalf("the half-installed build was left behind: %v", err)
	}
	if _, err := os.Stat(r.current + ".old"); !os.IsNotExist(err) {
		t.Fatalf("a backup appeared although nothing was replaced: %v", err)
	}
}

func (r *updateRig) replacedBy(t *testing.T, build []byte) {
	t.Helper()
	got, err := os.ReadFile(r.current)
	if err != nil || !bytes.Equal(got, build) {
		t.Fatalf("the running binary is %q, want %q (%v)", got, build, err)
	}
}

func TestManagedAgentWontGoBackToAnOlderBuild(t *testing.T) {
	older := agentScript("v1.2.0")
	for _, c := range []struct {
		name    string
		claimed string
		force   bool
		by      string // what the refusal says
	}{
		// the old check catches an honest, unforced downgrade by itself
		{"claimed honestly", "v1.2.0", false, "Downgrade rejected"},
		{"forced", "v1.2.0", true, "managed profile"},
		{"no version named", "", false, "managed profile"},
		{"no version named, forced", "", true, "managed profile"},
		{"claimed newer than it is", "v9.9.9", false, "managed profile"},
		{"claimed newer, forced", "v9.9.9", true, "managed profile"},
	} {
		r := newUpdateRig(t, config.ProfileManaged, "v1.3.0")
		resp := r.update(t, older, c.claimed, c.force)
		if resp.Success || !strings.Contains(resp.Message, c.by) {
			t.Errorf("%s: response = %+v", c.name, resp)
		}
		if c.by == "managed profile" && !strings.Contains(resp.Message, "older") {
			t.Errorf("%s: the message doesn't say why: %q", c.name, resp.Message)
		}
		if resp.OldVersion != "v1.3.0" {
			t.Errorf("%s: old version = %q", c.name, resp.OldVersion)
		}
		r.untouched(t)
	}
}

func TestManagedAgentTakesANewerBuild(t *testing.T) {
	newer := agentScript("v1.3.1")
	r := newUpdateRig(t, config.ProfileManaged, "v1.3.0")
	resp := r.update(t, newer, "v1.3.1", false)
	if !resp.Success || resp.NewVersion != "v1.3.1" {
		t.Fatalf("response = %+v", resp)
	}
	r.replacedBy(t, newer)
}

func TestManagedAgentRefusesABuildItCantReadAVersionFrom(t *testing.T) {
	r := newUpdateRig(t, config.ProfileManaged, "v1.3.0")
	resp := r.update(t, []byte("#!/bin/sh\necho hello\n"), "v1.4.0", false)
	if resp.Success || !strings.Contains(resp.Message, "managed profile") {
		t.Fatalf("response = %+v", resp)
	}
	r.untouched(t)
}

func TestManagedAgentStillWantsASignature(t *testing.T) {
	r := newUpdateRig(t, config.ProfileManaged, "v1.3.0")
	build := agentScript("v1.4.0")
	resp, err := r.s.SelfUpdate(context.Background(), &pb.UpdateRequest{BinaryContent: build, Version: "v1.4.0"})
	if err != nil || resp.Success || !strings.Contains(resp.Message, "signature") {
		t.Fatalf("unsigned: %+v, %v", resp, err)
	}
	r.untouched(t)
}

// The full profile keeps today's rules, force included.
func TestFullAgentUpdatesAsBefore(t *testing.T) {
	older := agentScript("v1.2.0")

	r := newUpdateRig(t, config.ProfileFull, "v1.3.0")
	resp := r.update(t, older, "v1.2.0", true)
	if !resp.Success {
		t.Fatalf("forced downgrade: %+v", resp)
	}
	r.replacedBy(t, older)

	r = newUpdateRig(t, config.ProfileFull, "v1.3.0")
	resp = r.update(t, older, "v1.2.0", false)
	if resp.Success || !strings.Contains(resp.Message, "Downgrade rejected") {
		t.Fatalf("unforced downgrade: %+v", resp)
	}
	r.untouched(t)

	// and an agent with no profile line at all is the same
	r = newUpdateRig(t, "", "v1.3.0")
	if resp := r.update(t, older, "", false); !resp.Success {
		t.Fatalf("no version named: %+v", resp)
	}
	r.replacedBy(t, older)
}
