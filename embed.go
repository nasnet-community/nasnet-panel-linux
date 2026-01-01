package main

import "embed"

// WebFS embeds the Vite build output for the admin panel SPA.
// The web-panel/dist directory must exist before `go build` (run `make web` first).
//
//go:embed all:web-panel/dist
var WebFS embed.FS
