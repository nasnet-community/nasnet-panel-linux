// Command release-sign makes and checks the signed files a release ships.
package main

import (
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
)

const keyEnv = "RELEASE_SIGNING_KEY"

const usage = `release-sign keygen
release-sign check                     the key in $RELEASE_SIGNING_KEY is one this build trusts
release-sign sign FILE...              writes FILE.sig
release-sign verify [--pub KEY] FILE   checks FILE.sig
release-sign bundle --version V --arch A --notes NOTES.md --built-at T DIR
release-sign release --version V --notes NOTES.md --built-at T --out DIR FILE...`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Stdout)
	case "check":
		err = check()
	case "sign":
		err = signFiles(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	case "bundle":
		err = bundleCmd(os.Args[2:])
	case "release":
		err = releaseCmd(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "release-sign:", err)
		os.Exit(1)
	}
}

func keygen(w io.Writer) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Private key, for the %s secret (keep an offline copy):\n%s\n\n", keyEnv, base64.StdEncoding.EncodeToString(priv.Seed()))
	fmt.Fprintf(w, "Public key %s, for PublicKeys in pkg/signing/keys.go:\n\tmustKey(%q),\n", signing.KeyID(pub), base64.StdEncoding.EncodeToString(pub))
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\nThe same key for RELEASE_KEYS in nasnet-tool.sh (OpenSSL checks with it):\n    %q\n", base64.StdEncoding.EncodeToString(der))
	return nil
}

func privateKey() (ed25519.PrivateKey, error) {
	s := os.Getenv(keyEnv)
	if s == "" {
		return nil, fmt.Errorf("%s is empty", keyEnv)
	}
	return signing.ParsePrivateKey(s)
}

func check() error {
	priv, err := privateKey()
	if err != nil {
		return err
	}
	pub := priv.Public().(ed25519.PublicKey)
	for _, k := range signing.PublicKeys {
		if k.Equal(pub) {
			fmt.Printf("key %s is trusted\n", signing.KeyID(pub))
			return nil
		}
	}
	return fmt.Errorf("key %s isn't in signing.PublicKeys, so panels would refuse this release", signing.KeyID(pub))
}

func signFile(path string, priv ed25519.PrivateKey) (string, error) {
	sig, err := signing.SignFile(path, priv)
	if err != nil {
		return "", err
	}
	text := signing.EncodeSig(sig)
	return strings.TrimSpace(text), os.WriteFile(path+release.SigSuffix, []byte(text), 0o644)
}

func signFiles(paths []string) error {
	priv, err := privateKey()
	if err != nil {
		return err
	}
	for _, p := range paths {
		if _, err := signFile(p, priv); err != nil {
			return err
		}
	}
	return nil
}

func verify(args []string) error {
	fl := flag.NewFlagSet("verify", flag.ContinueOnError)
	pubFlag := fl.String("pub", "", "base64 public key instead of the trusted ones")
	if err := fl.Parse(args); err != nil || fl.NArg() != 1 {
		return errors.New("usage: verify [--pub KEY] FILE")
	}
	path := fl.Arg(0)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path + release.SigSuffix)
	if err != nil {
		return err
	}
	sig, err := signing.ParseSig(raw)
	if err != nil {
		return err
	}
	ok := false
	if *pubFlag != "" {
		k, err := signing.ParsePublicKey(*pubFlag)
		if err != nil {
			return err
		}
		ok = signing.Verify(data, sig, k)
	} else {
		ok = signing.VerifyAny(data, sig)
	}
	if !ok {
		return release.ErrSignature
	}
	fmt.Println("signature good")
	return nil
}

type meta struct {
	version, notes, minFrom string
	builtAt                 time.Time
}

func metaFlags(fl *flag.FlagSet) func() (meta, error) {
	version := fl.String("version", "", "release tag")
	notesPath := fl.String("notes", "", "notes file, docs/releases/<tag>.md")
	builtAt := fl.String("built-at", "", "RFC 3339 build time")
	return func() (meta, error) {
		if _, err := release.Parse(*version); err != nil {
			return meta{}, err
		}
		md, err := os.ReadFile(*notesPath)
		if err != nil {
			return meta{}, err
		}
		at, err := time.Parse(time.RFC3339, *builtAt)
		if err != nil {
			return meta{}, fmt.Errorf("--built-at: %w", err)
		}
		notes, minFrom := release.SplitNotes(string(md))
		if minFrom != "" {
			if _, err := release.Parse(minFrom); err != nil {
				return meta{}, fmt.Errorf("min_from in %s: %w", *notesPath, err)
			}
		}
		return meta{version: *version, notes: notes, minFrom: minFrom, builtAt: at.UTC()}, nil
	}
}

func prerelease(tag string) bool { return !release.MustParse(tag).Stable() }

func bundleCmd(args []string) error {
	fl := flag.NewFlagSet("bundle", flag.ContinueOnError)
	read := metaFlags(fl)
	arch := fl.String("arch", "", "amd64 or arm64")
	if err := fl.Parse(args); err != nil || fl.NArg() != 1 || *arch == "" {
		return errors.New("usage: bundle --version V --arch A --notes F --built-at T DIR")
	}
	m, err := read()
	if err != nil {
		return err
	}
	priv, err := privateKey()
	if err != nil {
		return err
	}
	return writeBundle(fl.Arg(0), *arch, m, priv)
}

func writeBundle(dir, arch string, m meta, priv ed25519.PrivateKey) error {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if rel == release.BundleName || rel == release.BundleName+release.SigSuffix || rel == "checksums.txt" {
			return nil
		}
		sum, _, err := hashFile(p)
		files[rel] = sum
		return err
	})
	if err != nil {
		return err
	}
	b := release.Bundle{Schema: release.Schema, Version: m.version, Prerelease: prerelease(m.version), MinFrom: m.minFrom,
		Xray: pin.Version, Arch: arch, BuiltAt: m.builtAt, Notes: m.notes, Files: files}
	return writeSigned(filepath.Join(dir, release.BundleName), b, priv)
}

func releaseCmd(args []string) error {
	fl := flag.NewFlagSet("release", flag.ContinueOnError)
	read := metaFlags(fl)
	out := fl.String("out", ".", "where release.json goes")
	if err := fl.Parse(args); err != nil || fl.NArg() == 0 {
		return errors.New("usage: release --version V --notes F --built-at T --out DIR FILE...")
	}
	m, err := read()
	if err != nil {
		return err
	}
	priv, err := privateKey()
	if err != nil {
		return err
	}
	return writeRelease(*out, fl.Args(), m, priv)
}

// writeRelease lists the files in release.json. An agent-linux-ARCH.gz goes
// under agents_gz instead of assets.
func writeRelease(out string, paths []string, m meta, priv ed25519.PrivateKey) error {
	assets := map[string]release.Asset{}
	var gzPaths []string
	for _, p := range paths {
		if _, ok := release.AgentGzArch(filepath.Base(p)); ok {
			gzPaths = append(gzPaths, p) // set aside until the plain agents are hashed
			continue
		}
		sum, size, err := hashFile(p)
		if err != nil {
			return err
		}
		a := release.Asset{SHA256: sum, Size: size}
		if strings.HasPrefix(filepath.Base(p), "nasnet-agent-") {
			sig, err := signing.SignFile(p, priv)
			if err != nil {
				return err
			}
			a.Sig = strings.TrimSpace(signing.EncodeSig(sig))
		}
		assets[filepath.Base(p)] = a
	}
	agentsGz, err := agentGzEntries(gzPaths, assets)
	if err != nil {
		return err
	}
	r := release.Manifest{Schema: release.Schema, Version: m.version, Prerelease: prerelease(m.version), MinFrom: m.minFrom,
		Xray: pin.Version, BuiltAt: m.builtAt, Notes: m.notes, Assets: assets, AgentsGz: agentsGz}
	return writeSigned(filepath.Join(out, release.ManifestName), r, priv)
}

// agentGzEntries lists each gzipped agent beside the plain one it came from.
// A node checks the unpacked bytes against that agent's signed checksum, so a
// file that doesn't unpack to exactly it must stop the release here.
func agentGzEntries(paths []string, assets map[string]release.Asset) (map[string]release.AgentGz, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	entries := map[string]release.AgentGz{}
	for _, p := range paths {
		name := filepath.Base(p)
		arch, _ := release.AgentGzArch(name)
		plainName := release.AgentAsset(arch)
		plain, ok := assets[plainName]
		if !ok {
			return nil, fmt.Errorf("%s is listed without %s", name, plainName)
		}
		sum, size, err := hashFile(p)
		if err != nil {
			return nil, err
		}
		plainSum, plainSize, err := hashUnpacked(p)
		if err != nil {
			return nil, err
		}
		if plainSum != plain.SHA256 || plainSize != plain.Size {
			return nil, fmt.Errorf("%s doesn't unpack to %s", name, plainName)
		}
		entries[arch] = release.AgentGz{Name: name, Size: size, SHA256: sum,
			PlainSize: plain.Size, PlainSHA256: plain.SHA256, Sig: plain.Sig}
	}
	return entries, nil
}

func writeSigned(path string, v any, priv ed25519.PrivateKey) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	_, err = signFile(path, priv)
	return err
}

func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// hashUnpacked is the sha256 and size of what a gzip file unpacks to. Reading
// to the end also checks the file's own CRC, so a truncated copy fails here.
func hashUnpacked(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return "", 0, fmt.Errorf("%s: %w", filepath.Base(p), err)
	}
	h := sha256.New()
	n, err := io.Copy(h, zr)
	if err != nil {
		return "", 0, fmt.Errorf("%s: %w", filepath.Base(p), err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// sortedKeys keeps output stable in tests.
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
