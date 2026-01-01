package xray

import (
	"context"
	"fmt"
	"strings"

	statsService "github.com/xtls/xray-core/app/stats/command"
)

// GetUserStats retrieves uplink/downlink statistics for a specific user on a target node
func (c *GRPCClient) GetUserStats(ctx context.Context, target string, email string, reset bool) (*UserStats, error) {
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return nil, err
	}
	defer closeFunc()

	client := statsService.NewStatsServiceClient(conn)

	// Query uplink stats
	uplinkReq := &statsService.GetStatsRequest{
		Name:   fmt.Sprintf("user>>>%s>>>traffic>>>uplink", email),
		Reset_: reset,
	}
	uplinkResp, uplinkErr := client.GetStats(ctx, uplinkReq)

	var uplink int64
	if uplinkErr != nil {
		if !strings.Contains(uplinkErr.Error(), "not found") {
			return nil, fmt.Errorf("failed to get uplink stats: %w", uplinkErr)
		}
	} else if uplinkResp.Stat != nil {
		uplink = uplinkResp.Stat.Value
	}

	// Query downlink stats
	downlinkReq := &statsService.GetStatsRequest{
		Name:   fmt.Sprintf("user>>>%s>>>traffic>>>downlink", email),
		Reset_: reset,
	}
	downlinkResp, downlinkErr := client.GetStats(ctx, downlinkReq)

	var downlink int64
	if downlinkErr != nil {
		if !strings.Contains(downlinkErr.Error(), "not found") {
			return nil, fmt.Errorf("failed to get downlink stats: %w", downlinkErr)
		}
	} else if downlinkResp.Stat != nil {
		downlink = downlinkResp.Stat.Value
	}

	return &UserStats{
		Email:    email,
		Uplink:   uplink,
		Downlink: downlink,
	}, nil
}

// QueryStats queries statistics matching a pattern on a target node
func (c *GRPCClient) QueryStats(ctx context.Context, target string, pattern string, reset bool) ([]*Stat, error) {
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return nil, err
	}
	defer closeFunc()

	req := &statsService.QueryStatsRequest{
		Pattern: pattern,
		Reset_:  reset,
	}

	client := statsService.NewStatsServiceClient(conn)
	resp, err := client.QueryStats(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query stats with pattern %s at %s: %w", pattern, target, err)
	}

	stats := make([]*Stat, 0, len(resp.Stat))
	for _, s := range resp.Stat {
		stats = append(stats, &Stat{
			Name:  s.Name,
			Value: s.Value,
		})
	}

	return stats, nil
}

// GetSysStats retrieves system-level statistics from a target node
func (c *GRPCClient) GetSysStats(ctx context.Context, target string) (*SysStats, error) {
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return nil, err
	}
	defer closeFunc()

	req := &statsService.SysStatsRequest{}

	client := statsService.NewStatsServiceClient(conn)
	resp, err := client.GetSysStats(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get system stats at %s: %w", target, err)
	}

	return &SysStats{
		NumGoroutine: resp.NumGoroutine,
		NumGC:        resp.NumGC,
		Alloc:        resp.Alloc,
		TotalAlloc:   resp.TotalAlloc,
		Sys:          resp.Sys,
		Mallocs:      resp.Mallocs,
		Frees:        resp.Frees,
		LiveObjects:  resp.LiveObjects,
		PauseTotalNs: resp.PauseTotalNs,
		Uptime:       resp.Uptime,
	}, nil
}
