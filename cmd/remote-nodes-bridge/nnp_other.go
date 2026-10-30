// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package main

// noNewPrivs does nothing off Linux: the bridge only ships there, and a dev
// run on another system has nothing to drop.
func noNewPrivs() error { return nil }
