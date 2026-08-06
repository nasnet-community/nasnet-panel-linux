//go:build !linux

package server

// Router mode is Linux-only; this just keeps the darwin build working.
func permanentMAC(string) (string, error) { return "", nil }
