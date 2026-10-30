// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// build is a pinned Xray build that is whatever bytes a test wants.
type build struct{ plain []byte }

func (b build) Agent(context.Context, string) (*artifacts.Agent, error) {
	return nil, errors.New("no agent here")
}

func (b build) Xray(_ context.Context, arch string) (*artifacts.Xray, error) {
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write(b.plain)
	w.Close()
	sum := sha256.Sum256(b.plain)
	return &artifacts.Xray{Version: pin.Version, Arch: arch, Gz: gz.Bytes(), PlainSize: int64(len(b.plain)), PlainSHA256: hex.EncodeToString(sum[:])}, nil
}

// The real agent insists that what it is pushed is an ELF file for its host
// and the check can't be set from here, so a push that installs can't be tried
// with it: the build would have to run. The tests here go as far as the agent
// does with a build that isn't one, and with one that is the right kind of
// file and can't run. The push that installs is tested against the stand-in
// agent, which takes the build as it comes.

func TestThePushIsTakenByTheRealAgentAsFarAsItsChecks(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged, files: build{plain: []byte("#!/bin/sh\necho not an ELF file\n")}})
	c := r.connect()
	err := r.env.PushXray(bg, c, r.node, false, nil)
	if err == nil {
		t.Fatal("the agent took a script for an Xray build")
	}
	// the agent unpacked the stream and found the hash right, which it does first, and then looked at the file
	if !strings.Contains(err.Error(), "not an ELF file") || !strings.HasPrefix(err.Error(), "The node refused the Xray build") {
		t.Errorf("err = %q", err)
	}
	// and its Xray is as it was
	if info, err := c.GetVersion(bg); err != nil || agentops.ParseXrayVersion(info.XrayVersion) != "26.10.10" {
		t.Errorf("the node reports %+v, %v", info, err)
	}
}

// A file with the right header for the host passes the agent's check and then
// fails to run: the agent rolls back, and says so, and the node's Xray is the
// one it had.
func TestAPushThatCannotRunIsRolledBackAndSaidSo(t *testing.T) {
	var machine byte
	switch runtime.GOARCH {
	case "amd64":
		machine = 0x3E
	case "arm64":
		machine = 0xB7
	default:
		t.Skip("the agent's ELF check has no opinion on " + runtime.GOARCH)
	}
	elf := make([]byte, 256)
	copy(elf, "\x7fELF")
	elf[0x12] = machine
	r := startRig(t, options{profile: config.ProfileManaged, files: build{plain: elf}})
	c := r.connect()
	before, err := os.ReadFile(r.cfg.Xray.BinaryPath)
	if err != nil {
		t.Fatal(err)
	}

	err = r.env.PushXray(bg, c, r.node, false, nil)
	if err == nil {
		t.Fatal("a file that can't run was installed")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("err = %q", err)
	}
	after, err := os.ReadFile(r.cfg.Xray.BinaryPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("the node's Xray is not the one it had (%v)", err)
	}
	if info, err := c.GetVersion(bg); err != nil || agentops.ParseXrayVersion(info.XrayVersion) != "26.10.10" {
		t.Errorf("the node reports %+v, %v", info, err)
	}
}

// The node already runs the pinned version: nothing is sent, and the hub
// doesn't fetch a build it won't use.
func TestThePushLeavesARealNodeThatHasThePinnedBuild(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged, version: pin.Version, files: build{plain: []byte("never sent")}})
	c := r.connect()
	if err := r.env.PushXray(bg, c, r.node, false, nil); err != nil {
		t.Fatal(err)
	}
}
