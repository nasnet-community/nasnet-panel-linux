//go:build !linux

package mtproto

import "os/exec"

// setProcAttrs is a no-op off Linux (telemt only ships Linux builds).
func setProcAttrs(*exec.Cmd) {}
