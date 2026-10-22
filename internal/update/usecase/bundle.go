package usecase

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
)

const (
	bundleTopPrefix = "nasnet-panel-offline-linux-"
	maxBundleFiles  = 64
	maxUnpacked     = 2 << 30
)

// bundleEntry is a tar entry's path inside the bundle, without the top
// directory. It refuses anything that could land outside dir.
func bundleEntry(name string) (string, error) {
	name = strings.TrimPrefix(name, "./")
	top, rest, _ := strings.Cut(name, "/")
	if !strings.HasPrefix(top, bundleTopPrefix) {
		return "", fmt.Errorf("%q is outside the bundle directory", name)
	}
	if rest == "" {
		return "", nil
	}
	clean := path.Clean(rest)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%q escapes the bundle", name)
	}
	return clean, nil
}

// unpackBundle extracts tarball into dir and checks every file against the
// signed bundle.json. Files it doesn't list are refused, not ignored.
func unpackBundle(tarball, dir string) (*release.Bundle, error) {
	f, err := os.Open(tarball)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("not a gzip file: %w", err)
	}
	defer gz.Close()

	sums := map[string]string{}
	var total int64
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the bundle is damaged: %w", err)
		}
		rel, err := bundleEntry(h.Name)
		if err != nil {
			return nil, err
		}
		if rel == "" {
			continue
		}
		dest := filepath.Join(dir, filepath.FromSlash(rel))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return nil, err
			}
			continue
		case tar.TypeReg:
		default:
			return nil, fmt.Errorf("%s is not a plain file", rel)
		}
		if len(sums) >= maxBundleFiles {
			return nil, errors.New("the bundle has too many files")
		}
		if total += h.Size; total > maxUnpacked {
			return nil, errors.New("the bundle unpacks too large")
		}
		sum, err := writeEntry(dest, tr, h.Size, os.FileMode(h.Mode)&0o755)
		if err != nil {
			return nil, err
		}
		sums[rel] = sum
	}

	b, err := readUnpackedBundle(dir)
	if err != nil {
		return nil, err
	}
	for rel, want := range b.Files {
		if got, ok := sums[rel]; !ok || !strings.EqualFold(got, want) {
			return nil, fmt.Errorf("%s doesn't match the signed bundle", rel)
		}
	}
	for rel := range sums {
		if _, ok := b.Files[rel]; !ok && !unsignedBundleFile(rel) {
			return nil, fmt.Errorf("%s isn't in the signed bundle", rel)
		}
	}
	return b, nil
}

func unsignedBundleFile(rel string) bool {
	return rel == release.BundleName || rel == release.BundleName+release.SigSuffix || rel == "checksums.txt"
}

func writeEntry(dest string, r io.Reader, size int64, mode os.FileMode) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(r, size))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != size {
		err = fmt.Errorf("%s is cut short", dest)
	}
	return hex.EncodeToString(h.Sum(nil)), err
}

func readUnpackedBundle(dir string) (*release.Bundle, error) {
	data, err := os.ReadFile(filepath.Join(dir, release.BundleName))
	if err != nil {
		return nil, errors.New("the bundle has no bundle.json; it is older than in-panel updates")
	}
	sig, err := os.ReadFile(filepath.Join(dir, release.BundleName+release.SigSuffix))
	if err != nil {
		return nil, errors.New("the bundle has no signature")
	}
	return release.ParseBundle(data, sig)
}

// peekBundle reads bundle.json from a tarball without unpacking the rest.
func peekBundle(tarball string) (*release.Bundle, error) {
	f, err := os.Open(tarball)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, errors.New("that's not an offline bundle (.tar.gz)")
	}
	defer gz.Close()
	var data, sig []byte
	tr := tar.NewReader(gz)
	for data == nil || sig == nil {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the bundle is damaged: %w", err)
		}
		rel, err := bundleEntry(h.Name)
		if err != nil {
			return nil, err
		}
		switch rel {
		case release.BundleName:
			data, err = io.ReadAll(io.LimitReader(tr, 4<<20))
		case release.BundleName + release.SigSuffix:
			sig, err = io.ReadAll(io.LimitReader(tr, 4<<10))
		}
		if err != nil {
			return nil, err
		}
	}
	if data == nil {
		return nil, errors.New("the bundle has no bundle.json; it is older than in-panel updates")
	}
	if sig == nil {
		return nil, errors.New("the bundle has no signature")
	}
	return release.ParseBundle(data, sig)
}
