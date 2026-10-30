// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// noNewPrivs keeps the bridge, and anything it might start, from gaining
// privileges through a setuid or file-capability binary. The bridge runs as
// its own user already; this closes the way back up.
func noNewPrivs() error {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_SET_NO_NEW_PRIVS): %w", err)
	}
	return nil
}
