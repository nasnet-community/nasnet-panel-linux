package reverse

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
	hubpb "github.com/nasnet-community/nasnet-panel-linux/pkg/hub/pb"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

const (
	dialTimeout    = 5 * time.Second
	maxMessageSize = 100 * 1024 * 1024 // 100 MB

	// Sessions alive longer than this threshold are considered "healthy".
	// On disconnect after a healthy session, the failure counter resets so
	// the next reconnection attempt starts immediately instead of backing off.
	healthySessionThreshold = 30 * time.Second
)

// Connector maintains a persistent reverse gRPC connection to the master hub.
type Connector struct {
	cfg        *config.Config
	handler    *CommandHandler
	baseDelay  time.Duration
	maxDelay   time.Duration
	Version    string
	Commit     string
	stopChan   chan struct{}
	wg         sync.WaitGroup // tracks the Run goroutine
	inflightWg sync.WaitGroup // tracks spawned command/terminal goroutines
	sendMu     sync.Mutex
	connMu     sync.Mutex       // guards activeConn
	activeConn *grpc.ClientConn // current gRPC conn, closed by Stop() to unblock Recv()
}

// NewConnector creates a new Connector.
func NewConnector(cfg *config.Config, handler *CommandHandler) *Connector {
	return &Connector{
		cfg:       cfg,
		handler:   handler,
		baseDelay: 2 * time.Second,
		maxDelay:  2 * time.Minute,
		stopChan:  make(chan struct{}),
	}
}

// Run is the main loop: connect, run session, backoff on failure, repeat.
// It returns only when ctx is cancelled or Stop() is called.
func (c *Connector) Run(ctx context.Context) error {
	c.wg.Add(1)
	defer c.wg.Done()

	failures := 0
	for {
		select {
		case <-c.stopChan:
			logrus.Info("ReverseConnector: stop requested, exiting run loop")
			return nil
		case <-ctx.Done():
			logrus.Info("ReverseConnector: context cancelled, exiting run loop")
			return ctx.Err()
		default:
		}

		logrus.WithFields(logrus.Fields{
			"hub_url":   c.cfg.HubURL,
			"node_uuid": c.cfg.NodeUUID,
			"attempt":   failures + 1,
		}).Info("ReverseConnector: connecting to hub")

		sessionStart := time.Now()
		err := c.runSession(ctx)
		sessionDur := time.Since(sessionStart)

		if err != nil {
			// Check if we should stop
			select {
			case <-c.stopChan:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// If the session was alive long enough, the disconnect is a
			// fresh event (not a repeated failure), so reset the counter
			// so we retry immediately instead of backing off.
			if sessionDur >= healthySessionThreshold {
				failures = 0
			}

			failures++
			delay := c.backoff(failures)
			logrus.WithFields(logrus.Fields{
				"error":       err,
				"failures":    failures,
				"session_dur": sessionDur.Round(time.Millisecond),
				"retry_in":    delay,
			}).Warn("ReverseConnector: session ended, will retry")

			select {
			case <-c.stopChan:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		} else {
			// Clean session exit — reset failure counter
			failures = 0
		}
	}
}

// Stop signals the connector to stop and waits for it to finish.
func (c *Connector) Stop() {
	close(c.stopChan)

	// Close the active gRPC connection to immediately unblock stream.Recv().
	c.connMu.Lock()
	if c.activeConn != nil {
		c.activeConn.Close()
	}
	c.connMu.Unlock()

	c.wg.Wait()         // wait for Run() loop to exit
	c.inflightWg.Wait() // wait for in-flight commands/terminals to finish
}

func (c *Connector) setActiveConn(conn *grpc.ClientConn) {
	c.connMu.Lock()
	c.activeConn = conn
	c.connMu.Unlock()
}

func (c *Connector) clearActiveConn() {
	c.connMu.Lock()
	c.activeConn = nil
	c.connMu.Unlock()
}

// Recycle drops the current session; the run loop reconnects and picks up a
// freshly installed certificate.
func (c *Connector) Recycle() {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.activeConn != nil {
		_ = c.activeConn.Close()
	}
}

// runSession dials the hub, opens a CommandChannel, sends registration, and starts the recv loop.
func (c *Connector) runSession(ctx context.Context) error {
	// Build dial options
	dialOpts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxMessageSize),
			grpc.MaxCallSendMsgSize(maxMessageSize),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			// Ping often enough that a hub-side abort is noticed quickly. The
			// hub kills a stalled socket via TCP_USER_TIMEOUT without ever
			// putting a FIN on the wire, so this interval is what bounds how
			// long we sit on a dead session. Must stay >= the hub's
			// KeepaliveEnforcementPolicy.MinTime (5s) to avoid too_many_pings.
			Time:                10 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	if c.cfg.TLS.Enabled {
		tlsCfg, err := c.buildTLS()
		if err != nil {
			return fmt.Errorf("build TLS config: %w", err)
		}
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Dial with timeout
	dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)
	defer dialCancel()

	//nolint:staticcheck // grpc.Dial is deprecated but grpc.NewClient doesn't support WithBlock
	conn, err := grpc.DialContext(dialCtx, c.cfg.HubURL, dialOpts...)
	if err != nil {
		return fmt.Errorf("dial hub %s: %w", c.cfg.HubURL, err)
	}
	c.setActiveConn(conn)
	defer func() {
		c.clearActiveConn()
		if closeErr := conn.Close(); closeErr != nil {
			logrus.WithError(closeErr).Warn("ReverseConnector: error closing gRPC connection")
		}
	}()

	client := hubpb.NewAgentHubClient(conn)

	stream, err := client.CommandChannel(ctx)
	if err != nil {
		return fmt.Errorf("open CommandChannel: %w", err)
	}

	// Fetch initial status for registration
	status, err := c.handler.GetNodeStatus(ctx)
	if err != nil {
		logrus.WithError(err).Warn("ReverseConnector: failed to get initial status for registration, sending without it")
		status = nil
	}

	// Send registration
	regEnv := &hubpb.AgentEnvelope{
		Event: &hubpb.AgentEnvelope_Registration{
			Registration: &hubpb.AgentRegistration{
				NodeUuid:      c.cfg.NodeUUID,
				AgentVersion:  c.Version,
				AgentCommit:   c.Commit,
				InitialStatus: status,
			},
		},
	}
	if err := c.sendOnStream(stream, regEnv); err != nil {
		return fmt.Errorf("send registration: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"node_uuid": c.cfg.NodeUUID,
		"version":   c.Version,
		"commit":    c.Commit,
	}).Info("ReverseConnector: registered with hub, starting recv loop")

	return c.recvLoop(ctx, stream, conn)
}

// recvLoop receives HubEnvelopes from the stream and dispatches them.
func (c *Connector) recvLoop(ctx context.Context, stream hubpb.AgentHub_CommandChannelClient, conn *grpc.ClientConn) error {
	for {
		// Check for stop/cancel before blocking recv
		select {
		case <-c.stopChan:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		env, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("stream recv: %w", err)
		}

		switch {
		case env.GetPing() != nil:
			ping := env.GetPing()
			logrus.WithFields(logrus.Fields{
				"seq": ping.GetSequence(),
			}).Debug("ReverseConnector: received ping, sending pong")

			// Get current status for pong
			nodeStatus, _ := c.handler.GetNodeStatus(ctx)
			now := time.Now().UnixMilli()
			rttMs := now - ping.GetTimestamp()
			if rttMs < 0 {
				rttMs = 0
			}

			pong := &hubpb.AgentEnvelope{
				Event: &hubpb.AgentEnvelope_Heartbeat{
					Heartbeat: &hubpb.HeartbeatPong{
						Timestamp: now,
						Sequence:  ping.GetSequence(),
						RttMs:     rttMs,
						Status:    nodeStatus,
						NodeUuid:  c.cfg.NodeUUID,
					},
				},
			}
			if sendErr := c.sendOnStream(stream, pong); sendErr != nil {
				logrus.WithError(sendErr).Warn("ReverseConnector: failed to send heartbeat pong")
			}

		case env.GetOpenTerminal() != nil:
			sig := env.GetOpenTerminal()
			logrus.WithField("session_id", sig.GetSessionId()).Info("ReverseConnector: received OpenTerminal signal")
			c.inflightWg.Add(1)
			go func() {
				defer c.inflightWg.Done()
				c.handleTerminalSignal(ctx, conn, sig.GetSessionId())
			}()

		case env.GetOpenLogStream() != nil:
			sig := env.GetOpenLogStream()
			logrus.WithField("stream_id", sig.GetStreamId()).Info("ReverseConnector: received OpenLogStream signal")
			c.inflightWg.Add(1)
			go func() {
				defer c.inflightWg.Done()
				c.handleLogStreamSignal(ctx, conn, sig)
			}()

		case env.GetMethod() != "":
			// Command envelope — dispatch concurrently
			c.inflightWg.Add(1)
			go func() {
				defer c.inflightWg.Done()
				c.handleCommand(ctx, stream, env)
			}()

		default:
			logrus.WithField("request_id", env.GetRequestId()).Debug("ReverseConnector: received unknown envelope, ignoring")
		}
	}
}

// handleCommand calls handler.HandleCommand and sends the response back on the stream.
func (c *Connector) handleCommand(ctx context.Context, stream hubpb.AgentHub_CommandChannelClient, env *hubpb.HubEnvelope) {
	// Apply the hub-supplied deadline, or a conservative default so a
	// malformed or missing DeadlineMs cannot pin a handler goroutine
	// forever and keep the recv loop from being drained.
	deadline := 30 * time.Second
	if env.GetDeadlineMs() > 0 {
		deadline = time.Duration(env.GetDeadlineMs()) * time.Millisecond
	}
	cmdCtx, cmdCancel := context.WithTimeout(ctx, deadline)
	defer cmdCancel()

	logrus.WithFields(logrus.Fields{
		"request_id":  env.GetRequestId(),
		"method":      env.GetMethod(),
		"deadline_ms": env.GetDeadlineMs(),
	}).Debug("ReverseConnector: dispatching command")

	payload, err := c.handler.HandleCommand(cmdCtx, env.GetMethod(), env.GetPayload())

	resp := &hubpb.AgentEnvelope{
		RequestId: env.GetRequestId(),
	}
	if err != nil {
		resp.Error = err.Error()
		logrus.WithFields(logrus.Fields{
			"request_id": env.GetRequestId(),
			"method":     env.GetMethod(),
			"error":      err,
		}).Warn("ReverseConnector: command failed")
	} else {
		resp.Payload = payload
	}

	if sendErr := c.sendOnStream(stream, resp); sendErr != nil {
		logrus.WithFields(logrus.Fields{
			"request_id": env.GetRequestId(),
			"method":     env.GetMethod(),
			"error":      sendErr,
		}).Warn("ReverseConnector: failed to send command response")
	}
}

// handleTerminalSignal opens a TerminalChannel stream to the hub, starts a
// local PTY shell, and bridges the two until either side closes.
func (c *Connector) handleTerminalSignal(ctx context.Context, conn *grpc.ClientConn, sessionID string) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	log := logrus.WithField("session_id", sessionID)

	// 1. Open TerminalChannel stream to the hub.
	client := hubpb.NewAgentHubClient(conn)
	termStream, err := client.TerminalChannel(ctx)
	if err != nil {
		log.WithError(err).Error("ReverseConnector: failed to open TerminalChannel")
		return
	}

	// Mutex to serialize concurrent Send calls on termStream.
	var termSendMu sync.Mutex
	sendFrame := func(frame *hubpb.TerminalFrame) error {
		termSendMu.Lock()
		defer termSendMu.Unlock()
		return termStream.Send(frame)
	}

	// 2. Send identification frame so the hub can match this stream to the session.
	if err := sendFrame(&hubpb.TerminalFrame{
		SessionId: sessionID,
		NodeUuid:  c.cfg.NodeUUID,
	}); err != nil {
		log.WithError(err).Error("ReverseConnector: failed to send terminal ID frame")
		return
	}

	// 3. Start a local PTY shell (same approach as internal/agent/server/terminal.go).
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
		if _, err := os.Stat(shell); os.IsNotExist(err) {
			shell = "/bin/sh"
		}
	}

	cmd := exec.Command(shell, "-i")

	// Set TERM=xterm-256color, filtering any existing TERM variable.
	env := os.Environ()
	filteredEnv := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "TERM=") {
			filteredEnv = append(filteredEnv, e)
		}
	}
	cmd.Env = append(filteredEnv, "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.WithError(err).Error("ReverseConnector: failed to start PTY")
		_ = sendFrame(&hubpb.TerminalFrame{
			SessionId: sessionID,
			Payload:   &hubpb.TerminalFrame_Error{Error: "failed to start PTY: " + err.Error()},
		})
		// Half-close flushes the error before cancellation resets the RPC.
		_ = termStream.CloseSend()
		timeout := time.AfterFunc(5*time.Second, cancel)
		defer timeout.Stop()
		for {
			if _, err := termStream.Recv(); err != nil {
				break
			}
		}
		return
	}

	// pty.Start uses Fd(), which disables Go's poller. Reopen a nonblocking
	// duplicate so Close interrupts reads, including after terminal resizes.
	fd, err := unix.FcntlInt(ptmx.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err == nil {
		err = syscall.SetNonblock(fd, true)
		if err != nil {
			_ = syscall.Close(fd)
		}
	}
	_ = ptmx.Close()
	if err != nil {
		log.WithError(err).Error("ReverseConnector: failed to reopen PTY")
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return
	}
	ptmx = os.NewFile(uintptr(fd), "terminal-pty")

	log.WithField("shell", shell).Info("ReverseConnector: PTY started")

	processDone := make(chan struct{})
	exitCode := int32(0)
	go func() {
		if err := cmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = int32(exitErr.ExitCode())
			}
		}
		close(processDone)
	}()

	done := make(chan struct{})
	var finishOnce sync.Once
	finish := func() { finishOnce.Do(func() { close(done) }) }
	var outputWG sync.WaitGroup
	outputDone := make(chan struct{})
	var inputWG sync.WaitGroup
	defer func() {
		// Cancelling this terminal RPC releases Recv without disconnecting
		// the shared command channel or any other terminal session.
		cancel()
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
		<-processDone
		outputWG.Wait()
		inputWG.Wait()
		_ = termStream.CloseSend()
		log.Info("ReverseConnector: PTY session closed")
	}()

	// Match direct terminals: acknowledge a started PTY even when the shell
	// does not print a prompt or is waiting silently for its first input.
	if err := sendFrame(&hubpb.TerminalFrame{
		SessionId: sessionID,
		Payload:   &hubpb.TerminalFrame_Data{Data: []byte{}},
	}); err != nil {
		return
	}

	outputWG.Add(1)
	go func() {
		defer outputWG.Done()
		defer close(outputDone)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if err := sendFrame(&hubpb.TerminalFrame{
					SessionId: sessionID,
					Payload:   &hubpb.TerminalFrame_Data{Data: buf[:n]},
				}); err != nil {
					finish()
					return
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.EIO) && !errors.Is(err, os.ErrClosed) && ctx.Err() == nil {
					log.Debugf("ReverseConnector: PTY read error: %v", err)
					finish()
				}
				return
			}
		}
	}()

	inputWG.Add(1)
	go func() {
		defer inputWG.Done()
		defer finish()
		for {
			frame, err := termStream.Recv()
			if err != nil || ctx.Err() != nil {
				return
			}
			switch p := frame.GetPayload().(type) {
			case *hubpb.TerminalFrame_Data:
				if _, err := ptmx.Write(p.Data); err != nil {
					return
				}
			case *hubpb.TerminalFrame_Resize:
				if p.Resize != nil {
					if err := pty.Setsize(ptmx, &pty.Winsize{
						Rows: uint16(p.Resize.Rows),
						Cols: uint16(p.Resize.Cols),
					}); err != nil {
						log.Warnf("ReverseConnector: failed to resize PTY: %v", err)
					}
				}
			case *hubpb.TerminalFrame_Close:
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-done:
	case <-processDone:
		// Drain final output, then detach children that still hold the PTY open.
		drain := time.AfterFunc(250*time.Millisecond, func() { _ = ptmx.Close() })
		defer drain.Stop()
		select {
		case <-outputDone:
		case <-ctx.Done():
			return
		case <-done:
			return
		}
		if err := sendFrame(&hubpb.TerminalFrame{
			SessionId: sessionID,
			Payload:   &hubpb.TerminalFrame_ExitCode{ExitCode: exitCode},
		}); err == nil {
			// Let the hub consume the final frames before resetting the RPC.
			// Its browser owner closes the session as soon as it sees exit.
			_ = termStream.CloseSend()
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-done:
			case <-timer.C:
			}
		}
	}
}

// handleLogStreamSignal handles an OpenLogStreamSignal from the hub.
// Log streaming over the reverse channel is not yet supported because the
// LogChannel RPC is server-streaming (hub→agent), which is the wrong direction
// for forwarding agent log entries to the master. A bidirectional proto change
// is needed before this can be implemented.
func (c *Connector) handleLogStreamSignal(_ context.Context, _ *grpc.ClientConn, sig *hubpb.OpenLogStreamSignal) {
	logrus.WithFields(logrus.Fields{
		"stream_id":  sig.GetStreamId(),
		"tail_lines": sig.GetTailLines(),
		"follow":     sig.GetFollow(),
	}).Warn("[ReverseConnector] Log streaming over reverse channel not yet supported")
}

// sendOnStream sends an AgentEnvelope on the stream with a mutex to prevent concurrent sends.
func (c *Connector) sendOnStream(stream hubpb.AgentHub_CommandChannelClient, env *hubpb.AgentEnvelope) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return stream.Send(env)
}

// buildTLS loads the agent's client TLS credentials.
func (c *Connector) buildTLS() (*tls.Config, error) {
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		// Every node certificate chains to the same CA; one naming a node
		// must not be accepted as the hub.
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return nil
			}
			cn := cs.PeerCertificates[0].Subject.CommonName
			if id, ok := certs.NodeIDFromCommonName(cn); ok && id != 0 {
				return fmt.Errorf("node certificate %q cannot act as the hub", cn)
			}
			return nil
		},
	}

	// Load CA certificate for server verification
	if c.cfg.TLS.CACert != "" {
		caCert, err := os.ReadFile(c.cfg.TLS.CACert)
		if err != nil {
			return nil, fmt.Errorf("read CA cert %s: %w", c.cfg.TLS.CACert, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert from %s", c.cfg.TLS.CACert)
		}
		tlsCfg.RootCAs = pool
	}

	// Load client certificate and key (mTLS)
	if c.cfg.TLS.ServerCert != "" && c.cfg.TLS.ServerKey != "" {
		cert, err := tls.LoadX509KeyPair(c.cfg.TLS.ServerCert, c.cfg.TLS.ServerKey)
		if err != nil {
			return nil, fmt.Errorf("load client keypair (%s, %s): %w", c.cfg.TLS.ServerCert, c.cfg.TLS.ServerKey, err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}

// backoff returns an exponential backoff duration with jitter for the given failure count.
func (c *Connector) backoff(failures int) time.Duration {
	if failures <= 0 {
		return c.baseDelay
	}

	// First failure: retry almost immediately (500ms + small jitter)
	if failures == 1 {
		return 500*time.Millisecond + time.Duration(rand.Int63n(int64(200*time.Millisecond)))
	}

	// Exponential: base * 2^(failures-2), capped at maxDelay.
	// Cap the exponent to avoid int64 overflow when converting the
	// float64 result to time.Duration (which is int64 nanoseconds).
	// 2^30 * 2s is well above maxDelay so the cap below always applies.
	exp := failures - 2
	if exp > 30 {
		exp = 30
	}
	delay := time.Duration(float64(c.baseDelay) * math.Pow(2, float64(exp)))
	if delay > c.maxDelay {
		delay = c.maxDelay
	}

	// Add up to 20% jitter
	jitter := time.Duration(rand.Int63n(int64(delay / 5)))
	return delay + jitter
}
