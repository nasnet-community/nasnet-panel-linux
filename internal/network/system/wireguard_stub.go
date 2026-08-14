//go:build !linux

package system

import (
	"context"
	"fmt"
	"net/netip"
)

type wgDevice struct{}

// NewWGDevice off Linux exists so the package builds; router mode is Ubuntu only.
func NewWGDevice() WGDevice { return &wgDevice{} }

func (d *wgDevice) Ensure(context.Context, WGApplyConfig) error {
	return fmt.Errorf("WireGuard requires Linux")
}

func (d *wgDevice) UpdateEndpoint(context.Context, netip.AddrPort) error {
	return fmt.Errorf("WireGuard requires Linux")
}

func (d *wgDevice) Status(context.Context) (*WGStatus, error) { return nil, ErrNoWGDevice }

func (d *wgDevice) Delete(context.Context) error { return nil }
