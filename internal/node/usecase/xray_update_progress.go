package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

type xrayProgressKey struct{}

// WithXrayUpdateProgress observes real operation boundaries, never simulated progress.
func WithXrayUpdateProgress(ctx context.Context, report func(string, string)) context.Context {
	return context.WithValue(ctx, xrayProgressKey{}, report)
}
func reportXrayUpdate(ctx context.Context, stage, detail string) {
	if report, ok := ctx.Value(xrayProgressKey{}).(func(string, string)); ok {
		report(stage, detail)
	}
}

func matchesXrayVersion(reported, requested string) bool {
	fields := strings.Fields(reported)
	if len(fields) > 1 && strings.EqualFold(fields[0], "Xray") {
		reported = fields[1]
	}
	return strings.TrimPrefix(reported, "v") == strings.TrimPrefix(requested, "v")
}

func verifyXrayUpdate(ctx context.Context, client agent.NodeClient, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var reason = "node did not return a healthy status"
	for {
		status, err := client.GetStatus(ctx)
		if err == nil && status != nil {
			if status.XrayRunning && matchesXrayVersion(status.XrayVersion, version) {
				return nil
			}
			reason = fmt.Sprintf("running=%t, reported version=%q", status.XrayRunning, status.XrayVersion)
		} else if err != nil {
			reason = err.Error()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("update completed but health verification failed: %s; check node status before retrying", reason)
		case <-time.After(2 * time.Second):
		}
	}
}
