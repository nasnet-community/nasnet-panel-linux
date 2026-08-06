//go:build !linux

package system

import "fmt"

// Router mode is Ubuntu 24.04 only
func NewNetlinkBackend() (Backend, error) {
	return nil, fmt.Errorf("router mode requires Linux")
}
