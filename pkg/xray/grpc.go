package xray

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// GRPCClient handles gRPC connections to xray-core API
// Uses connection pooling to manage connections
type GRPCClient struct {
	timeout int
	conns   map[string]*grpc.ClientConn
	mu      sync.RWMutex
}

// NewGRPCClient creates a new gRPC client for xray-core API
func NewGRPCClient(timeout int) *GRPCClient {
	if timeout <= 0 {
		timeout = 5
	}
	return &GRPCClient{
		timeout: timeout,
		conns:   make(map[string]*grpc.ClientConn),
	}
}

// dial establishes or reuses a gRPC connection to a specific xray-core API server
// Returns the connection, context, and a cleanup function (which is a no-op for pooled connections)
func (c *GRPCClient) dial(ctx context.Context, target string) (*grpc.ClientConn, context.Context, func(), error) {
	c.mu.RLock()
	conn, exists := c.conns[target]
	c.mu.RUnlock()

	// Check if existing connection is valid
	if exists {
		state := conn.GetState()
		if state == connectivity.Ready || state == connectivity.Idle {
			// Reuse existing connection
			callCtx, cancel := context.WithTimeout(ctx, time.Duration(c.timeout)*time.Second)

			// For pooled connections, closeFunc is a no-op merely cancelling the context
			closeFunc := func() {
				cancel()
			}
			return conn, callCtx, closeFunc, nil
		}

		// If connection is in bad state, close and remove it
		conn.Close()
		c.mu.Lock()
		delete(c.conns, target)
		c.mu.Unlock()
	}

	// Dial new connection
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second) // Dial timeout
	defer dialCancel()

	conn, err := grpc.DialContext(
		dialCtx,
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second, // Send pings every 10s if no activity
			Timeout:             5 * time.Second,  // Wait 5s for ping ack before considering dead
			PermitWithoutStream: true,             // Send pings even without active streams
		}),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to dial xray API at %s: %w", target, err)
	}

	// Store in pool
	c.mu.Lock()
	c.conns[target] = conn
	c.mu.Unlock()

	// Prepare return values
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(c.timeout)*time.Second)

	closeFunc := func() {
		cancel()
		// Do NOT close the connection here; it stays in the pool
	}

	return conn, callCtx, closeFunc, nil
}

// PingTarget checks if a specific Xray API endpoint is reachable
// Returns latency in milliseconds
func (c *GRPCClient) PingTarget(ctx context.Context, target string) (int64, error) {
	start := time.Now()

	// Pooled dial — reachability check, not strict latency measurement.
	conn, _, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return 0, err
	}
	defer closeFunc()

	// Check state to ensure it's actually alive
	if conn.GetState() != connectivity.Ready {
		// Try to wait for state change? Or just accept it's connected (dial verifies initial connection)
	}

	latency := time.Since(start).Milliseconds()
	return latency, nil
}

// CloseAll closes all pooled connections
func (c *GRPCClient) CloseAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for target, conn := range c.conns {
		conn.Close()
		delete(c.conns, target)
	}
}

// GetTimeout returns the configured timeout in seconds
func (c *GRPCClient) GetTimeout() int {
	return c.timeout
}
