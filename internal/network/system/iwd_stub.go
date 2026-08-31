//go:build !linux

package system

import "fmt"

// Router mode is Linux only; this exists so the package builds on a mac
func NewIWDClient() (StationClient, error) {
	return nil, fmt.Errorf("station mode requires Linux")
}
