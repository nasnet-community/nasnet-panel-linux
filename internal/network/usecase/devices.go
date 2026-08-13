package usecase

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/oui"
)

// LANDevice is one client on the bridge, assembled from three sources.
type LANDevice struct {
	MAC string `json:"mac"`
	// IPs holds every address seen for the MAC, the leased one first. A client
	// with a static address on top of its lease has two, which is real.
	IPs []string `json:"ips"`
	// Hostname is what the client asked to be called, after sanitizing. Empty
	// when it offered none or offered something unsafe to render.
	Hostname string `json:"hostname"`
	// Vendor is from the MAC's registered prefix. Empty for a randomized MAC,
	// where any match would be coincidence.
	Vendor string `json:"vendor"`
	// Label is the operator's name for it.
	Label string `json:"label"`
	// Randomized means the MAC is locally administered, so it names a session
	// rather than a device and cannot carry a name.
	Randomized bool `json:"randomized"`
	// Port is the bridge member it was learned on. With a switch behind one
	// port, everything behind it reports that port.
	Port string `json:"port,omitempty"`
	// Online is FDB age against the bridge's own ageing time. Arrivals show up
	// on the first frame; a departure takes until the entry ages out.
	Online bool `json:"online"`
	// LastSeenSeconds is how stale the FDB sighting is. Nil when the device is
	// known only from a lease.
	LastSeenSeconds *int `json:"last_seen_seconds,omitempty"`
	// LeaseExpiry is nil for a device with no lease, i.e. a static address.
	LeaseExpiry *time.Time `json:"lease_expiry,omitempty"`
}

// LANDeviceList states which sources answered rather than quietly returning a
// short list. An unexplained empty list is indistinguishable from "nothing is
// connected", which is the question this feature exists to answer.
type LANDeviceList struct {
	Devices []LANDevice `json:"devices"`
	// Enabled is false when the LAN is off; then there is nothing to list.
	Enabled bool `json:"enabled"`
	// LeasesOK false means no hostnames and no lease-only devices.
	LeasesOK bool `json:"leases_ok"`
	// NeighboursOK false means devices with a static address may show no IP.
	NeighboursOK bool `json:"neighbours_ok"`
	// OfflineAfterSeconds is the bridge's ageing time: how long a departed
	// device keeps reading online. Surfaced so the UI can say so.
	OfflineAfterSeconds int `json:"offline_after_seconds"`
}

// ListDevices assembles the device list.
//
// The FDB is the only required source: it decides who is present, so losing it
// means the answer is unknown rather than empty. Leases and neighbours each
// degrade to a missing field.
func (u *networkUsecase) ListDevices(ctx context.Context) (*LANDeviceList, error) {
	out := &LANDeviceList{Devices: []LANDevice{}}

	lan := u.lanConfig(ctx)
	if lan == nil || !lan.Enabled {
		return out, nil
	}
	out.Enabled = true

	bridge := lan.BridgeName
	if bridge == "" {
		bridge = system.LANBridgeName
	}
	src := u.deviceSource()

	ageing, err := src.AgeingSeconds(ctx, bridge)
	if err != nil || ageing <= 0 {
		ageing = system.DefaultBridgeAgeingSeconds
	}
	out.OfflineAfterSeconds = ageing

	fdb, err := src.FDB(ctx, bridge)
	if err != nil {
		return nil, err
	}

	byMAC := map[string]*LANDevice{}
	get := func(mac string) *LANDevice {
		if d, ok := byMAC[mac]; ok {
			return d
		}
		d := &LANDevice{MAC: mac, IPs: []string{}, Randomized: oui.IsRandomized(mac)}
		d.Vendor, _ = oui.Lookup(mac)
		byMAC[mac] = d
		return d
	}

	for _, e := range fdb {
		d := get(e.MAC)
		age := e.Updated
		d.LastSeenSeconds = &age
		d.Port = e.Port
		d.Online = age < ageing
	}

	// Leases name devices and carry the address the box handed out. A lease
	// alone is not presence: it can outlive the device by hours.
	if leases, err := src.Leases(ctx); err == nil {
		out.LeasesOK = true
		for _, l := range leases {
			d := get(l.MAC)
			if d.Hostname == "" {
				d.Hostname = l.Hostname
			}
			expiry := l.Expiry
			d.LeaseExpiry = &expiry
			d.IPs = appendIP(d.IPs, l.IP, true)
		}
	}

	// Neighbours only fill in addresses, mostly for statics. They say nothing
	// about liveness: entries go stale within a minute and are never collected
	// at this scale.
	if ns, err := src.Neighbours(ctx, bridge); err == nil {
		out.NeighboursOK = true
		for _, n := range ns {
			if d, ok := byMAC[n.MAC]; ok {
				d.IPs = appendIP(d.IPs, n.IP, false)
			}
		}
	}

	if u.DeviceLabels != nil {
		if labels, err := u.DeviceLabels.ByMAC(ctx); err == nil {
			for mac, label := range labels {
				if d, ok := byMAC[mac]; ok {
					d.Label = label
				}
			}
		}
	}

	for _, d := range byMAC {
		out.Devices = append(out.Devices, *d)
	}
	sortDevices(out.Devices)
	return out, nil
}

// appendIP keeps the leased address first: it is the one the box handed out and
// the one the hostname belongs to.
func appendIP(ips []string, ip string, leased bool) []string {
	if ip == "" {
		return ips
	}
	for _, existing := range ips {
		if existing == ip {
			return ips
		}
	}
	if leased {
		return append([]string{ip}, ips...)
	}
	return append(ips, ip)
}

// sortDevices puts what is present first, then names, so the list does not
// reshuffle between polls.
func sortDevices(ds []LANDevice) {
	sort.Slice(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Online != b.Online {
			return a.Online
		}
		an, bn := displayName(a), displayName(b)
		if an != bn {
			return an < bn
		}
		return a.MAC < b.MAC
	})
}

func displayName(d LANDevice) string {
	switch {
	case d.Label != "":
		return d.Label
	case d.Hostname != "":
		return d.Hostname
	case d.Vendor != "":
		return d.Vendor
	}
	return d.MAC
}

// SetDeviceLabel names a device, or clears the name when label is empty.
func (u *networkUsecase) SetDeviceLabel(ctx context.Context, mac, label string) error {
	if u.DeviceLabels == nil {
		return fmt.Errorf("no device label storage configured")
	}
	normMAC, normLabel, err := domain.ValidateDeviceLabel(mac, label)
	if err != nil {
		return err
	}
	return u.DeviceLabels.Set(ctx, normMAC, normLabel)
}

func (u *networkUsecase) deviceSource() system.DeviceSource {
	if u.Devices != nil {
		return u.Devices
	}
	return system.NewDeviceSource()
}
