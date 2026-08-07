//go:build !linux

package httpclient

// No-op off Linux; router mode is Ubuntu 24.04 only.
func setSOMark(int, uint32) error { return nil }
