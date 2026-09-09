package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	log "github.com/sirupsen/logrus"
)

// extraAllowedWSOrigins: extra Origin allowlist for WS upgrade beyond
// same-origin. From NASNET_WS_ALLOWED_ORIGINS env, comma-separated.
var extraAllowedWSOrigins = loadExtraAllowedWSOrigins()

func loadExtraAllowedWSOrigins() map[string]struct{} {
	raw := strings.TrimSpace(os.Getenv("NASNET_WS_ALLOWED_ORIGINS"))
	if raw == "" {
		return nil
	}
	out := make(map[string]struct{})
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out[strings.ToLower(s)] = struct{}{}
		}
	}
	return out
}

// checkOrigin enforces same-origin for the terminal WebSocket. Non-browser
// clients (empty Origin) are allowed — they already had to authenticate via
// the session cookie or bearer token. Browser clients must present an
// Origin that matches the request's Host, or be explicitly allow-listed
// via NASNET_WS_ALLOWED_ORIGINS for cross-origin panel deployments.
func checkOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// Non-browser client (CLI tooling, tests). Auth already happened
		// before reaching this handler; allow the upgrade.
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// Same-origin: Origin's host must equal the request Host.
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	// Explicit allow-list for split-domain panels.
	if _, ok := extraAllowedWSOrigins[strings.ToLower(origin)]; ok {
		return true
	}
	log.Warnf("Terminal WS: rejecting cross-origin upgrade from %q (host %q)", origin, r.Host)
	return false
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     checkOrigin,
}

const (
	// pongWait bounds how long we tolerate silence on the socket before
	// treating the peer as gone. Gorilla's read side honours this via
	// SetReadDeadline, so a dead client surfaces as a ReadMessage error.
	wsPongWait = 60 * time.Second
	// wsPingPeriod must be less than wsPongWait so we get at least one pong
	// before the deadline expires.
	wsPingPeriod = (wsPongWait * 9) / 10
	// wsWriteWait bounds individual write calls.
	wsWriteWait = 10 * time.Second
)

// TerminalMessage represents incoming WebSocket control messages from the browser
type TerminalMessage struct {
	Resize *TerminalResize `json:"resize,omitempty"`
	Close  bool            `json:"close,omitempty"`
	Ping   bool            `json:"ping,omitempty"`
}

// TerminalResize represents terminal resize dimensions
type TerminalResize struct {
	Cols uint32 `json:"cols"`
	Rows uint32 `json:"rows"`
}

// TerminalWebSocket bridges a browser WS to the agent's bidi gRPC stream.
// Binary frames = raw PTY I/O; text frames = JSON control (resize, close, ping).
func (h *Handler) TerminalWebSocket(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	// Upgrade HTTP to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Errorf("Terminal WS: Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	log.Infof("Terminal WS: Client connected for node %d", nodeID)

	// Keepalive: set an initial read deadline; refresh it whenever we
	// observe a pong. The pinger goroutine keeps the tunnel warm through
	// idle load balancers.
	_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	// WriteMessage is not safe for concurrent use. The gRPC→WS goroutine
	// and the pinger both produce frames, so we serialize all writes
	// through writeMu.
	var writeMu sync.Mutex
	terminalEnded := false // guarded by writeMu; final control always precedes close
	writeFrame := func(msgType int, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if terminalEnded && msgType != websocket.CloseMessage {
			return websocket.ErrCloseSent
		}
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		return conn.WriteMessage(msgType, data)
	}
	writeControl := func(v any, final bool) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if terminalEnded {
			return websocket.ErrCloseSent
		}
		terminalEnded = final
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		return conn.WriteJSON(v)
	}
	writeJSON := func(v any) error { return writeControl(v, false) }
	writeResult := func(v any) error { return writeControl(v, true) }

	// Own the stream context: a shell exit or browser disconnect must cancel
	// its peer even while the original HTTP request is still alive.
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	termSession, cleanup, err := h.nodeUsecase.OpenTerminal(ctx, uint(nodeID))
	if err != nil {
		log.Errorf("Terminal WS: Failed to open terminal for node %d: %v", nodeID, err)
		_ = writeResult(gin.H{"error": err.Error()})
		_ = writeFrame(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "Terminal unavailable"))
		return
	}

	// Every pump signals completion, including explicit close and successful
	// shell exit. A closed channel cannot block a second pump during shutdown.
	done := make(chan struct{})
	var finishOnce sync.Once
	finish := func() { finishOnce.Do(func() { close(done) }) }
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer finish()
		ticker := time.NewTicker(wsPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := writeFrame(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Updated agents acknowledge PTY startup with an empty data frame; older
	// agents first send shell output. Either establishes PTY readiness, unlike
	// a successful WebSocket upgrade alone.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer finish()
		ready := false
		for {
			output, err := termSession.Recv()
			if err != nil {
				if ctx.Err() == nil {
					message := "Terminal connection was lost. Start a new session to reconnect."
					if errors.Is(err, io.EOF) {
						message = "Terminal stream ended without an exit status."
					}
					log.Debugf("Terminal WS: gRPC recv error: %v", err)
					_ = writeResult(gin.H{"error": message})
				}
				return
			}
			if ctx.Err() != nil {
				return
			}

			switch p := output.Payload.(type) {
			case *pb.TerminalOutput_Data:
				if !ready {
					if err := writeJSON(gin.H{"ready": true}); err != nil {
						return
					}
					ready = true
				}
				if err := writeFrame(websocket.BinaryMessage, p.Data); err != nil {
					return
				}
			case *pb.TerminalOutput_ExitCode:
				_ = writeResult(gin.H{"exit_code": p.ExitCode})
				return
			case *pb.TerminalOutput_Error:
				_ = writeResult(gin.H{"error": p.Error})
				return
			}
		}
	}()

	// This goroutine is the sole gRPC input writer, including CloseSend.
	// Shutdown closes the WebSocket to wake ReadMessage; stream cancellation
	// wakes an in-flight Send, without another goroutine sending concurrently.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer finish()
		defer func() { _ = termSession.CloseSend() }()
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil || ctx.Err() != nil {
				return
			}
			switch msgType {
			case websocket.BinaryMessage:
				if len(data) > 0 {
					if err := termSession.Send(&pb.TerminalInput{
						Payload: &pb.TerminalInput_Data{Data: data},
					}); err != nil {
						_ = writeResult(gin.H{"error": "Terminal input could not be delivered. Start a new session to reconnect."})
						return
					}
				}
			case websocket.TextMessage:
				var msg TerminalMessage
				if err := json.Unmarshal(data, &msg); err != nil {
					log.Debugf("Terminal WS: Failed to parse control message: %v", err)
					continue
				}
				if msg.Ping {
					_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
					if err := writeJSON(gin.H{"pong": true}); err != nil {
						return
					}
				}
				if msg.Close {
					return
				}
				if msg.Resize != nil {
					if err := termSession.Send(&pb.TerminalInput{
						Payload: &pb.TerminalInput_Resize{Resize: &pb.TerminalResize{
							Cols: msg.Resize.Cols,
							Rows: msg.Resize.Rows,
						}},
					}); err != nil {
						_ = writeResult(gin.H{"error": "Terminal resize could not be delivered. Start a new session to reconnect."})
						return
					}
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}
	cancel()
	cleanup()
	_ = writeFrame(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Terminal session ended"))
	_ = conn.Close()
	wg.Wait()
	log.Infof("Terminal WS: Session ended for node %d", nodeID)
}
