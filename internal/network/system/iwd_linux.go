//go:build linux

package system

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	iwdService      = "net.connman.iwd"
	iwdStationIface = "net.connman.iwd.Station"
	iwdNetworkIface = "net.connman.iwd.Network"
	iwdKnownIface   = "net.connman.iwd.KnownNetwork"
	iwdDeviceIface  = "net.connman.iwd.Device"
	dbusObjMgrIface = "org.freedesktop.DBus.ObjectManager"
	dbusPropsIface  = "org.freedesktop.DBus.Properties"
)

type iwdObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

type iwdClient struct {
	conn *dbus.Conn
}

// NewIWDClient connects to the system bus. iwd's packaged unit is disabled at
// install so nasnet owns when it runs and the two never race for a radio.
func NewIWDClient() (StationClient, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect to the system bus: %w", err)
	}
	return &iwdClient{conn: conn}, nil
}

// managedObjects fetches the whole tree once. iwd exposes a device, a station
// and one network object per visible SSID, all keyed by path.
func (c *iwdClient) managedObjects(ctx context.Context) (iwdObjects, error) {
	var objects iwdObjects
	err := c.conn.Object(iwdService, dbus.ObjectPath("/")).
		CallWithContext(ctx, dbusObjMgrIface+".GetManagedObjects", 0).Store(&objects)
	if err != nil {
		return nil, fmt.Errorf("iwd GetManagedObjects: %w", err)
	}
	return objects, nil
}

func (c *iwdClient) stationPath(ctx context.Context, ifName string) (dbus.ObjectPath, iwdObjects, error) {
	objects, err := c.managedObjects(ctx)
	if err != nil {
		return "", nil, err
	}
	for path, ifaces := range objects {
		dev, ok := ifaces[iwdDeviceIface]
		if !ok {
			continue
		}
		name, _ := dev["Name"].Value().(string)
		if name != ifName {
			continue
		}
		if _, isStation := ifaces[iwdStationIface]; !isStation {
			return "", nil, fmt.Errorf(
				"%s is not in station mode; a radio is a station or an access point, never both", ifName)
		}
		return path, objects, nil
	}
	return "", nil, fmt.Errorf("iwd does not manage %s", ifName)
}

func (c *iwdClient) Scan(ctx context.Context, ifName string) error {
	path, _, err := c.stationPath(ctx, ifName)
	if err != nil {
		return err
	}
	call := c.conn.Object(iwdService, path).CallWithContext(ctx, iwdStationIface+".Scan", 0)
	if call.Err != nil {
		// A scan already running is success from the caller's side
		if strings.Contains(call.Err.Error(), "InProgress") {
			return nil
		}
		return fmt.Errorf("iwd scan on %s: %w", ifName, call.Err)
	}
	return nil
}

func (c *iwdClient) Networks(ctx context.Context, ifName string) ([]WifiNetwork, error) {
	path, objects, err := c.stationPath(ctx, ifName)
	if err != nil {
		return nil, err
	}

	type entry struct {
		Path   dbus.ObjectPath
		Signal int16
	}
	var entries []entry
	call := c.conn.Object(iwdService, path).
		CallWithContext(ctx, iwdStationIface+".GetOrderedNetworks", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("iwd GetOrderedNetworks: %w", call.Err)
	}
	if err := call.Store(&entries); err != nil {
		return nil, fmt.Errorf("decode ordered networks: %w", err)
	}

	connected := c.connectedNetworkPath(ctx, path)

	out := make([]WifiNetwork, 0, len(entries))
	for _, e := range entries {
		props, ok := objects[e.Path][iwdNetworkIface]
		if !ok {
			continue
		}
		n := WifiNetwork{
			// iwd reports signal in centi-dBm
			SignalDBm: int(e.Signal) / 100,
			Connected: e.Path == connected,
		}
		n.SSID, _ = props["Name"].Value().(string)
		if raw, ok := props["Type"].Value().(string); ok {
			n.Security = iwdSecurityLabel(raw)
		}
		if p, ok := props["KnownNetwork"].Value().(dbus.ObjectPath); ok {
			n.Known = p != ""
		}
		if n.SSID != "" {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SignalDBm > out[j].SignalDBm })
	return out, nil
}

func (c *iwdClient) connectedNetworkPath(ctx context.Context, station dbus.ObjectPath) dbus.ObjectPath {
	var v dbus.Variant
	err := c.conn.Object(iwdService, station).
		CallWithContext(ctx, dbusPropsIface+".Get", 0, iwdStationIface, "ConnectedNetwork").Store(&v)
	if err != nil {
		return ""
	}
	p, _ := v.Value().(dbus.ObjectPath)
	return p
}

// Connect associates with an SSID. The passphrase is pre-seeded into iwd's own
// network file rather than served over its Agent interface, which avoids a
// long-lived agent and makes the network known for autoconnect at boot.
func (c *iwdClient) Connect(ctx context.Context, ifName, ssid, psk string) error {
	if err := validateStationCredentials(ssid, psk); err != nil {
		return err
	}
	if psk != "" {
		if err := writeIWDNetworkFile(ssid, psk); err != nil {
			return err
		}
	}

	_, objects, err := c.stationPath(ctx, ifName)
	if err != nil {
		return err
	}
	target, err := networkPathForSSID(objects, ssid)
	if err != nil {
		return err
	}
	call := c.conn.Object(iwdService, target).CallWithContext(ctx, iwdNetworkIface+".Connect", 0)
	if call.Err != nil {
		return fmt.Errorf("connect %s to %q: %w", ifName, ssid, call.Err)
	}
	return nil
}

func (c *iwdClient) Disconnect(ctx context.Context, ifName string) error {
	path, _, err := c.stationPath(ctx, ifName)
	if err != nil {
		return err
	}
	call := c.conn.Object(iwdService, path).CallWithContext(ctx, iwdStationIface+".Disconnect", 0)
	if call.Err != nil {
		return fmt.Errorf("disconnect %s: %w", ifName, call.Err)
	}
	return nil
}

func (c *iwdClient) Forget(ctx context.Context, ifName, ssid string) error {
	objects, err := c.managedObjects(ctx)
	if err != nil {
		return err
	}
	for path, ifaces := range objects {
		known, ok := ifaces[iwdKnownIface]
		if !ok {
			continue
		}
		if name, _ := known["Name"].Value().(string); name == ssid {
			call := c.conn.Object(iwdService, path).CallWithContext(ctx, iwdKnownIface+".Forget", 0)
			if call.Err != nil {
				return fmt.Errorf("forget %q: %w", ssid, call.Err)
			}
			break
		}
	}
	return removeIWDNetworkFile(ssid)
}

func (c *iwdClient) State(ctx context.Context, ifName string) (string, error) {
	path, _, err := c.stationPath(ctx, ifName)
	if err != nil {
		return "", err
	}
	var v dbus.Variant
	err = c.conn.Object(iwdService, path).
		CallWithContext(ctx, dbusPropsIface+".Get", 0, iwdStationIface, "State").Store(&v)
	if err != nil {
		return "", fmt.Errorf("read %s state: %w", ifName, err)
	}
	s, _ := v.Value().(string)
	return s, nil
}

func networkPathForSSID(objects iwdObjects, ssid string) (dbus.ObjectPath, error) {
	for path, ifaces := range objects {
		net, ok := ifaces[iwdNetworkIface]
		if !ok {
			continue
		}
		if name, _ := net["Name"].Value().(string); name == ssid {
			return path, nil
		}
	}
	return "", fmt.Errorf("network %q not visible; scan first", ssid)
}
