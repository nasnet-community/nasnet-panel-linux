//go:build !linux

package usecase

import (
	"context"
	"errors"
)

type kernelProbe struct{}

func NewKernelProbe() Probe { return &kernelProbe{} }

func (kernelProbe) Carrier(context.Context, string) (bool, error) {
	return false, errors.New("router mode requires Linux")
}

func (kernelProbe) GatewayReachable(context.Context, string, string) (bool, error) {
	return false, errors.New("router mode requires Linux")
}
