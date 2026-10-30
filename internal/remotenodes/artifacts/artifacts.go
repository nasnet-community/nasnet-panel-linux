// SPDX-License-Identifier: AGPL-3.0-or-later

// Package artifacts fetches what an install pushes to a node: the agent, from
// the project's release, and the Xray build the plugin pins. Both are held in
// memory and nothing touches the router's flash.
//
// The agent is trusted through release.json: its signature is checked, the
// gzipped agent has to match the checksum in it, and the node then unpacks it
// and checks the plain bytes against the plain checksum and signature that
// release.json carries. Xray is trusted through the hashes in internal/xray/pin,
// for the zip and for the binary inside it.
//
// The package imports pkg/release, pkg/signing and internal/xray/pin, which
// are light, and the standard library.
package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
)

const (
	// MinAgentVersion is the first release the plugin can work with, written
	// without the v: the one that carries the gzipped agents, PushXrayBinary
	// and the managed profile. A prerelease of it sorts below it, and is
	// refused too.
	MinAgentVersion = "1.3.0"

	// MaxDownload is the most one file may take in memory: an agent's .gz, the
	// Xray zip, and the xray binary unpacked from it.
	MaxDownload = 64 << 20

	// DefaultReleaseBase serves the project's latest stable release.
	DefaultReleaseBase = "https://github.com/nasnet-community/nasnet-panel-linux/releases/latest/download"
	// ReleaseBaseEnv names the variable that replaces it. The drills point it
	// at a mirror. The signature check is what protects integrity, not the
	// address, so any address will do.
	ReleaseBaseEnv = "RELEASE_BASE_URL"

	// xrayBase is XTLS's releases, one folder per version.
	xrayBase = "https://github.com/XTLS/Xray-core/releases/download"

	maxManifest = 4 << 20 // release.json; the panel's updater takes the same
	maxSig      = 4 << 10
)

var (
	ErrNoGzAgents      = errors.New("artifacts: the release has no gzipped agents")
	ErrAgentTooOld     = errors.New("artifacts: the release is older than the plugin needs")
	ErrBadHash         = errors.New("artifacts: a download isn't what its checksum says")
	ErrTooLarge        = errors.New("artifacts: a download is larger than allowed")
	ErrUnsupportedArch = errors.New("artifacts: unsupported architecture")
	ErrManifest        = errors.New("artifacts: the release manifest can't be trusted")
	ErrBadArchive      = errors.New("artifacts: the Xray zip isn't laid out as expected")
)

// minAgent is MinAgentVersion as a release tag.
var minAgent = release.MustParse("v" + MinAgentVersion)

// xraySuffix is how XTLS names each zip after Xray-linux-, by the
// architecture the plugin uses. Its keys are the architectures there are.
var xraySuffix = map[string]string{"amd64": "64", "arm64": "arm64-v8a"}

func checkArch(arch string) error {
	if _, ok := xraySuffix[arch]; !ok {
		return fmt.Errorf("%w: %q (amd64 and arm64 are the ones)", ErrUnsupportedArch, arch)
	}
	return nil
}

// Agent is an agent build as the install pushes it with SelfUpdate: the
// gzipped bytes, and what the node checks them against after unpacking. A
// Holder gives the same Agent to every node of a job, so nothing may change it.
type Agent struct {
	Version string // the release's tag, as release.json says, such as "v1.3.0"
	Arch    string
	Gz      []byte // as published, which the checksum below covers
	// GzSHA256 is the checksum of Gz.
	GzSHA256 string
	// PlainSize and PlainSHA256 are for the unpacked agent. The node must be
	// sent PlainSHA256: with none, it skips the check.
	PlainSize   int64
	PlainSHA256 string
	// Sig is the unpacked agent's signature, 64 bytes, for the signature
	// argument of SelfUpdate. It comes from release.json, whose own signature
	// covers it.
	Sig []byte
}

// Xray is the pinned Xray build for one architecture, gzipped for
// PushXrayBinary. Like an Agent, it is shared and read-only.
type Xray struct {
	Version     string // pin.Version
	Arch        string
	Gz          []byte
	PlainSize   int64
	PlainSHA256 string // the pinned hash of the unpacked binary
}

// VerifyFunc checks release.json against its signature file and decodes it.
type VerifyFunc func(manifest, sig []byte) (*release.Manifest, error)

// Source is where the project's release files are fetched from.
type Source struct {
	// Base is a URL that a file name is added to, without the slash between.
	// Empty means DefaultReleaseBase.
	Base string
	// Verify is nil for release.ParseManifest, which trusts the keys in
	// pkg/signing. Tests give the check of a key of their own.
	Verify VerifyFunc
}

// SourceFromEnv is the default source, or the one RELEASE_BASE_URL names. The
// error doesn't repeat the value, which could hold a password.
func SourceFromEnv(getenv func(string) string) (Source, error) {
	v := strings.TrimSpace(getenv(ReleaseBaseEnv))
	if v == "" {
		return Source{}, nil
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return Source{}, fmt.Errorf("%s must be an http or https address, with no login, query or fragment", ReleaseBaseEnv)
	}
	return Source{Base: strings.TrimRight(v, "/")}, nil
}

func (s Source) url(name string) string {
	base := s.Base
	if base == "" {
		base = DefaultReleaseBase
	}
	return strings.TrimRight(base, "/") + "/" + name
}

// xrayPin is the release the plugin installs, with the hashes that vouch for it.
type xrayPin struct {
	version  string
	zip, bin map[string]string // by arch
}

// fetcher is what FetchAgent and FetchXray run on. The exported functions use
// the real pin and limits; tests set their own.
type fetcher struct {
	xrayBase string
	pin      xrayPin
	limit    int64         // MaxDownload
	stall    time.Duration // how long a download may go without a byte
}

func defaults() fetcher {
	return fetcher{
		xrayBase: xrayBase,
		pin:      xrayPin{version: pin.Version, zip: pin.ZipSHA256, bin: pin.BinarySHA256},
		limit:    MaxDownload,
		stall:    stallTimeout,
	}
}

// FetchAgent gets the gzipped agent for arch from the release at src. It
// refuses a release that isn't signed by a trusted key (ErrManifest), is
// older than MinAgentVersion (ErrAgentTooOld) or has no gzipped agents
// (ErrNoGzAgents), and a download that isn't the size and checksum the release
// says.
//
// release.json and the file are two requests. If a release is published
// between them, the checksum fails (ErrBadHash) and trying again settles it.
func FetchAgent(ctx context.Context, c *http.Client, src Source, arch string) (*Agent, error) {
	return defaults().agent(ctx, c, src, arch)
}

// FetchXray gets the pinned Xray release for arch, checks the zip against its
// pinned hash, unpacks the xray binary in memory and checks that against its
// pinned hash too, and returns it gzipped.
func FetchXray(ctx context.Context, c *http.Client, arch string) (*Xray, error) {
	return defaults().xray(ctx, c, arch)
}

// agentEntry is what release.json says about one architecture's agent.
type agentEntry struct {
	version string
	arch    string
	gz      release.AgentGz
	gzSum   string // lower case
	sig     []byte
}

func (f fetcher) agent(ctx context.Context, c *http.Client, src Source, arch string) (*Agent, error) {
	e, err := f.entry(ctx, c, src, arch)
	if err != nil {
		return nil, err
	}
	return f.downloadAgent(ctx, c, src, e)
}

// entry reads release.json and picks the agent for arch out of it.
func (f fetcher) entry(ctx context.Context, c *http.Client, src Source, arch string) (*agentEntry, error) {
	if err := checkArch(arch); err != nil {
		return nil, err
	}
	data, err := f.small(ctx, c, src.url(release.ManifestName), maxManifest)
	if err != nil {
		return nil, err
	}
	sigFile, err := f.small(ctx, c, src.url(release.ManifestName+release.SigSuffix), maxSig)
	if err != nil {
		return nil, err
	}
	verify := src.Verify
	if verify == nil {
		verify = release.ParseManifest
	}
	m, err := verify(data, sigFile)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrManifest, err)
	}
	// whatever verified it, these are the plugin's to check
	if m == nil || m.Schema != release.Schema {
		return nil, fmt.Errorf("%w: not a schema %d manifest", ErrManifest, release.Schema)
	}
	v, err := release.Parse(m.Version)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManifest, err)
	}
	if v.Compare(minAgent) < 0 {
		return nil, fmt.Errorf("%w: the source has %s and the plugin needs %s or later", ErrAgentTooOld, m.Version, minAgent)
	}
	gz, ok := m.AgentsGz[arch]
	if !ok {
		return nil, fmt.Errorf("%w: %s has none for %s", ErrNoGzAgents, m.Version, arch)
	}
	return f.checkEntry(m.Version, arch, gz)
}

// checkEntry holds an entry to what the release tool writes. Its name goes
// into a URL and its numbers decide how much memory is taken, so nothing in it
// is used as it comes.
func (f fetcher) checkEntry(version, arch string, gz release.AgentGz) (*agentEntry, error) {
	bad := func(what string) (*agentEntry, error) {
		return nil, fmt.Errorf("%w: %s's agent for %s %s", ErrManifest, version, arch, what)
	}
	if gz.Name != release.AgentGzAsset(arch) {
		return bad("has the wrong file name")
	}
	sum, ok := lowerSHA256(gz.SHA256)
	if _, plainOK := lowerSHA256(gz.PlainSHA256); !ok || !plainOK {
		return bad("has a checksum that isn't SHA-256")
	}
	if gz.Size <= 0 || gz.PlainSize <= 0 {
		return bad("has no size")
	}
	if gz.Size > f.limit {
		return nil, fmt.Errorf("%w: %s's agent for %s is %d bytes, the most taken is %d", ErrTooLarge, version, arch, gz.Size, f.limit)
	}
	sig, err := signing.ParseSig([]byte(gz.Sig))
	if err != nil {
		return bad("has no usable signature")
	}
	gz.PlainSHA256, _ = lowerSHA256(gz.PlainSHA256)
	return &agentEntry{version: version, arch: arch, gz: gz, gzSum: sum, sig: sig}, nil
}

func (f fetcher) downloadAgent(ctx context.Context, c *http.Client, src Source, e *agentEntry) (*Agent, error) {
	body, err := f.exact(ctx, c, src.url(e.gz.Name), e.gz.Name, e.gz.Size)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != e.gzSum {
		return nil, fmt.Errorf("%w: %s is %s, release.json says %s", ErrBadHash, e.gz.Name, got, e.gzSum)
	}
	return &Agent{
		Version: e.version, Arch: e.arch, Gz: body, GzSHA256: e.gzSum,
		PlainSize: e.gz.PlainSize, PlainSHA256: e.gz.PlainSHA256, Sig: e.sig,
	}, nil
}

// lowerSHA256 is s in lower case if it is 64 hex digits.
func lowerSHA256(s string) (string, bool) {
	if len(s) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", false
	}
	return strings.ToLower(s), true
}
