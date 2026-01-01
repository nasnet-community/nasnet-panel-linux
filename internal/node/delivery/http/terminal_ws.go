package http

import (
	"encoding/json"
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
}

// TerminalResize represents terminal resize dimensions
type TerminalResize struct {
	Cols uint32 `json:"cols"`
	Rows uint32 `json:"rows"`
}

// TerminalWebSocket bridges a browser WS to the agent's bidi gRPC stream.
// Binary frames = raw PTY I/O; text frames = JSON control (resize, close).
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
	writeFrame := func(msgType int, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		return conn.WriteMessage(msgType, data)
	}
	writeJSON := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		return conn.WriteJSON(v)
	}

	// Open terminal session via usecase
	termSession, cleanup, err := h.nodeUsecase.OpenTerminal(c.Request.Context(), uint(nodeID))
	if err != nil {
		log.Errorf("Terminal WS: Failed to open terminal for node %d: %v", nodeID, err)
		_ = writeJSON(gin.H{"error": err.Error()})
		return
	}
	defer cleanup()

	// Use context cancellation for clean shutdown
	ctx := c.Request.Context()

	// Error channel for coordinating shutdown
	errChan := make(chan error, 2)
	var wg sync.WaitGroup

	// Pinger: sends a control ping every wsPingPeriod. Gorilla handles the
	// pong automatically client-side; we refresh the read deadline in the
	// pong handler above.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(wsPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := writeFrame(websocket.PingMessage, nil); err != nil {
					log.Debugf("Terminal WS: ping write failed: %v", err)
					select {
					case errChan <- err:
					default:
					}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Goroutine: Read from gRPC stream → Write to WebSocket (as binary)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				output, err := termSession.Recv()
				if err != nil {
					log.Debugf("Terminal WS: gRPC recv error: %v", err)
					errChan <- err
					return
				}

				switch p := output.Payload.(type) {
				case *pb.TerminalOutput_Data:
					if err := writeFrame(websocket.BinaryMessage, p.Data); err != nil {
						log.Debugf("Terminal WS: WebSocket write error: %v", err)
						errChan <- err
						return
					}
				case *pb.TerminalOutput_ExitCode:
					_ = writeJSON(gin.H{"exit_code": p.ExitCode})
					return
				case *pb.TerminalOutput_Error:
					_ = writeJSON(gin.H{"error": p.Error})
					return
				}
			}
		}
	}()

	// Goroutine: Read from WebSocket → Write to gRPC stream
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				msgType, data, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						log.Debug("Terminal WS: Client disconnected normally")
					} else {
						log.Debugf("Terminal WS: WebSocket read error: %v", err)
					}
					errChan <- err
					return
				}

				switch msgType {
				case websocket.BinaryMessage:
					// Binary frames = raw terminal input, forward directly to PTY
					if len(data) > 0 {
						if err := termSession.Send(&pb.TerminalInput{
							Payload: &pb.TerminalInput_Data{Data: data},
						}); err != nil {
							log.Debugf("Terminal WS: Failed to send input: %v", err)
							errChan <- err
							return
						}
					}

				case websocket.TextMessage:
					// Text frames = JSON control messages (resize, close)
					var msg TerminalMessage
					if err := json.Unmarshal(data, &msg); err != nil {
						log.Debugf("Terminal WS: Failed to parse control message: %v", err)
						continue
					}
					if msg.Resize != nil {
						if err := termSession.Send(&pb.TerminalInput{
							Payload: &pb.TerminalInput_Resize{
								Resize: &pb.TerminalResize{
									Cols: msg.Resize.Cols,
									Rows: msg.Resize.Rows,
								},
							},
						}); err != nil {
							log.Debugf("Terminal WS: Failed to send resize: %v", err)
						}
					}
					if msg.Close {
						termSession.Send(&pb.TerminalInput{
							Payload: &pb.TerminalInput_Close{Close: true},
						})
						return
					}
				}
			}
		}
	}()

	// Wait for either goroutine to finish or context cancellation
	select {
	case <-ctx.Done():
		log.Info("Terminal WS: Context cancelled")
	case <-errChan:
		// One of the goroutines encountered an error
	}

	// Signal close to agent
	termSession.Send(&pb.TerminalInput{
		Payload: &pb.TerminalInput_Close{Close: true},
	})

	log.Infof("Terminal WS: Session ended for node %d", nodeID)
}
