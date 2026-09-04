package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

var (
	ErrValidationFailed = errors.New("validation failed")
	ErrConfirmRequired  = errors.New("confirmation required")
)

// RenderPortForwards turns the rows into nat_pre rules, regenerating the whole
// chain. An empty UplinkKey means any uplink: a rule names an interface.
func RenderPortForwards(rows []domain.PortForward, uplinks []Uplink) []nft.PortForward {
	byKey := map[string]Uplink{}
	for _, u := range uplinks {
		byKey[u.Key] = u
	}

	var out []nft.PortForward
	for _, r := range rows {
		if !r.Enabled {
			continue
		}
		targets := uplinks
		if r.UplinkKey != "" {
			u, ok := byKey[r.UplinkKey]
			if !ok {
				// Unassigned since this row was written. Skip rather than name a
				// device with no role.
				continue
			}
			targets = []Uplink{u}
		}
		for _, u := range targets {
			out = append(out, nft.PortForward{
				IfName: u.IfName, Proto: r.Proto, DPort: r.DPort,
				ToAddr: r.ToAddr, ToPort: r.ToPort, Comment: r.Comment,
			})
		}
	}
	return out
}

// ApplyPortForwards renders the nat_pre chain. Independent of the LAN: a
// forward may target the box itself.
func ApplyPortForwards(ctx context.Context, m *nft.Manager,
	rows []domain.PortForward, uplinks []Uplink) error {
	return m.Update(ctx, func(rs *nft.Ruleset) {
		rs.PortForwards = RenderPortForwards(rows, uplinks)
	})
}

func (u *networkUsecase) ListPortForwards(ctx context.Context) ([]domain.PortForward, error) {
	if u.PFRepo == nil {
		return []domain.PortForward{}, nil
	}
	rows, err := u.PFRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []domain.PortForward{}
	}
	return rows, nil
}

// CreatePortForward validates then re-renders nat_pre. A V28 confirm returns its
// own error so the handler can 409. Applies at once: no addressing changes.
func (u *networkUsecase) CreatePortForward(ctx context.Context, pf domain.PortForward,
	confirmed bool) ([]domain.Verdict, error) {

	verdicts, err := u.checkPortForward(ctx, pf, confirmed)
	if err != nil {
		return verdicts, err
	}
	if err := u.PFRepo.Create(ctx, &pf); err != nil {
		return verdicts, err
	}
	return verdicts, u.reapplyPortForwards(ctx)
}

func (u *networkUsecase) UpdatePortForward(ctx context.Context, pf domain.PortForward,
	confirmed bool) ([]domain.Verdict, error) {

	verdicts, err := u.checkPortForward(ctx, pf, confirmed)
	if err != nil {
		return verdicts, err
	}
	if err := u.PFRepo.Update(ctx, &pf); err != nil {
		return verdicts, err
	}
	return verdicts, u.reapplyPortForwards(ctx)
}

func (u *networkUsecase) DeletePortForward(ctx context.Context, id uint) error {
	if u.PFRepo == nil {
		return fmt.Errorf("no port-forward storage configured")
	}
	if err := u.PFRepo.Delete(ctx, id); err != nil {
		return err
	}
	return u.reapplyPortForwards(ctx)
}

func (u *networkUsecase) checkPortForward(ctx context.Context, pf domain.PortForward,
	confirmed bool) ([]domain.Verdict, error) {

	if u.PFRepo == nil {
		return nil, fmt.Errorf("no port-forward storage configured")
	}
	in, err := u.portForwardInput(ctx, pf, confirmed)
	if err != nil {
		return nil, err
	}
	verdicts := domain.ValidatePortForward(in)
	if verdicts == nil {
		verdicts = []domain.Verdict{}
	}
	if domain.Rejected(verdicts) {
		return verdicts, ErrValidationFailed
	}
	if !confirmed && needsConfirm(verdicts) {
		return verdicts, ErrConfirmRequired
	}
	return verdicts, nil
}

func (u *networkUsecase) portForwardInput(ctx context.Context, pf domain.PortForward,
	confirmed bool) (domain.PortForwardInput, error) {

	existing, err := u.PFRepo.List(ctx)
	if err != nil {
		return domain.PortForwardInput{}, err
	}
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return domain.PortForwardInput{}, err
	}
	keys := make([]string, 0, len(uplinks))
	for _, up := range uplinks {
		keys = append(keys, up.Key)
	}

	in := domain.PortForwardInput{
		Existing: existing, New: pf,
		PanelPort: u.PanelPort, SSHPort: 22,
		InboundPorts: u.inboundPorts(ctx),
		UplinkKeys:   keys, Confirmed: confirmed,
	}
	if lan := u.lanConfig(ctx); lan != nil {
		in.LANCIDR = lan.CIDR
	}
	return in, nil
}

// inboundPorts is proto -> ports for the enabled inbounds. Empty with no source
// wired, which only loosens V27 — filter_in is the enforcement point.
func (u *networkUsecase) inboundPorts(ctx context.Context) map[string][]int {
	return u.inboundPortsWhere(ctx, func(InboundSpec) bool { return true })
}

// autoMappedInboundPorts is the subset the upstream mapper asks for by itself.
// A local-only inbound is missing here on purpose: nothing maps it, so a manual
// rule for it is the operator's one way to expose it, not a duplicate.
func (u *networkUsecase) autoMappedInboundPorts(ctx context.Context) map[string][]int {
	return u.inboundPortsWhere(ctx, func(s InboundSpec) bool { return !s.NoAutoMap })
}

func (u *networkUsecase) inboundPortsWhere(ctx context.Context, keep func(InboundSpec) bool) map[string][]int {
	out := map[string][]int{}
	if u.Inbounds == nil {
		return out
	}
	specs, err := u.Inbounds.EnabledInbounds(ctx)
	if err != nil {
		return out
	}
	for _, s := range specs {
		if s.Enabled && s.Port > 0 && keep(s) {
			out[s.Proto] = append(out[s.Proto], s.Port)
		}
	}
	return out
}

// reapplyPortForwards regenerates the whole chain from the table. Port forwards
// change the input surface too, so filter_in is re-derived in the same pass.
func (u *networkUsecase) reapplyPortForwards(ctx context.Context) error {
	rows, err := u.PFRepo.List(ctx)
	if err != nil {
		return err
	}
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return err
	}
	if err := ApplyPortForwards(ctx, u.Nft, rows, uplinks); err != nil {
		return err
	}
	return u.reapplyFilterInput(ctx)
}

func needsConfirm(vs []domain.Verdict) bool {
	for _, v := range vs {
		if v.Level == domain.LevelConfirm {
			return true
		}
	}
	return false
}
