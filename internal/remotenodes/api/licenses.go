// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// maxLicenseBytes is the most /licenses serves, headings included. The image's
// files come to about 200 kB, so a directory far past this is the wrong one.
const maxLicenseBytes = 2 << 20

// licenseOrder is what the text starts with, in this order. Any other .txt
// file follows, by name.
var licenseOrder = []string{"LICENSE", "NOTICES", "web-notices.txt"}

var errNoLicenses = &Error{CodeNotFound, "This hub has no licence files to show."}

// licenses answers GET /licenses with the licence files the image carries, as
// one plain text, so the page's Notices link shows what is really shipped. It
// reads them on every request. Nothing in the request picks a file, and like
// the page itself it needs no header: a link opened in a new tab can't send one.
func (a *API) licenses(w http.ResponseWriter, _ *http.Request) {
	if a.cfg.Licenses == nil {
		a.fail(w, errNoLicenses)
		return
	}
	body, err := licenseText(a.cfg.Licenses)
	if err != nil {
		a.fail(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.Write(body)
}

var licenseRule = strings.Repeat("#", 64)

func licenseHeading(name string) string {
	return licenseRule + "\n# " + name + "\n" + licenseRule + "\n\n"
}

// licenseWanted says whether a file name in the directory is part of the text:
// the three named in licenseOrder, and the other .txt files. A name that starts
// with a dot is the junk a copy leaves behind (._NOTICES.txt), not a notice.
func licenseWanted(name string) bool {
	if slices.Contains(licenseOrder, name) {
		return true
	}
	return !strings.HasPrefix(name, ".") && strings.HasSuffix(strings.ToLower(name), ".txt")
}

func licenseRank(name string) int {
	if i := slices.Index(licenseOrder, name); i >= 0 {
		return i
	}
	return len(licenseOrder)
}

// licenseText is the files of fsys one after the other, each under a heading:
// LICENSE, NOTICES and web-notices.txt first when they are there, then the other
// .txt files by name. A file that is empty, or is not a plain file, is left out
// (a link is followed, a named pipe would block the read). errNoLicenses means
// there is nothing to show, because the directory is missing or holds none of
// them. Anything else is not the visitor's doing: a read that fails, or more
// than maxLicenseBytes of text.
func licenseText(fsys fs.FS) ([]byte, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errNoLicenses
	}
	if err != nil {
		return nil, fmt.Errorf("licences: %w", err)
	}
	var names []string
	for _, e := range entries {
		if licenseWanted(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(licenseRank(a), licenseRank(b)), strings.Compare(a, b))
	})

	// the sizes first, so a directory that is far too big is refused unread
	var files []string
	var total int64 // the text as long as it can get: every file with its heading, a blank line between
	for _, name := range names {
		fi, err := fs.Stat(fsys, name)
		switch {
		case errors.Is(err, fs.ErrNotExist): // a link to nowhere, or gone since the listing
			continue
		case err != nil:
			return nil, fmt.Errorf("licences: %w", err)
		case !fi.Mode().IsRegular() || fi.Size() == 0:
			continue
		}
		if len(files) > 0 {
			total++
		}
		files = append(files, name)
		total += int64(len(licenseHeading(name))) + fi.Size() + 1
	}
	tooBig := func() error {
		return fmt.Errorf("licences: the files add up to more than the %d bytes that are served", maxLicenseBytes)
	}
	if total > maxLicenseBytes {
		return nil, tooBig()
	}

	var out bytes.Buffer
	out.Grow(int(total))
	for _, name := range files {
		sep := 0
		if out.Len() > 0 {
			sep = 1
		}
		// a file may have grown since it was measured: read no more than what is left
		left := maxLicenseBytes - out.Len() - sep - len(licenseHeading(name)) - 1
		if left < 0 {
			return nil, tooBig()
		}
		data, err := readLimited(fsys, name, left)
		if err != nil {
			return nil, fmt.Errorf("licences: %w", err)
		}
		if len(data) > left {
			return nil, tooBig()
		}
		data = bytes.TrimRight(data, "\n")
		if len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		if sep > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(licenseHeading(name))
		out.Write(data)
		out.WriteByte('\n')
	}
	if out.Len() == 0 {
		return nil, errNoLicenses
	}
	return out.Bytes(), nil
}

// readLimited reads a file, but no more than limit+1 bytes: the extra one shows
// the caller that there was more.
func readLimited(fsys fs.FS, name string, limit int) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(limit)+1))
}
