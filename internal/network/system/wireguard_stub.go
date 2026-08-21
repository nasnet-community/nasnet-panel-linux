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

func (d *wgDevice) Ensure(context.Context, string, WGApplyConfig) error {
	return fmt.Errorf("WireGuard requires Linux")
}

func (d *wgDevice) UpdateEndpoint(context.Context, string, netip.AddrPort) error {
	return fmt.Errorf("WireGuard requires Linux")
}

func (d *wgDevice) Status(context.Context, string) (*WGStatus, error) { return nil, ErrNoWGDevice }

func (d *wgDevice) Delete(context.Context, string) error { return nil }

func (d *wgDevice) List(context.Context) ([]string, error) { return nil, nil }
