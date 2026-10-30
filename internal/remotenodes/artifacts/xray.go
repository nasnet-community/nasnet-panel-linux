// SPDX-License-Identifier: AGPL-3.0-or-later

package artifacts

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// A release zip holds five files. A bound well above that is enough to see a
// zip that is meant to eat memory.
const maxZipEntries = 64

func (f fetcher) xray(ctx context.Context, c *http.Client, arch string) (*Xray, error) {
	if err := checkArch(arch); err != nil {
		return nil, err
	}
	wantZip, wantBin := f.pin.zip[arch], f.pin.bin[arch]
	if wantZip == "" || wantBin == "" {
		return nil, fmt.Errorf("%w: the pin has no Xray %s for %s", ErrUnsupportedArch, f.pin.version, arch)
	}
	name := "Xray-linux-" + xraySuffix[arch] + ".zip"
	data, sum, err := f.capped(ctx, c, f.xrayBase+"/v"+f.pin.version+"/"+name, name)
	if err != nil {
		return nil, err
	}
	// the whole zip is vouched for before any of it is read
	if sum != wantZip {
		return nil, fmt.Errorf("%w: %s is %s, the pin says %s", ErrBadHash, name, sum, wantZip)
	}
	gz, size, binSum, err := f.unpack(data)
	if err != nil {
		return nil, err
	}
	if binSum != wantBin {
		return nil, fmt.Errorf("%w: the xray in %s is %s, the pin says %s", ErrBadHash, name, binSum, wantBin)
	}
	return &Xray{Version: f.pin.version, Arch: arch, Gz: gz, PlainSize: size, PlainSHA256: binSum}, nil
}

// unpack finds the xray file in a release zip and gzips it as it comes out,
// hashing the unpacked bytes on the way, so the binary is never held whole.
//
// The zip is only looked into by name, never written out, so a name that
// climbs out of a folder can't hurt; it still stops the whole zip, since no
// release has one. The same goes for a second xray, which would leave it open
// which one is meant. The declared size is held to the limit before a byte is
// read, and the bytes are held to it again as they are read.
func (f fetcher) unpack(data []byte) (gz []byte, size int64, sum string, err error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, 0, "", fmt.Errorf("%w: %v", ErrBadArchive, err) // a name it finds unsafe included
	}
	if len(zr.File) > maxZipEntries {
		return nil, 0, "", fmt.Errorf("%w: %d entries", ErrBadArchive, len(zr.File))
	}
	var entry *zip.File
	for _, zf := range zr.File {
		if !safeName(zf.Name) {
			return nil, 0, "", fmt.Errorf("%w: an entry named %q", ErrBadArchive, clip(zf.Name))
		}
		if zf.Name != "xray" {
			continue
		}
		if entry != nil {
			return nil, 0, "", fmt.Errorf("%w: two entries named xray", ErrBadArchive)
		}
		entry = zf
	}
	switch {
	case entry == nil:
		return nil, 0, "", fmt.Errorf("%w: no entry named xray", ErrBadArchive)
	case !entry.Mode().IsRegular() || entry.Flags&0x1 != 0:
		return nil, 0, "", fmt.Errorf("%w: xray is not a plain file", ErrBadArchive)
	case entry.UncompressedSize64 > uint64(f.limit):
		return nil, 0, "", fmt.Errorf("%w: xray unpacks to %d bytes, the most taken is %d", ErrTooLarge, entry.UncompressedSize64, f.limit)
	}
	rc, err := entry.Open()
	if err != nil {
		return nil, 0, "", fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	defer rc.Close()

	var out bytes.Buffer
	out.Grow(int(entry.UncompressedSize64 / 2)) // gzip -9 leaves about 40% of Xray; sized once so the buffer doesn't double
	zw, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
	if err != nil {
		return nil, 0, "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(h, zw), io.LimitReader(rc, f.limit+1))
	switch {
	case err != nil:
		// the zip reader notices a size or checksum that doesn't match the entry
		return nil, 0, "", fmt.Errorf("%w: xray doesn't unpack: %v", ErrBadArchive, err)
	case n > f.limit:
		return nil, 0, "", fmt.Errorf("%w: xray unpacks past %d bytes", ErrTooLarge, f.limit)
	}
	if err := zw.Close(); err != nil {
		return nil, 0, "", err
	}
	return out.Bytes(), n, hex.EncodeToString(h.Sum(nil)), nil
}

// safeName is a relative path with no backslash, NUL, colon (a drive), empty
// part or dot part. A folder entry's trailing slash is let by.
func safeName(name string) bool {
	n := strings.TrimSuffix(name, "/")
	return !strings.ContainsAny(n, "\\\x00:") && n != "." && fs.ValidPath(n)
}

func clip(s string) string {
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "..."
	}
	return s
}
