// SPDX-License-Identifier: AGPL-3.0-or-later

// Command remote-nodes-bridge runs on the router beside the hub. The hub
// starts it as another user and sends its configuration on stdin; it dials
// each node and carries the subscriptions' traffic out through the router.
package main

import (
	"fmt"
	"io"
	"os"
)

// maxConfig bounds how much of stdin is read.
const maxConfig = 16 << 20

func main() {
	var in io.Reader = os.Stdin
	// by hand from a terminal nothing is coming, so don't wait for it
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice != 0 {
		in = nil
	}
	os.Exit(run(in, os.Stderr))
}

// run is not the bridge yet. It takes what the hub sends so that the hub's
// write doesn't fail on a closed pipe, then says so and stops.
func run(in io.Reader, errOut io.Writer) int {
	if in != nil {
		io.Copy(io.Discard, io.LimitReader(in, maxConfig))
	}
	fmt.Fprintln(errOut, "remote-nodes-bridge: not implemented yet")
	return 1
}
