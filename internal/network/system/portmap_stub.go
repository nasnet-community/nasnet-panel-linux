//go:build !linux

package system

// Router mode is Linux only; unbound sockets keep the package testable on a mac.
func NewPortMapper() PortMapper { return newPortMapClient(nil) }
