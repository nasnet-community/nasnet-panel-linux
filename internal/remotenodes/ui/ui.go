// SPDX-License-Identifier: AGPL-3.0-or-later

// The base path handling and the page handler follow the ones in the Telegram
// MTProto plugin (github.com/nasnet-community/mtproto-remote-plugin), which is
// MIT licensed:
//
//	MIT License
//
//	Copyright (c) 2026 NASNET Community
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE.

// Package ui serves the plugin's web page.
package ui

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// page is a placeholder until the real single-file app takes its place.
//
//go:embed index.html
var page []byte

// New serves the embedded page for base.
func New(base string) http.Handler { return NewHandler(page, base) }

// NewHandler serves html with the base tag added.
func NewHandler(html []byte, base string) http.Handler {
	return handler{html: Inject(html, base)}
}

type handler struct{ html []byte }

func (h handler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	hd := w.Header()
	hd.Set("Content-Type", "text/html; charset=utf-8")
	hd.Set("Cache-Control", "no-store")
	hd.Set("Content-Length", strconv.Itoa(len(h.html)))
	w.Write(h.html)
}

// BasePath normalises BASE_PATH to "" or "/a/b". It lands in the page, so
// only plain segments pass.
func BasePath(s string) (string, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s == "" {
		return "", nil
	}
	if !strings.HasPrefix(s, "/") {
		return "", fmt.Errorf("BASE_PATH %q must start with /", s)
	}
	for _, seg := range strings.Split(s[1:], "/") {
		if !plain(seg) || seg == "." || seg == ".." {
			return "", fmt.Errorf("BASE_PATH %q is not a plain path", s)
		}
	}
	return s, nil
}

// plain is ^[A-Za-z0-9._~-]+$ without a regexp.
func plain(seg string) bool {
	if seg == "" {
		return false
	}
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '.' || c == '_' || c == '~' || c == '-') {
			return false
		}
	}
	return true
}

// Inject adds <base href="BASE/"> so the page's relative api/ calls work
// whether the router's proxy strips the prefix or not.
func Inject(html []byte, base string) []byte {
	tag := []byte(`<head><base href="` + base + `/">`)
	return bytes.Replace(html, []byte("<head>"), tag, 1)
}
