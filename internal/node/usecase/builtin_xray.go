package usecase

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
)

// BuiltinXray is the version picked for the built-in node; empty while it
// follows the panel.
func (u *nodeUsecase) BuiltinXray(ctx context.Context) string {
	if u.settingUC == nil {
		return ""
	}
	v, _ := u.settingUC.GetByKey(ctx, settingDomain.BuiltinXrayKey)
	return v
}

// switchBuiltinXray runs the built-in node on a cached build and remembers
// the pick. Picking the tested version goes back to following the panel.
// Nothing outside the cache is written.
func (u *nodeUsecase) switchBuiltinXray(ctx context.Context, nodeID uint, version string) error {
	cache, ok := u.xrayBM.(interface {
		VerifiedPath(version, arch string) (string, error)
	})
	if !ok {
		return errors.New("the panel's Xray cache is not ready")
	}
	arch := runtime.GOARCH

	route := "direct"
	if u.httpFactory != nil {
		route = u.httpFactory.RouteFor(httpclient.FeatureXrayBinary, "https://github.com")
	}
	reportXrayUpdate(ctx, "downloading", fmt.Sprintf("Downloading into the panel cache (GitHub route: %s).", route))
	if err := u.xrayBM.EnsureCached(version, arch); err != nil {
		return fmt.Errorf("failed to download xray %s: %w", version, err)
	}
	path, err := cache.VerifiedPath(version, arch)
	if err != nil {
		return err
	}

	reportXrayUpdate(ctx, "installing", "Restarting Xray on the new build.")
	reported, err := u.embeddedSrv.SwitchXray(path)
	if err != nil {
		return fmt.Errorf("failed to update xray version: %w", err)
	}
	u.forgetAgentVersion(nodeID)

	picked := version
	if version == pin.Version {
		picked = ""
	}
	if u.settingUC != nil {
		ctx := settingDomain.AllowProtectedWrite(ctx)
		if err := u.settingUC.UpdateMany(ctx, []*settingDomain.Setting{settingDomain.BuiltinXraySetting(picked)}); err != nil {
			return fmt.Errorf("xray runs %s but the choice wasn't saved: %w", version, err)
		}
	}
	reportXrayUpdate(ctx, "verifying", fmt.Sprintf("Xray reports %s.", reported))
	return nil
}
