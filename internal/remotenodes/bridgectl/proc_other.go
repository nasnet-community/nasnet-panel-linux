// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package bridgectl

import "os/exec"

// applyCredential does nothing off Linux: the hub only ships there, and a dev
// run has no other user to drop to.
func applyCredential(cmd *exec.Cmd, c Credential) (applied bool, note string) {
	return false, "changing user is supported on Linux only: the bridge runs as the current user"
}
