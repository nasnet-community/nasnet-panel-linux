package xray

import (
	"context"
	"fmt"
	"strings"
	"time"

	handlerService "github.com/xtls/xray-core/app/proxyman/command"
	statsService "github.com/xtls/xray-core/app/stats/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	hysteriaAccount "github.com/xtls/xray-core/proxy/hysteria/account"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vmess"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// LocalClient wraps communication with the local xray-core gRPC API
type LocalClient struct {
	addr    string
	timeout time.Duration
}

// NewLocalClient creates a new client to talk to local xray-core API
func NewLocalClient(addr string, timeout time.Duration) *LocalClient {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &LocalClient{
		addr:    addr,
		timeout: timeout,
	}
}

// dial establishes a connection to the local xray API
func (c *LocalClient) dial(ctx context.Context) (*grpc.ClientConn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	conn, err := grpc.DialContext(
		dialCtx,
		c.addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to local xray API at %s: %w", c.addr, err)
	}
	return conn, nil
}

// AddUser adds a user to an inbound handler
func (c *LocalClient) AddUser(ctx context.Context, inboundTag, email, uuid, protocolType, flow, encryption string, level int32) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := handlerService.NewHandlerServiceClient(conn)

	// Build user based on protocol
	user := &protocol.User{
		Level: uint32(level),
		Email: email,
	}

	switch strings.ToLower(protocolType) {
	case "vless":
		user.Account = serial.ToTypedMessage(&vless.Account{
			Id:         uuid,
			Flow:       flow,
			Encryption: encryption,
		})
	case "vmess":
		user.Account = serial.ToTypedMessage(&vmess.Account{
			Id: uuid,
		})
	case "trojan":
		user.Account = serial.ToTypedMessage(&trojan.Account{
			Password: uuid, // Trojan uses password, not UUID
		})
	case "hysteria2", "hysteria":
		user.Account = serial.ToTypedMessage(&hysteriaAccount.Account{
			Auth: uuid,
		})
	default:
		return fmt.Errorf("unsupported protocol: %s", protocolType)
	}

	addUserOp := &handlerService.AddUserOperation{
		User: user,
	}

	req := &handlerService.AlterInboundRequest{
		Tag:       inboundTag,
		Operation: serial.ToTypedMessage(addUserOp),
	}

	_, err = client.AlterInbound(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to add user %s: %w", email, err)
	}
	return nil
}

// RemoveUser removes a user from an inbound handler
func (c *LocalClient) RemoveUser(ctx context.Context, inboundTag, email string) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := handlerService.NewHandlerServiceClient(conn)

	removeUserOp := &handlerService.RemoveUserOperation{
		Email: email,
	}

	req := &handlerService.AlterInboundRequest{
		Tag:       inboundTag,
		Operation: serial.ToTypedMessage(removeUserOp),
	}

	_, err = client.AlterInbound(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to remove user %s: %w", email, err)
	}
	return nil
}

// User represents a user in Xray
type User struct {
	Email string
	Level uint32
}

// GetInboundUsers retrieves users from an inbound handler
func (c *LocalClient) GetInboundUsers(ctx context.Context, inboundTag string) ([]*User, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := handlerService.NewHandlerServiceClient(conn)

	req := &handlerService.GetInboundUserRequest{
		Tag: inboundTag,
	}

	resp, err := client.GetInboundUsers(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get users from inbound %s: %w", inboundTag, err)
	}

	users := make([]*User, 0, len(resp.Users))
	for _, u := range resp.Users {
		users = append(users, &User{
			Email: u.Email,
			Level: u.Level,
		})
	}

	return users, nil
}

// UserStats holds user traffic statistics
type UserStats struct {
	Email    string
	Uplink   int64
	Downlink int64
}

// GetUserStats retrieves traffic statistics for a user
func (c *LocalClient) GetUserStats(ctx context.Context, email string, reset bool) (*UserStats, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := statsService.NewStatsServiceClient(conn)

	// Get uplink
	uplinkReq := &statsService.GetStatsRequest{
		Name:   fmt.Sprintf("user>>>%s>>>traffic>>>uplink", email),
		Reset_: reset,
	}
	uplinkResp, err := client.GetStats(ctx, uplinkReq)
	var uplink int64
	if err == nil && uplinkResp.Stat != nil {
		uplink = uplinkResp.Stat.Value
	}

	// Get downlink
	downlinkReq := &statsService.GetStatsRequest{
		Name:   fmt.Sprintf("user>>>%s>>>traffic>>>downlink", email),
		Reset_: reset,
	}
	downlinkResp, err := client.GetStats(ctx, downlinkReq)
	var downlink int64
	if err == nil && downlinkResp.Stat != nil {
		downlink = downlinkResp.Stat.Value
	}

	return &UserStats{
		Email:    email,
		Uplink:   uplink,
		Downlink: downlink,
	}, nil
}

// XrayStats holds aggregated traffic statistics
type XrayStats struct {
	UserUplink       map[string]int64
	UserDownlink     map[string]int64
	InboundUplink    map[string]int64
	InboundDownlink  map[string]int64
	OutboundUplink   map[string]int64
	OutboundDownlink map[string]int64
	TotalUplink      int64
	TotalDownlink    int64
}

// QueryStats queries traffic statistics matching a pattern
func (c *LocalClient) QueryStats(ctx context.Context, pattern string, reset bool) (*XrayStats, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := statsService.NewStatsServiceClient(conn)

	// Query all stats
	req := &statsService.QueryStatsRequest{
		Pattern: pattern,
		Reset_:  reset,
	}

	resp, err := client.QueryStats(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query stats: %w", err)
	}

	stats := &XrayStats{
		UserUplink:       make(map[string]int64),
		UserDownlink:     make(map[string]int64),
		InboundUplink:    make(map[string]int64),
		InboundDownlink:  make(map[string]int64),
		OutboundUplink:   make(map[string]int64),
		OutboundDownlink: make(map[string]int64),
	}

	for _, stat := range resp.Stat {
		parts := strings.Split(stat.Name, ">>>")
		if len(parts) < 4 {
			continue
		}

		category := parts[0]
		name := parts[1]
		direction := parts[3]

		switch category {
		case "user":
			if direction == "uplink" {
				stats.UserUplink[name] = stat.Value
				stats.TotalUplink += stat.Value
			} else if direction == "downlink" {
				stats.UserDownlink[name] = stat.Value
				stats.TotalDownlink += stat.Value
			}
		case "inbound":
			if direction == "uplink" {
				stats.InboundUplink[name] = stat.Value
				// Exclude api tag (loopback control traffic).
				if name != "api" {
					stats.TotalUplink += stat.Value
				}
			} else if direction == "downlink" {
				stats.InboundDownlink[name] = stat.Value
				if name != "api" {
					stats.TotalDownlink += stat.Value
				}
			}
		case "outbound":
			if direction == "uplink" {
				stats.OutboundUplink[name] = stat.Value
			} else if direction == "downlink" {
				stats.OutboundDownlink[name] = stat.Value
			}
		}
	}

	return stats, nil
}

// Ping checks if the local xray API is reachable
func (c *LocalClient) Ping(ctx context.Context) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Just establishing a connection is enough for a ping
	return nil
}

// GetVersion: xray-core doesn't expose version via gRPC. Callers needing
// the version must shell out to `xray version`.
func (c *LocalClient) GetVersion(ctx context.Context) (string, error) {
	return "", nil
}

// GetAllOnlineUsers returns emails with active sessions using Xray's GetStatsOnline API.
// Falls back to traffic-based heuristic if the API is unavailable.
func (c *LocalClient) GetAllOnlineUsers(ctx context.Context) ([]string, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := statsService.NewStatsServiceClient(conn)

	// Try statsUserOnline: query all user online stats
	req := &statsService.QueryStatsRequest{
		Pattern: "user>>>",
		Reset_:  false,
	}

	resp, err := client.QueryStats(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query stats: %w", err)
	}

	// Collect users with online sessions (stat name: user>>>email>>>online, value > 0)
	onlineEmails := make(map[string]struct{})
	hasOnlineStats := false

	for _, stat := range resp.Stat {
		parts := strings.Split(stat.Name, ">>>")
		if len(parts) >= 3 && parts[2] == "online" && stat.Value > 0 {
			hasOnlineStats = true
			onlineEmails[parts[1]] = struct{}{}
		}
	}

	// If we found online stats, use them (statsUserOnline is available)
	if hasOnlineStats {
		result := make([]string, 0, len(onlineEmails))
		for email := range onlineEmails {
			result = append(result, email)
		}
		return result, nil
	}

	// Fallback: traffic-based heuristic (users with traffic > 0)
	userTraffic := make(map[string]int64)
	for _, stat := range resp.Stat {
		if !strings.Contains(stat.Name, ">>>traffic>>>") {
			continue
		}
		parts := strings.Split(stat.Name, ">>>")
		if len(parts) >= 2 && parts[1] != "" {
			userTraffic[parts[1]] += stat.Value
		}
	}

	var result []string
	for email, traffic := range userTraffic {
		if traffic > 0 {
			result = append(result, email)
		}
	}
	return result, nil
}

// GetUserOnlineIPs returns the IPs currently connected for a specific user
// using Xray's GetStatsOnlineIpList API
func (c *LocalClient) GetUserOnlineIPs(ctx context.Context, email string) (map[string]int64, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := statsService.NewStatsServiceClient(conn)

	statName := fmt.Sprintf("user>>>%s>>>online", email)
	req := &statsService.GetStatsRequest{
		Name:   statName,
		Reset_: false,
	}

	resp, err := client.GetStatsOnlineIpList(ctx, req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get online IPs for %s: %w", email, err)
	}

	return resp.Ips, nil
}
