// SPDX-License-Identifier: AGPL-3.0-or-later

// The JSON helper and the same-site check follow the ones in the Telegram
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

package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UIHeader is what the page sends on every call to /api.
const UIHeader = "X-RemoteNodes-UI"

func writeJSON(w http.ResponseWriter, code int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// utc is t for JSON, or nil (null) when it is unset.
func utc(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// allowed answers 403 and returns false unless the request comes from this
// page. There is no login, so who is asking can't be checked, only where the
// request comes from: it carries a header no other site can add without a
// preflight we refuse, and a request that changes something also has to come
// from this site.
func (a *API) allowed(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.Header.Get(UIHeader) != "1":
		a.fail(w, &Error{CodeForbidden, "This request did not come from the Remote Nodes page."})
	case r.Method != http.MethodGet && r.Method != http.MethodHead && !fromThisSite(r):
		a.fail(w, &Error{CodeForbidden, "Requests from other sites are refused."})
	default:
		return true
	}
	return false
}

// guarded is a route under /api: it runs only for what the page asked.
func (a *API) guarded(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.allowed(w, r) {
			h(w, r)
		}
	}
}

// looksLikeAPI is for a path no route took, which should still be refused
// before it is told "not found". The base path itself usually starts with
// /api, so under it the path is judged by what follows.
func (a *API) looksLikeAPI(path string) bool {
	if b := a.cfg.BasePath; b != "" && (path == b || strings.HasPrefix(path, b+"/")) {
		path = strings.TrimPrefix(path, b)
	}
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

// fromThisSite is the extra check on requests that change something.
//
// A browser states where a request came from in Sec-Fetch-Site, but only over
// https or to localhost, and a router's panel is usually plain http. Without
// it, Origin is compared with the address the request was made to. Behind the
// router's proxy that address is gone (it sets Host to the container's and
// adds X-Forwarded-For), so unless X-Forwarded-Host says otherwise there is
// nothing to compare and the request is let through.
func fromThisSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false // "null" and the like
	}
	want := r.Host
	if fwd, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ","); strings.TrimSpace(fwd) != "" {
		want = strings.TrimSpace(fwd)
	} else if r.Header.Get("X-Forwarded-For") != "" {
		return true // proxied, Host rewritten
	}
	return strings.EqualFold(u.Host, want)
}
