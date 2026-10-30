// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgectl

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// applyCredential makes cmd run as c, and says what it did. Only root may
// change user, so for anyone else (tests, a dev run) the child runs as the
// current user and the note says so.
func applyCredential(cmd *exec.Cmd, c Credential) (applied bool, note string) {
	if euid := os.Geteuid(); euid != 0 {
		return false, fmt.Sprintf("not root (euid %d): the bridge runs as the current user", euid)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// An empty Groups list drops the hub's supplementary groups too.
		Credential: &syscall.Credential{Uid: c.UID, Gid: c.GID, Groups: []uint32{}},
	}
	return true, fmt.Sprintf("the bridge runs as uid %d, gid %d", c.UID, c.GID)
}
