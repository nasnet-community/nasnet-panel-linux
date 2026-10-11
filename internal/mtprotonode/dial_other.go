//go:build !linux

package mtprotonode

// NewDialers off Linux has no policy routing: plain dials only.
func NewDialers(bool) Dialers { return plainDialers() }
