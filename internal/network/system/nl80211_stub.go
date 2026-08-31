//go:build !linux

package system

import "fmt"

// Router mode is Linux only; this exists so the package builds on a mac
func NewRadioProber() (RadioProber, error) {
	return nil, fmt.Errorf("radio probing requires Linux")
}
