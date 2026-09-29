// Package coredeps is the declarative dependency manifest for the tunnel cores,
// shared by the hub and the node agent. Each protocol maps to the binaries a
// node needs before that core can run.
//
// The agent interprets these specs (probe + install); it never receives
// free-form commands or URLs from the API — the manifest is compiled into the
// binary and is the security boundary (URLs and SHA256s are pinned here). This
// package depends on the standard library only, so both sides can import it.
// Protocols are plain strings matching the hub's CoreProtocol and the agent's
// CoreType.
package coredeps

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Protocol is a tunnel core protocol identifier (wire value shared hub↔agent).
type Protocol string

const (
	SSH     Protocol = "ssh"
	MTProto Protocol = "mtproto"
)

// DepKind selects how a Dependency is satisfied. Only static binaries are
// installed: SSH is built in and MTProto is one relay binary.
type DepKind string

const KindBinary DepKind = "binary" // download → verify → extract → move to Dest

// Probe describes how to detect whether a dependency is already satisfied.
type Probe struct {
	// Binary is the executable name looked up on $PATH first.
	Binary string
	// Paths are well-known absolute fallbacks checked in order.
	Paths []string
	// VersionCmd, when set, is run (args after the resolved binary) to read a
	// version string for reporting; failure is non-fatal.
	VersionCmd []string
}

// BinarySpec is the payload for KindBinary: a release tarball holding the
// binary, pinned per machine architecture.
type BinarySpec struct {
	// URLTemplate is the release asset URL; {uarch} expands to the uname
	// machine name (x86_64 / aarch64).
	URLTemplate string
	// SHA256 maps a uname machine name to the tarball's pinned hash. An
	// architecture without an entry is unsupported.
	SHA256 map[string]string
	// ArchiveMember names the file to extract from the tarball (by basename).
	ArchiveMember string
	Dest          string      // absolute install path, e.g. /usr/local/bin/telemt
	Mode          os.FileMode // e.g. 0o755
	// Unsupported is the message for an architecture with no pinned build.
	Unsupported string
}

// Target returns the download URL and pinned hash for goarch ("" = this host).
func (b *BinarySpec) Target(goarch string) (url, sha string, err error) {
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	if runtime.GOOS != "linux" {
		return "", "", fmt.Errorf("%s", b.Unsupported)
	}
	uarch := unameArch(goarch)
	sha, ok := b.SHA256[uarch]
	if !ok {
		return "", "", fmt.Errorf("%s", b.Unsupported)
	}
	return strings.ReplaceAll(b.URLTemplate, "{uarch}", uarch), sha, nil
}

// unameArch maps a Go GOARCH to the `uname -m` machine name used in release
// asset filenames.
func unameArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return goarch
	}
}

// Dependency is one requirement of a core.
type Dependency struct {
	Name   string
	Kind   DepKind
	Probe  Probe
	Binary *BinarySpec
	// Optional marks a dependency whose absence does not fail an install;
	// informational for the UI.
	Optional bool
}

// ProbeResult is the outcome of evaluating a Dependency.Probe on a node.
type ProbeResult struct {
	Name     string  `json:"name"`
	Kind     DepKind `json:"kind"`
	Present  bool    `json:"present"`
	Version  string  `json:"version,omitempty"`
	Path     string  `json:"path,omitempty"`
	Optional bool    `json:"optional"`
}

// Resolve returns the resolved absolute path for the probe's binary: $PATH
// first, then the first existing well-known candidate, else the bare name (so a
// subsequent exec surfaces a clear not-found error). ok reports whether a real
// file was found. A Probe with an empty Binary always resolves ok (nothing to
// install — the pure-Go SSH core).
func (p Probe) Resolve() (path string, ok bool) {
	if p.Binary == "" {
		return "", true
	}
	if abs, err := exec.LookPath(p.Binary); err == nil {
		return abs, true
	}
	for _, c := range p.Paths {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, true
		}
	}
	return p.Binary, false
}

// ResolveBin resolves an executable name to an absolute path: $PATH first, then
// the first existing well-known candidate. ok is false when neither exists.
func ResolveBin(name string, candidates ...string) (string, bool) {
	return Probe{Binary: name, Paths: candidates}.Resolve()
}

// Version runs VersionCmd against the resolved binary and returns the first
// non-empty trimmed line, or "" on any failure. Best-effort, never errors.
func (p Probe) Version() string {
	if len(p.VersionCmd) == 0 {
		return ""
	}
	abs, ok := p.Resolve()
	if !ok {
		return ""
	}
	out, err := exec.Command(abs, p.VersionCmd...).CombinedOutput() //nolint:gosec // args are static manifest data
	if err != nil && len(out) == 0 {
		return ""
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}
