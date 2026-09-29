//go:build linux

package mtproto

import (
	"os/exec"
	"syscall"
)

// setProcAttrs makes the kernel stop telemt when the process supervising it
// dies, so an orphan never keeps the inbound's port.
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
