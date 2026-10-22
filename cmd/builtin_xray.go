package cmd

import (
	"context"
	"os/exec"
	"runtime"
	"time"

	agentconfig "github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	xrayUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/xray/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
)

// builtinXrayPath picks what the built-in node runs: the cached build of the
// picked version, or of the tested one when nothing is picked. Without a
// cached build it stays on installed, the binary the installer put in place.
func builtinXrayPath(ctx context.Context, settings interface {
	GetByKey(ctx context.Context, key string) (string, error)
}, installed string) string {
	bm := xrayUsecase.NewBinaryManager(xrayUsecase.CacheDir, nil)
	version, _ := settings.GetByKey(ctx, settingDomain.BuiltinXrayKey)
	if version == "" {
		version = pin.Version
	}
	p, err := bm.VerifiedPath(version, runtime.GOARCH)
	if err != nil {
		logger.GetLogger().WithError(err).WithField("fallback", installed).Info("Built-in node runs the installed xray")
		return installed
	}
	return p
}

// adoptInstalledXray keeps an xray newer than the tested one that was put
// in place by hand before the cache existed, so the first boot on the cache
// doesn't step it back. It runs once, while the setting has no row yet.
func adoptInstalledXray(ctx context.Context, repo interface {
	GetByKey(ctx context.Context, key string) (*settingDomain.Setting, error)
	Update(ctx context.Context, s *settingDomain.Setting) error
}, installed string) {
	if _, err := repo.GetByKey(ctx, settingDomain.BuiltinXrayKey); err == nil {
		return
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probe, installed, "version").Output()
	if err != nil {
		return
	}
	v := nodeUC.XrayVersionNumber(string(out))
	have, err := release.Parse("v" + v)
	if err != nil || !have.Newer(release.MustParse("v"+pin.Version)) {
		return
	}
	if err := repo.Update(ctx, settingDomain.BuiltinXraySetting(v)); err != nil {
		logger.GetLogger().WithError(err).Warn("Could not keep the installed xray version")
		return
	}
	logger.GetLogger().WithField("version", v).Info("Built-in node keeps its installed xray")
}

// installedXray is where the installer puts xray.
func installedXray() string { return agentconfig.DefaultConfig().Xray.BinaryPath }
