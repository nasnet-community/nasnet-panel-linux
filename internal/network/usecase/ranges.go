package usecase

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
)

// rangesCacheFile holds the last accepted fetch, so a restart need not refetch.
const rangesCacheFile = "ir-ranges.json"

func (u *networkUsecase) rangesCachePath() string {
	return filepath.Join(u.Paths.StateDir, rangesCacheFile)
}

// RefreshDomesticRanges installs the upstream list if it passes the retention
// floor. The embedded file underneath means upstream only adds freshness.
func (u *networkUsecase) RefreshDomesticRanges(ctx context.Context) error {
	url := u.RangesURL
	if url == "" {
		url = geoip.DefaultRangesURL
	}

	fresh, err := geoip.FetchCIDRs(ctx, u.RangesClient, geoip.FetchConfig{
		BaseURL: url, UserID: u.RangesUserID,
	})
	if err != nil {
		return err
	}

	// Compare against what is live, embedded included: that is what we replace.
	current, _, err := u.domesticSets(ctx)
	if err != nil {
		return err
	}
	currentCount := 0
	for _, s := range current {
		if s.Name == DomesticSetV4 {
			currentCount = len(s.Elements)
		}
	}
	if err := geoip.AcceptRefresh(len(fresh), currentCount); err != nil {
		return err
	}

	if err := geoip.SaveCachedRanges(u.rangesCachePath(), &geoip.CachedRanges{
		FetchedAt: time.Now(), Source: url, V4: fresh,
	}); err != nil {
		return fmt.Errorf("save domestic ranges: %w", err)
	}

	// Drop the compiled sets so the next build picks the new list up.
	u.setsMu.Lock()
	u.setsBuilt = false
	u.setsMu.Unlock()

	return u.reapplyDomesticSets(ctx)
}

// Pushes fresh sets into the table. Nothing references them with no LAN.
func (u *networkUsecase) reapplyDomesticSets(ctx context.Context) error {
	lan := u.lanConfig(ctx)
	if lan == nil || !lan.Enabled {
		return nil
	}
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return err
	}
	sets, _, err := u.domesticSets(ctx)
	if err != nil {
		return err
	}
	return ApplyLANNftState(ctx, u.Nft, lan, uplinks, sets, u.vpnRouteState(ctx))
}

// StartRangesRefreshLoop refreshes on the same cadence the MikroTik build uses.
// The first attempt waits: at boot the uplinks are often still coming up.
func (u *networkUsecase) StartRangesRefreshLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = geoip.RefreshInterval
	}
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(rangesFirstAttemptDelay):
		}

		for {
			if u.rangesDue(interval) {
				if err := u.RefreshDomesticRanges(ctx); err != nil {
					u.emitRangesWarning(err)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(rangesCheckInterval):
			}
		}
	}()
}

const (
	// rangesFirstAttemptDelay lets the uplinks settle before the first fetch.
	rangesFirstAttemptDelay = 2 * time.Minute
	// rangesCheckInterval is how often the age is examined, not how often we
	// fetch. A ticker would leave a box that was off for a week un-refreshed.
	rangesCheckInterval = 1 * time.Hour
)

func (u *networkUsecase) rangesDue(interval time.Duration) bool {
	c, err := geoip.LoadCachedRanges(u.rangesCachePath())
	if err != nil || c == nil {
		return true
	}
	return c.Age() >= interval
}

func (u *networkUsecase) emitRangesWarning(err error) {
	u.emit("wan.lease_warning", map[string]any{
		"rule": "geoip", "level": "warn",
		"message": "could not refresh the domestic address list: " + err.Error(),
	})
}
