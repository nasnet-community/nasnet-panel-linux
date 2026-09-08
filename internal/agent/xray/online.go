package xray

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"
)

const onlineIPConcurrency = 4

// CollectOnlineIPs makes a bounded sweep over a shared Xray connection. Failed
// or cancelled sweeps return no snapshot, so unknown data never becomes an empty
// (offline) user map. The background sampler retains the last completed sample
// for its bounded freshness window.
func (c *LocalClient) CollectOnlineIPs(ctx context.Context) (map[string]map[string]int64, error) {
	emails, err := c.GetAllOnlineUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := make(map[string]map[string]int64, len(emails))
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(onlineIPConcurrency)
	for _, email := range emails {
		if ctx.Err() != nil {
			break
		}
		g.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			ips, err := c.GetUserOnlineIPs(ctx, email)
			if err != nil {
				return fmt.Errorf("online IPs for %s: %w", email, err)
			}
			if ips == nil {
				ips = make(map[string]int64)
			}
			mu.Lock()
			users[email] = ips
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	// errgroup cancels its context on Wait; all users must have completed.
	if len(users) != len(emails) {
		return nil, fmt.Errorf("online IP sweep incomplete")
	}
	return users, nil
}
