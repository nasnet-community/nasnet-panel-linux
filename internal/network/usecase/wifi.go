package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

// RadioView is one radio as the UI sees it: what it can do, what it is doing,
// and what the regulatory domain permits.
type RadioView struct {
	Phy    string `json:"phy"`
	IfName string `json:"if_name"`
	Key    string `json:"key"`
	// InterfaceID is what EnableAP takes, so the UI needs no second lookup
	InterfaceID uint   `json:"interface_id"`
	Role        string `json:"role"`
	Mode        string `json:"mode"` // "ap" | "station" | ""

	SupportsAP  bool `json:"supports_ap"`
	SupportsSTA bool `json:"supports_sta"`

	Bands map[system.Band][]system.Channel `json:"bands"`

	CountryCode    string `json:"country_code"`
	CountryCodeSet bool   `json:"country_code_set"`
	AXSupported    bool   `json:"ax_supported"`
	SAESupported   bool   `json:"sae_supported"`

	// SiblingRole is a role held by another interface on the same radio. When
	// set the other mode is unavailable, and the UI has to say why rather than
	// silently disabling a control.
	SiblingRole string `json:"sibling_role"`

	// Config is the stored intent, PSK elided by the model's json:"-"
	Config *domain.WifiConfig `json:"config,omitempty"`
}

// Radios reports every radio with its probed capability and current role
func (u *networkUsecase) Radios(ctx context.Context) ([]RadioView, error) {
	if u.RadioProber == nil {
		return []RadioView{}, nil
	}
	caps, err := u.RadioProber.Radios(ctx)
	if err != nil {
		return nil, fmt.Errorf("probe radios: %w", err)
	}
	rows, err := u.IfRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	country, _ := system.ReadRegDomain(ctx)
	ax := system.HostapdSupportsAX(ctx, "")
	sae := system.HostapdSupportsSAE(ctx, "")

	byPhy := map[string][]domain.NetworkInterface{}
	for _, r := range rows {
		if r.PhyName != "" {
			byPhy[r.PhyName] = append(byPhy[r.PhyName], r)
		}
	}

	out := make([]RadioView, 0, len(caps))
	for _, c := range caps {
		v := RadioView{
			Phy: c.Phy, SupportsAP: c.SupportsAP, SupportsSTA: c.SupportsSTA,
			Bands: c.Bands, CountryCode: country,
			CountryCodeSet: !system.RegDomainIsUnset(country),
			AXSupported:    ax, SAESupported: sae,
		}
		for _, r := range byPhy[c.Phy] {
			if v.IfName == "" {
				v.IfName, v.Key, v.Role, v.InterfaceID = r.IfName, r.Key, string(r.Role), r.ID
				switch r.Role {
				case domain.RoleLAN, domain.RoleLANMember:
					v.Mode = "ap"
				case domain.RoleWAN:
					v.Mode = "station"
				}
				if u.WifiRepo != nil {
					if cfg, cerr := u.WifiRepo.GetByInterface(ctx, r.ID); cerr == nil {
						v.Config = cfg
					}
				}
				continue
			}
			if r.Role != domain.RoleUnassigned {
				v.SiblingRole = string(r.Role)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// wantsLANMemberUnit reports whether renderAll should write a Bridge= unit for a
// LAN member. Radios are enslaved by hostapd, so a unit would fight it.
func wantsLANMemberUnit(in domain.NetworkInterface) bool {
	return !isRadio(in)
}

func isRadio(in domain.NetworkInterface) bool {
	return strings.HasPrefix(in.Source, "wifi_")
}

// wantsAP reports whether this row should be beaconing. An AP with no bridge
// has no DHCP, no DNS and no route out, so the LAN gates it.
func wantsAP(in domain.NetworkInterface, cfg *domain.WifiConfig, lan *domain.LANConfig) bool {
	if !isRadio(in) || !in.Present || cfg == nil || !cfg.Enabled || cfg.Mode != "ap" {
		return false
	}
	if in.Role != domain.RoleLAN && in.Role != domain.RoleLANMember {
		return false
	}
	return lan != nil && lan.Enabled
}

func wantsStation(in domain.NetworkInterface, cfg *domain.WifiConfig) bool {
	return isRadio(in) && in.Present && cfg != nil && cfg.Enabled &&
		cfg.Mode == "station" && in.Role == domain.RoleWAN
}

func hostapdConfigFor(in domain.NetworkInterface, cfg domain.WifiConfig, bridge string) system.HostapdConfig {
	return system.HostapdConfig{
		IfName: in.IfName, BridgeName: bridge,
		SSID: cfg.SSID, PSK: cfg.PSK, CountryCode: cfg.CountryCode,
		Band: system.Band(cfg.Band), Channel: cfg.Channel, Hidden: cfg.Hidden,
		// Asked for; HostapdSupportsAX settles it against the binary
		EnableAX: true,
	}
}

// applyWifi makes the daemons match the stored intent. Runs inside Reconcile
// after applyLAN: hostapd needs the bridge up before it can enslave into it.
func (u *networkUsecase) applyWifi(ctx context.Context, lan *domain.LANConfig,
	rows []domain.NetworkInterface) error {

	if u.WifiRepo == nil || u.RadioProber == nil {
		return nil
	}
	cfgs, err := u.WifiRepo.List(ctx)
	if err != nil {
		return err
	}
	byIface := map[uint]*domain.WifiConfig{}
	for i := range cfgs {
		byIface[cfgs[i].InterfaceID] = &cfgs[i]
	}

	apServed, stationWanted := false, false
	for _, r := range rows {
		cfg := byIface[r.ID]
		switch {
		// One config file means one AP; EnableAP refuses a second enabled row
		case !apServed && wantsAP(r, cfg, lan):
			caps, err := u.RadioProber.RadioFor(ctx, r.PhyName)
			if err != nil {
				return fmt.Errorf("radio %s: %w", r.PhyName, err)
			}
			if err := u.hostapd.Ensure(ctx, hostapdConfigFor(r, *cfg, lan.BridgeName), *caps); err != nil {
				return fmt.Errorf("access point on %s: %w", r.IfName, err)
			}
			apServed = true
		case wantsStation(r, cfg):
			// iwd autoconnects known networks, so running is all it needs
			stationWanted = true
		}
	}

	if !apServed {
		if err := u.hostapd.Stop(ctx); err != nil {
			return err
		}
	}
	return system.EnsureUnitActive(ctx, "iwd", stationWanted)
}

// describeWifiRoleError explains why an operation does not apply to a radio in
// its current role. A radio is a station or an access point, never both.
func describeWifiRoleError(ifName, role string) string {
	switch role {
	case string(domain.RoleLAN), string(domain.RoleLANMember):
		return fmt.Sprintf("%s is an access point, so it cannot scan for networks. "+
			"A radio is a station or an access point, never both — getting both needs "+
			"a second radio or a USB adapter.", ifName)
	case string(domain.RoleWAN):
		return fmt.Sprintf("%s is a station uplink, so it cannot beacon.", ifName)
	}
	return fmt.Sprintf("%s holds no Wi-Fi role; assign one on the Ports tab first", ifName)
}

// wifiRow finds the interface a wifi call targets and rejects non-radios
func (u *networkUsecase) wifiRow(ctx context.Context,
	match func(domain.NetworkInterface) bool) (*domain.NetworkInterface, error) {

	rows, err := u.IfRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if !match(rows[i]) {
			continue
		}
		if !isRadio(rows[i]) {
			return nil, fmt.Errorf("%s is not a radio", rows[i].IfName)
		}
		return &rows[i], nil
	}
	return nil, fmt.Errorf("no such interface")
}

// otherEnabledAP names an interface already running an AP. One hostapd config
// file means one AP, so refuse the second where the operator is.
func otherEnabledAP(cfgs []domain.WifiConfig, exceptIface uint) (uint, bool) {
	for i := range cfgs {
		c := cfgs[i]
		if c.Enabled && c.Mode == "ap" && c.InterfaceID != exceptIface {
			return c.InterfaceID, true
		}
	}
	return 0, false
}

// EnableAP saves the intent and applies. Reconcile starts hostapd; the
// dead-man's snapshot, files and rows both, is the undo.
func (u *networkUsecase) EnableAP(ctx context.Context, cfg domain.WifiConfig) ([]domain.Verdict, *ApplyView, error) {
	if u.WifiRepo == nil {
		return nil, nil, fmt.Errorf("no wifi storage configured")
	}
	row, err := u.wifiRow(ctx, func(r domain.NetworkInterface) bool { return r.ID == cfg.InterfaceID })
	if err != nil {
		return nil, nil, err
	}
	if row.Role != domain.RoleLAN && row.Role != domain.RoleLANMember {
		return nil, nil, fmt.Errorf("%s", describeWifiRoleError(row.IfName, string(row.Role)))
	}

	existing, err := u.WifiRepo.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	if other, found := otherEnabledAP(existing, cfg.InterfaceID); found {
		return nil, nil, fmt.Errorf(
			"one access point at a time: interface %d already runs one; disable it first", other)
	}

	stored, err := u.WifiRepo.GetByInterface(ctx, cfg.InterfaceID)
	if err != nil {
		return nil, nil, err
	}
	// An empty PSK on an edit keeps the stored one; the UI never holds the secret
	if cfg.PSK == "" {
		cfg.PSK = stored.PSK
	}
	cfg.ID, cfg.Mode, cfg.Enabled = stored.ID, "ap", true

	verdicts := domain.ValidateWifiConfig(cfg)
	if verdicts == nil {
		verdicts = []domain.Verdict{}
	}
	if domain.Rejected(verdicts) {
		return verdicts, nil, nil
	}

	saved := cfg
	var plan system.Plan
	if !system.TakeoverDone(u.Paths) {
		plan.Ops = append(plan.Ops, system.TakeoverOps(u.Paths)...)
	}
	plan.Ops = append(plan.Ops,
		system.Op{
			Desc: fmt.Sprintf("enable the access point %q on %s", cfg.SSID, row.IfName),
			Do:   func(ctx context.Context) error { return u.WifiRepo.Save(ctx, &saved) },
		},
		system.Op{
			Desc: "render networkd units, rt_tables and the sysctl drop-in",
			Do:   func(ctx context.Context) error { return u.renderAll(ctx) },
		},
		system.Op{
			Desc: "set the regulatory domain, start hostapd bridged into the LAN",
			Do:   func(ctx context.Context) error { return u.Reconcile(ctx) },
		},
	)

	prev := *stored
	rec, err := u.applier.Apply(ctx, plan, !system.TakeoverDone(u.Paths))
	if err != nil {
		// The snapshot does not cover a failed apply's row, so put it back or the
		// next reconcile resurrects the AP.
		if saveErr := u.WifiRepo.Save(ctx, &prev); saveErr != nil {
			return verdicts, nil, fmt.Errorf("%w (and the wifi row still says enabled: %v)", err, saveErr)
		}
		return verdicts, nil, err
	}
	return verdicts, applyViewOf(rec), nil
}

// DisableWifi turns the radio's intent off; Reconcile stops the daemon
func (u *networkUsecase) DisableWifi(ctx context.Context, key string) (*ApplyView, error) {
	if u.WifiRepo == nil {
		return nil, fmt.Errorf("no wifi storage configured")
	}
	row, err := u.wifiRow(ctx, func(r domain.NetworkInterface) bool { return r.Key == key })
	if err != nil {
		return nil, err
	}
	stored, err := u.WifiRepo.GetByInterface(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	cfg := *stored
	cfg.Enabled = false

	plan := system.Plan{Ops: []system.Op{
		{
			Desc: fmt.Sprintf("disable wifi on %s", row.IfName),
			Do:   func(ctx context.Context) error { return u.WifiRepo.Save(ctx, &cfg) },
		},
		{
			Desc: "stop the daemon and settle routing",
			Do:   func(ctx context.Context) error { return u.Reconcile(ctx) },
		},
	}}
	rec, err := u.applier.Apply(ctx, plan, false)
	if err != nil {
		return nil, err
	}
	return applyViewOf(rec), nil
}

// ScanWifi is read-only and does not ride the apply. iwd has to be up to scan,
// and starting it is safe: it manages nothing until told to connect.
func (u *networkUsecase) ScanWifi(ctx context.Context, key string) ([]system.WifiNetwork, error) {
	if u.Station == nil {
		return nil, fmt.Errorf("no station support on this platform")
	}
	row, err := u.wifiRow(ctx, func(r domain.NetworkInterface) bool { return r.Key == key })
	if err != nil {
		return nil, err
	}
	if row.Role != domain.RoleWAN {
		return nil, fmt.Errorf("%s", describeWifiRoleError(row.IfName, string(row.Role)))
	}
	if err := system.EnsureUnitActive(ctx, "iwd", true); err != nil {
		return nil, err
	}
	if err := u.Station.Scan(ctx, row.IfName); err != nil {
		return nil, err
	}
	// iwd scans asynchronously. A stale-but-instant list beats a spinner that
	// never resolves, so wait once and read what it has.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(scanSettle):
	}
	return u.Station.Networks(ctx, row.IfName)
}

// scanSettle is one wait, not a poll: iwd keeps its last results either way
const scanSettle = 3 * time.Second

// ConnectWifi saves the station intent and applies. Association rides the
// dead-man, so joining a network that breaks reachability gets disconnected.
func (u *networkUsecase) ConnectWifi(ctx context.Context, key, ssid, psk string) (*ApplyView, error) {
	if u.Station == nil {
		return nil, fmt.Errorf("no station support on this platform")
	}
	if u.WifiRepo == nil {
		return nil, fmt.Errorf("no wifi storage configured")
	}
	row, err := u.wifiRow(ctx, func(r domain.NetworkInterface) bool { return r.Key == key })
	if err != nil {
		return nil, err
	}
	if row.Role != domain.RoleWAN {
		return nil, fmt.Errorf("%s", describeWifiRoleError(row.IfName, string(row.Role)))
	}

	stored, err := u.WifiRepo.GetByInterface(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	cfg := *stored
	cfg.Mode, cfg.SSID, cfg.PSK, cfg.Enabled = "station", ssid, psk, true
	if vs := domain.ValidateWifiConfig(cfg); domain.Rejected(vs) {
		return nil, fmt.Errorf("%s", vs[0].Message)
	}

	ifName := row.IfName
	plan := system.Plan{Ops: []system.Op{
		{
			Desc: fmt.Sprintf("save the uplink network %q for %s", ssid, ifName),
			Do:   func(ctx context.Context) error { return u.WifiRepo.Save(ctx, &cfg) },
		},
		{
			Desc: fmt.Sprintf("connect %s to %q", ifName, ssid),
			Do: func(ctx context.Context) error {
				if err := system.EnsureUnitActive(ctx, "iwd", true); err != nil {
					return err
				}
				return u.Station.Connect(ctx, ifName, ssid, psk)
			},
		},
		{
			Desc: "settle addressing and routing on the new uplink",
			Do:   func(ctx context.Context) error { return u.Reconcile(ctx) },
		},
	}}
	rec, err := u.applier.Apply(ctx, plan, false)
	if err != nil {
		return nil, err
	}
	return applyViewOf(rec), nil
}

// applyViewOf is the tail every apply-riding method shares
func applyViewOf(rec *domain.ApplyRecord) *ApplyView {
	ops := rec.Ops
	if ops == nil {
		ops = []string{}
	}
	view := &ApplyView{PlanID: rec.ID, Ops: ops}
	if rec.Deadline != nil {
		view.ConfirmDeadlineUnix = rec.Deadline.Unix()
	}
	return view
}
