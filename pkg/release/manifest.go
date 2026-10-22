package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
)

// Schema is the manifest format this build reads.
const Schema = 1

// File names on the release and inside each bundle.
const (
	ManifestName = "release.json"
	BundleName   = "bundle.json"
	SigSuffix    = ".sig"
)

// BundleAsset is the update file for one architecture.
func BundleAsset(arch string) string { return "nasnet-panel-offline-linux-" + arch + ".tar.gz" }

// Asset is one release file.
type Asset struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// Sig is set for agent binaries, which nodes check themselves.
	Sig string `json:"sig,omitempty"`
}

// Manifest is release.json: what a release is and what it ships.
type Manifest struct {
	Schema     int              `json:"schema"`
	Version    string           `json:"version"`
	Prerelease bool             `json:"prerelease"`
	MinFrom    string           `json:"min_from,omitempty"`
	Xray       string           `json:"xray"`
	BuiltAt    time.Time        `json:"built_at"`
	Notes      string           `json:"notes"`
	Assets     map[string]Asset `json:"assets"`
}

// Bundle is bundle.json, inside each offline bundle, so an uploaded one checks itself.
type Bundle struct {
	Schema     int               `json:"schema"`
	Version    string            `json:"version"`
	Prerelease bool              `json:"prerelease"`
	MinFrom    string            `json:"min_from,omitempty"`
	Xray       string            `json:"xray"`
	Arch       string            `json:"arch"`
	BuiltAt    time.Time         `json:"built_at"`
	Notes      string            `json:"notes"`
	Files      map[string]string `json:"files"`
}

// ErrSignature means no trusted key signed the file.
var ErrSignature = errors.New("the signature doesn't match any key this panel trusts")

// ParseManifest checks release.json's signature, then reads it.
func ParseManifest(data, sig []byte) (*Manifest, error) {
	var m Manifest
	if err := parseSigned(data, sig, &m); err != nil {
		return nil, err
	}
	if err := checkHeader(m.Schema, m.Version, m.MinFrom); err != nil {
		return nil, err
	}
	return &m, nil
}

// ParseBundle checks bundle.json's signature, then reads it.
func ParseBundle(data, sig []byte) (*Bundle, error) {
	var b Bundle
	if err := parseSigned(data, sig, &b); err != nil {
		return nil, err
	}
	if err := checkHeader(b.Schema, b.Version, b.MinFrom); err != nil {
		return nil, err
	}
	if b.Arch == "" || len(b.Files) == 0 {
		return nil, errors.New("bundle.json names no architecture or files")
	}
	return &b, nil
}

func parseSigned(data, sigFile []byte, into any) error {
	sig, err := signing.ParseSig(sigFile)
	if err != nil {
		return err
	}
	if !signing.VerifyAny(data, sig) {
		return ErrSignature
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("unreadable manifest: %w", err)
	}
	return nil
}

func checkHeader(schema int, version, minFrom string) error {
	if schema != Schema {
		return fmt.Errorf("manifest schema %d; this panel reads %d", schema, Schema)
	}
	if _, err := Parse(version); err != nil {
		return err
	}
	if minFrom != "" {
		if _, err := Parse(minFrom); err != nil {
			return fmt.Errorf("min_from: %w", err)
		}
	}
	return nil
}

// AllowsFrom reports whether a panel on running may update straight to this release.
func AllowsFrom(minFrom string, running Version) bool {
	if minFrom == "" {
		return true
	}
	v, err := Parse(minFrom)
	return err == nil && running.Compare(v) >= 0
}

var minFromRE = regexp.MustCompile(`(?m)^\s*<!--\s*min_from:\s*(\S+)\s*-->\s*$\n?`)

// SplitNotes pulls the optional <!-- min_from: vX --> line out of a notes file.
func SplitNotes(md string) (notes, minFrom string) {
	if m := minFromRE.FindStringSubmatch(md); m != nil {
		minFrom = m[1]
	}
	return strings.TrimSpace(minFromRE.ReplaceAllString(md, "")), minFrom
}

var atomTagRE = regexp.MustCompile(`/releases/tag/([^"<>\s/]+)`)

// AtomTags lists the tags in GitHub's releases.atom, prereleases included.
func AtomTags(feed []byte) []string {
	seen := map[string]bool{}
	var tags []string
	for _, m := range atomTagRE.FindAllSubmatch(feed, -1) {
		t := string(m[1])
		if !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	return tags
}
