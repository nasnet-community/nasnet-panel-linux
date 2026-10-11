package mtprotonode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/nasnet-community/mtproto-remote-plugin/ctrl"
	"github.com/nasnet-community/mtproto-remote-plugin/relayops"
	"github.com/nasnet-community/mtproto-remote-plugin/release"
	"github.com/nasnet-community/mtproto-remote-plugin/tgnet"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
)

// Handler is the MTProto node API: admin-only routes that never return a secret.
type Handler struct {
	svc   *relayops.Service
	live  LiveNodes
	allow *tgnet.Refresher
}

// LiveNodes is the manager as the handler sees it.
type LiveNodes interface {
	View(nodeID uint) (View, bool)
	Call(ctx context.Context, nodeID uint, method string, req, resp any) error
	Domestic(addrs []string) *bool
	Row(ctx context.Context, nodeID uint) (*LiteNode, error)
	Rows(ctx context.Context) ([]*LiteNode, error)
	UpdateSettings(ctx context.Context, nodeID uint, s Settings) error
	Events(nodeID uint, limit int) EventFeed
	Record(nodeID uint, ev Event)
}

func NewHandler(svc *relayops.Service, live LiveNodes, allow *tgnet.Refresher) *Handler {
	return &Handler{svc: svc, live: live, allow: allow}
}

func (h *Handler) RegisterRoutes(r gin.IRouter) {
	g := r.Group("/mtproto")
	g.POST("/probe", h.Probe)
	g.POST("/nodes", h.Install)
	g.GET("/jobs/:id", h.Job)
	g.GET("/jobs/:id/stream", h.StreamJob)
	g.GET("/nodes", h.List)
	g.GET("/nodes/:id", h.Node)
	g.PATCH("/nodes/:id", h.Settings)
	g.GET("/nodes/:id/events", h.Events)
	g.POST("/nodes/:id/repair", h.Repair)
	g.POST("/nodes/:id/remove", h.Delete)
	g.POST("/nodes/:id/update", h.Update)
	g.POST("/nodes/:id/rotate", h.Rotate)
	g.POST("/nodes/:id/link-port", h.LinkPort)
	g.POST("/nodes/:id/address", h.Address)
	g.POST("/nodes/:id/restart", h.Restart)
	g.POST("/nodes/:id/stop", h.Stop)
	g.POST("/nodes/:id/start", h.Start)
	g.GET("/nodes/:id/logs", h.Logs)
	g.POST("/nodes/:id/disguise-check", h.DisguiseCheck)
	g.GET("/allowlist", h.Allowlist)
	g.POST("/allowlist/refresh", h.RefreshAllowlist)
}

// fail maps errors to status codes; SSH and agent failures keep their text for the operator.
func (h *Handler) fail(c *gin.Context, err error) {
	var ce *ctrl.Error
	switch {
	case errors.Is(err, relayops.ErrInvalid):
		httputil.Error(c, http.StatusBadRequest, plain(err.Error()))
	case errors.Is(err, relayops.ErrVPS):
		httputil.Error(c, http.StatusBadGateway, plain(err.Error()))
	case errors.Is(err, ErrNotFound), errors.Is(err, relayops.ErrNotFound):
		httputil.Error(c, http.StatusNotFound, "no such MTProto node")
	case errors.Is(err, relayops.ErrBusy):
		httputil.Error(c, http.StatusConflict, "another job is already running on this node: wait for it to finish")
	case errors.Is(err, relayops.ErrUpToDate):
		httputil.Error(c, http.StatusConflict, "this node already runs that version")
	case errors.Is(err, relayops.ErrNotConnected), errors.Is(err, relayops.ErrNeedLogin), errors.Is(err, relayops.ErrUnknownArch),
		errors.Is(err, ErrNotRunning), errors.Is(err, ErrDisabled), errors.Is(err, ErrProxyStopped):
		httputil.Error(c, http.StatusConflict, plain(err.Error()))
	case errors.As(err, &ce):
		httputil.Error(c, http.StatusBadGateway, plain(ce.Error()))
	default:
		httputil.Error(c, http.StatusInternalServerError, plain(err.Error()))
	}
}

// plain drops the module's "relay:" package prefix wherever it starts an error in the chain.
func plain(msg string) string {
	return strings.ReplaceAll(strings.TrimPrefix(msg, "relay: "), ": relay: ", ": ")
}

// plainJob is a job as the operator reads it; the steps are a copy already.
func plainJob(v relayops.JobView) relayops.JobView {
	v.Error = plain(v.Error)
	for i := range v.Steps {
		v.Steps[i].Detail = plain(v.Steps[i].Detail)
	}
	return v
}

func nodeID(c *gin.Context) (uint, bool) {
	n, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || n == 0 {
		httputil.Error(c, http.StatusBadRequest, "bad node id")
		return 0, false
	}
	return uint(n), true
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil && !errors.Is(err, io.EOF) {
		httputil.Error(c, http.StatusBadRequest, "malformed request body")
		return false
	}
	return true
}

type loginBody struct {
	Host       string `json:"host"`
	SSHPort    int    `json:"ssh_port"`
	SSHUser    string `json:"ssh_user"`
	Password   string `json:"ssh_password"`
	Key        string `json:"ssh_key"`
	Passphrase string `json:"ssh_passphrase"`
	HostKey    string `json:"host_key"`
}

func (b loginBody) login() relayops.Login {
	return relayops.Login{Host: b.Host, Port: b.SSHPort, User: b.SSHUser, Password: b.Password, Key: []byte(b.Key), Passphrase: []byte(b.Passphrase), HostKey: b.HostKey}
}

// POST /mtproto/probe  {host, ssh_port, ssh_user} -> {fingerprint}
func (h *Handler) Probe(c *gin.Context) {
	var b loginBody
	if !bind(c, &b) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	fp, err := h.svc.Fingerprint(ctx, b.Host, b.SSHPort, b.SSHUser)
	if err != nil {
		if errors.Is(err, relayops.ErrInvalid) {
			h.fail(c, err)
			return
		}
		httputil.Error(c, http.StatusBadGateway, plain(err.Error()))
		return
	}
	httputil.OK(c, gin.H{"fingerprint": fp})
}

type installBody struct {
	loginBody
	Name                    string `json:"name"`
	Disguise                string `json:"disguise"`
	MaxStreams              int    `json:"max_streams"`
	HubPort                 int    `json:"hub_port"`
	CapturePort             int    `json:"capture_port"`
	AllowUnverifiedDisguise bool   `json:"allow_unverified_disguise"`
}

// POST /mtproto/nodes -> 202 {job_id}; a cover site that fails its test from the
// node -> 422 with the test in data.cover, before anything is installed
func (h *Handler) Install(c *gin.Context) {
	var b installBody
	if !bind(c, &b) {
		return
	}
	id, err := h.svc.StartInstall(c.Request.Context(), relayops.InstallParams{
		Name: b.Name, Login: b.login(), Disguise: b.Disguise, MaxStreams: b.MaxStreams,
		HubPort: b.HubPort, CapturePort: b.CapturePort, AllowUnverifiedDisguise: b.AllowUnverifiedDisguise,
	})
	var cover *relayops.CoverError
	if errors.As(err, &cover) {
		c.JSON(http.StatusUnprocessableEntity, httputil.Response{Error: plain(err.Error()), Data: gin.H{"cover": cover.Result}})
		return
	}
	if err != nil {
		h.fail(c, err)
		return
	}
	h.follow(id, 0, "install", "")
	httputil.JSON(c, http.StatusAccepted, gin.H{"job_id": id})
}

// jobWatch outlasts the longest job (an install's 20 minutes).
const (
	jobWatch     = 25 * time.Minute
	jobWatchTick = 500 * time.Millisecond
)

// follow records how a job ended in its node's feed; an install learns its node on the way.
func (h *Handler) follow(jobID string, nodeID uint, subject, what string) {
	go func() {
		t := time.NewTicker(jobWatchTick)
		defer t.Stop()
		for deadline := time.Now().Add(jobWatch); time.Now().Before(deadline); {
			<-t.C
			v, ok := h.svc.Job(jobID)
			if !ok {
				return
			}
			if v.State == relayops.JobRunning {
				continue
			}
			if nodeID == 0 {
				nodeID = v.NodeID
			}
			if nodeID != 0 {
				h.live.Record(nodeID, jobEvent(subject, what, v))
			}
			return
		}
	}()
}

// jobEvent is a finished job as the feed shows it; an update says which part it was.
func jobEvent(subject, what string, v relayops.JobView) Event {
	if v.State == relayops.JobOK {
		return Event{Kind: kindJobOK, Level: levelInfo, Subject: subject, Detail: what}
	}
	why := plain(v.Error)
	if what != "" {
		why = what + ": " + why
	}
	return Event{Kind: kindJobFailed, Level: levelError, Subject: subject, Detail: why}
}

// GET /mtproto/jobs/:id
func (h *Handler) Job(c *gin.Context) {
	v, ok := h.svc.Job(c.Param("id"))
	if !ok {
		httputil.Error(c, http.StatusNotFound, "no such job")
		return
	}
	httputil.OK(c, plainJob(v))
}

// GET /mtproto/jobs/:id/stream: server-sent progress until the job ends.
func (h *Handler) StreamJob(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.svc.Job(id); !ok {
		httputil.Error(c, http.StatusNotFound, "no such job")
		return
	}
	hd := c.Writer.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-cache")
	hd.Set("Connection", "keep-alive")
	hd.Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	ctx := c.Request.Context()
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	keep := time.NewTicker(15 * time.Second)
	defer keep.Stop()
	var last []byte
	emit := func() (done bool) {
		v, ok := h.svc.Job(id)
		if !ok {
			return true
		}
		b, _ := json.Marshal(plainJob(v))
		if string(b) != string(last) {
			last = b
			fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", b)
			c.Writer.Flush()
		}
		return v.State != relayops.JobRunning
	}
	if emit() {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-keep.C:
			fmt.Fprint(c.Writer, ": keepalive\n\n")
			c.Writer.Flush()
		case <-tick.C:
			if emit() {
				return
			}
		}
	}
}

// NodeView is everything the UI shows about one MTProto node. It has no secrets.
type NodeView struct {
	NodeID        uint       `json:"node_id"`
	AgentID       string     `json:"agent_id"`
	HubPort       int        `json:"hub_port"`
	CapturePort   int        `json:"capture_port"`
	SSHPort       int        `json:"ssh_port"`
	SSHUser       string     `json:"ssh_user"`
	HostKey       string     `json:"host_key"`
	Disguise      string     `json:"disguise"`
	MaxStreams    int        `json:"max_streams"`
	ProxyStopped  bool       `json:"proxy_stopped"` // the operator stopped the proxy
	Arch          string     `json:"arch"`
	AgentVersion  string     `json:"agent_version"`
	TelemtVersion string     `json:"telemt_version"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
	KeysRotatedAt *time.Time `json:"keys_rotated_at,omitempty"`

	Running bool         `json:"running"`
	Link    *LinkView    `json:"link,omitempty"`
	Agent   *ctrl.Status `json:"agent,omitempty"`
	Healthy bool         `json:"healthy"`
	// the panel's last periodic check of the cover site
	DisguiseCheck *DisguiseCheckView `json:"disguise_check,omitempty"`

	DesiredRev  string `json:"desired_rev,omitempty"`
	AppliedRev  string `json:"applied_rev,omitempty"`
	LastPushErr string `json:"last_push_error,omitempty"`
	// the tunnel's cover site the node could not use yet; users still update
	CoverPending string `json:"cover_pending,omitempty"`

	InstalledAt *time.Time  `json:"installed_at,omitempty"`
	Users       *int        `json:"users,omitempty"`
	ActiveUsers *int        `json:"active_users,omitempty"`
	Throughput  *Throughput `json:"throughput,omitempty"`
	Latest      Releases    `json:"latest"`
	Updates     Updates     `json:"updates"`
}

// Releases are what this panel installs.
type Releases struct {
	Agent  string `json:"agent"`
	Telemt string `json:"telemt"`
	// AgentBuild is the SHA-256 of the agent it would install here: dev builds share a version
	AgentBuild string `json:"agent_build,omitempty"`
}

// Updates are what the node can take: it runs something else than the panel installs.
type Updates struct {
	Agent  bool `json:"agent"`
	Telemt bool `json:"telemt"`
}

var latest = Releases{Agent: release.AgentVersion, Telemt: release.TelemtVersion}

type LinkView struct {
	Up           int        `json:"up"`
	Conns        []ConnView `json:"conns"`
	Streams      int64      `json:"streams"`
	Relayed      uint64     `json:"relayed"`
	DialFailures uint64     `json:"dial_failures"`
	Violations   uint64     `json:"violations"`
	BytesToDC    uint64     `json:"bytes_to_dc"`
	BytesFromDC  uint64     `json:"bytes_from_dc"`
}

type ConnView struct {
	Name    string     `json:"name"`
	Up      bool       `json:"up"`
	Since   *time.Time `json:"since,omitempty"`
	RTTms   int64      `json:"rtt_ms"`
	Streams int        `json:"streams"`
	Error   string     `json:"error,omitempty"`
	SkewMS  int64      `json:"skew_ms"` // the node's clock minus the panel's
}

type DisguiseCheckView struct {
	ctrl.DisguiseResult
	CheckedAt time.Time `json:"checked_at"`
}

// GET /mtproto/nodes/:id
func (h *Handler) Node(c *gin.Context) {
	id, ok := nodeID(c)
	if !ok {
		return
	}
	out, err := h.view(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	httputil.OK(c, out)
}

func (h *Handler) view(ctx context.Context, id uint) (NodeView, error) {
	row, err := h.live.Row(ctx, id)
	if err != nil {
		return NodeView{}, err
	}
	out := NodeView{
		NodeID: row.NodeID, AgentID: row.AgentID, HubPort: row.HubPort, CapturePort: row.CapturePort,
		SSHPort: row.SSHPort, SSHUser: row.SSHUser, HostKey: row.HostKey, Disguise: row.Disguise,
		MaxStreams: row.MaxStreams, ProxyStopped: row.ProxyStopped, Arch: row.Arch, AgentVersion: row.AgentVersion, TelemtVersion: row.TelemtVersion, LastSeenAt: row.LastSeenAt,
		KeysRotatedAt: row.KeysRotatedAt, Latest: latest,
	}
	v, running := h.live.View(id)
	var rep relayops.LinkView
	if running {
		rep = report(v)
	}
	due := h.svc.DueFor(rep, row.Arch)
	out.Latest.AgentBuild, out.Updates = due.AgentBuild, Updates{Agent: due.Agent, Telemt: due.Telemt}
	if !row.CreatedAt.IsZero() {
		at := row.CreatedAt
		out.InstalledAt = &at
	}
	if running {
		s := v.Snapshot
		out.Running, out.Agent, out.Healthy = true, s.Agent, s.Healthy
		out.DesiredRev, out.AppliedRev, out.LastPushErr = s.DesiredRev, s.AppliedRev, s.LastPushErr
		out.CoverPending = v.CoverPending
		out.Users, out.ActiveUsers, out.Throughput = v.Users, v.ActiveUsers, v.Throughput
		lv := &LinkView{Up: s.Link.Up, Streams: s.Link.Streams, Relayed: s.Link.Relayed, DialFailures: s.Link.DialFailures,
			Violations: v.Violations, BytesToDC: s.Link.BytesToDC, BytesFromDC: s.Link.BytesFromDC}
		if s.Disguise != nil {
			out.DisguiseCheck = &DisguiseCheckView{DisguiseResult: *s.Disguise, CheckedAt: s.DisguiseAt}
		}
		for _, cs := range s.Link.Conns {
			cv := ConnView{Name: cs.Name, Up: cs.Up, RTTms: cs.RTT.Milliseconds(), Streams: cs.Streams, Error: cs.LastErr, SkewMS: cs.Skew.Milliseconds()}
			if !cs.Since.IsZero() {
				t := cs.Since
				cv.Since = &t
			}
			lv.Conns = append(lv.Conns, cv)
		}
		out.Link = lv
	}
	return out, nil
}

// NodeSummary is one MTProto node as the nodes list shows it; a stopped one has only what its row knows.
type NodeSummary struct {
	NodeID       uint   `json:"node_id"`
	Running      bool   `json:"running"`
	Healthy      bool   `json:"healthy"`
	LinkUp       int    `json:"link_up"`
	LinkConns    int    `json:"link_conns"`
	RTTms        *int64 `json:"rtt_ms,omitempty"` // the connections that are up, on average
	Streams      int64  `json:"streams"`
	MaxStreams   int    `json:"max_streams"`
	ProxyRunning bool   `json:"proxy_running"`
	ProxyStopped bool   `json:"proxy_stopped,omitempty"` // the operator stopped it
	ProxyHeld    string `json:"proxy_held,omitempty"`
	CrashLoop    bool   `json:"crash_loop,omitempty"`
	Users        *int   `json:"users,omitempty"`
	ActiveUsers  *int   `json:"active_users,omitempty"`
	UpBps        uint64 `json:"up_bps"`
	DownBps      uint64 `json:"down_bps"`
	AgentVersion string `json:"agent_version"`
	LatestAgent  string `json:"latest_agent"`
	AgentUpdate  bool   `json:"agent_update"`
}

// GET /mtproto/nodes
func (h *Handler) List(c *gin.Context) {
	rows, err := h.live.Rows(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].NodeID < rows[j].NodeID })
	out := make([]NodeSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, h.summary(row))
	}
	httputil.OK(c, out)
}

func (h *Handler) summary(row *LiteNode) NodeSummary {
	out := NodeSummary{NodeID: row.NodeID, MaxStreams: row.MaxStreams, AgentVersion: row.AgentVersion, LatestAgent: latest.Agent, ProxyStopped: row.ProxyStopped}
	v, running := h.live.View(row.NodeID)
	if !running {
		return out
	}
	out.AgentUpdate = h.svc.DueFor(report(v), row.Arch).Agent
	s := v.Snapshot
	out.Running, out.Healthy = true, s.Healthy
	out.LinkUp, out.LinkConns, out.Streams = s.Link.Up, len(s.Link.Conns), s.Link.Streams
	var sum, n int64
	for _, cs := range s.Link.Conns {
		if cs.Up && cs.RTT > 0 { // up but not timed yet says nothing
			sum, n = sum+cs.RTT.Milliseconds(), n+1
		}
	}
	if n > 0 {
		avg := sum / n
		out.RTTms = &avg
	}
	if a := s.Agent; a != nil {
		if a.AgentVersion != "" {
			out.AgentVersion = a.AgentVersion
		}
		// with the link down the last report is stale
		if s.Link.Up > 0 {
			out.ProxyRunning, out.ProxyHeld, out.CrashLoop = a.Telemt.Running, a.Telemt.Held, a.Telemt.CrashLoop
		}
	}
	out.Users, out.ActiveUsers = v.Users, v.ActiveUsers
	if v.Throughput != nil {
		out.UpBps, out.DownBps = v.Throughput.UpBps, v.Throughput.DownBps
	}
	return out
}

// GET /mtproto/nodes/:id/events?limit=50
func (h *Handler) Events(c *gin.Context) {
	id, ok := nodeID(c)
	if !ok {
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit < 1 || limit > feedCap {
		httputil.Error(c, http.StatusBadRequest, fmt.Sprintf("limit must be 1 to %d", feedCap))
		return
	}
	if _, err := h.live.Row(c.Request.Context(), id); err != nil {
		h.fail(c, err)
		return
	}
	httputil.OK(c, h.live.Events(id, limit))
}

type settingsBody struct {
	MaxStreams *int    `json:"max_streams"`
	SSHPort    *int    `json:"ssh_port"`
	SSHUser    *string `json:"ssh_user"`
}

// validSSHUser keeps it a plain account name; upper case is allowed, odd as it is on Linux.
func validSSHUser(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		letter := 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '_'
		if !letter && (i == 0 || !('0' <= c && c <= '9' || c == '.' || c == '-')) {
			return false
		}
	}
	return true
}

func (b settingsBody) check() string {
	switch {
	case b.MaxStreams == nil && b.SSHPort == nil && b.SSHUser == nil:
		return "nothing to change: give max_streams, ssh_port or ssh_user"
	case b.MaxStreams != nil && (*b.MaxStreams < MinStreamLimit || *b.MaxStreams > MaxStreamLimit):
		return fmt.Sprintf("the stream limit must be %d to %d", MinStreamLimit, MaxStreamLimit)
	case b.SSHPort != nil && (*b.SSHPort < 1 || *b.SSHPort > 65535):
		return "the SSH port must be 1 to 65535"
	case b.SSHUser != nil && !validSSHUser(*b.SSHUser):
		return "the SSH user must be 1 to 32 letters, digits, dots, dashes or underscores, starting with a letter or underscore"
	}
	return ""
}

// PATCH /mtproto/nodes/:id  {max_streams?, ssh_port?, ssh_user?} -> the node's view
func (h *Handler) Settings(c *gin.Context) {
	id, ok := nodeID(c)
	if !ok {
		return
	}
	var b settingsBody
	if !bind(c, &b) {
		return
	}
	if msg := b.check(); msg != "" {
		httputil.Error(c, http.StatusBadRequest, msg)
		return
	}
	ctx := c.Request.Context()
	if err := h.live.UpdateSettings(ctx, id, Settings{MaxStreams: b.MaxStreams, SSHPort: b.SSHPort, SSHUser: b.SSHUser}); err != nil {
		h.fail(c, err)
		return
	}
	out, err := h.view(ctx, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	httputil.OK(c, out)
}

type repairBody struct {
	loginBody
	Reset bool `json:"reset"`
}

// POST /mtproto/nodes/:id/repair  {ssh_password|ssh_key, reset} -> 202 {job_id}
func (h *Handler) Repair(c *gin.Context) {
	id, ok := nodeID(c)
	if !ok {
		return
	}
	var b repairBody
	if !bind(c, &b) {
		return
	}
	jid, err := h.svc.StartRepair(c.Request.Context(), id, b.login(), b.Reset)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.follow(jid, id, "repair", "")
	httputil.JSON(c, http.StatusAccepted, gin.H{"job_id": jid})
}

type deleteBody struct {
	loginBody
	Force bool `json:"force"`
}

// POST /mtproto/nodes/:id/remove: a POST because proxies may drop a DELETE's body.
func (h *Handler) Delete(c *gin.Context) {
	id, ok := nodeID(c)
	if !ok {
		return
	}
	var b deleteBody
	if !bind(c, &b) {
		return
	}
	var l *relayops.Login
	if b.Password != "" || b.Key != "" {
		lg := b.login()
		l = &lg
	}
	jid, err := h.svc.StartUninstall(c.Request.Context(), id, l, b.Force)
	if err != nil {
		h.fail(c, err)
		return
	}
	httputil.JSON(c, http.StatusAccepted, gin.H{"job_id": jid})
}

// POST /mtproto/nodes/:id/update  {kind: agent|telemt} -> 202 {job_id}
func (h *Handler) Update(c *gin.Context) {
	id, ok := nodeID(c)
	if !ok {
		return
	}
	var b struct {
		Kind string `json:"kind"`
	}
	if !bind(c, &b) {
		return
	}
	jid, err := h.svc.StartUpdate(c.Request.Context(), id, b.Kind)
	if err != nil {
		h.fail(c, err)
		return
	}
	what := "agent"
	if b.Kind == "telemt" {
		what = "proxy"
	}
	h.follow(jid, id, "update", what)
	httputil.JSON(c, http.StatusAccepted, gin.H{"job_id": jid})
}

// POST /mtproto/nodes/:id/rotate -> 202 {job_id}
func (h *Handler) Rotate(c *gin.Context) {
	id, ok := nodeID(c)
	if !ok {
		return
	}
	jid, err := h.svc.StartRotate(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.follow(jid, id, "rotate", "")
	httputil.JSON(c, http.StatusAccepted, gin.H{"job_id": jid})
}

// POST /mtproto/nodes/:id/link-port  {port} -> 202 {job_id}: move the link; a port the panel can't reach changes nothing.
func (h *Handler) LinkPort(c *gin.Context) {
	id, ok := nodeID(c)
	if !ok {
		return
	}
	var b struct {
		Port int `json:"port"`
	}
	if !bind(c, &b) {
		return
	}
	jid, err := h.svc.StartLinkPort(c.Request.Context(), id, b.Port)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.follow(jid, id, "link-port", fmt.Sprintf("port %d", b.Port))
	httputil.JSON(c, http.StatusAccepted, gin.H{"job_id": jid})
}

// POST /mtproto/nodes/:id/address  {address} -> 202 {job_id}: point the panel at
// the node's new address; nothing answering there changes nothing.
func (h *Handler) Address(c *gin.Context) {
	id, ok := nodeID(c)
	if !ok {
		return
	}
	var b struct {
		Address string `json:"address"`
	}
	if !bind(c, &b) {
		return
	}
	jid, err := h.svc.StartAddress(c.Request.Context(), id, b.Address)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.follow(jid, id, "address", strings.TrimSpace(b.Address))
	httputil.JSON(c, http.StatusAccepted, gin.H{"job_id": jid})
}

func (h *Handler) call(c *gin.Context, method string, req, resp any) bool {
	id, ok := nodeID(c)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if err := h.live.Call(ctx, id, method, req, resp); err != nil {
		h.fail(c, err)
		return false
	}
	return true
}

// ErrProxyStopped: a restart would do nothing while the operator keeps the proxy stopped.
var ErrProxyStopped = errors.New("the proxy is stopped: start it instead")

// POST /mtproto/nodes/:id/restart: restart telemt.
func (h *Handler) Restart(c *gin.Context) {
	id, ok := nodeID(c)
	if !ok {
		return
	}
	if row, err := h.live.Row(c.Request.Context(), id); err != nil {
		h.fail(c, err)
		return
	} else if row.ProxyStopped {
		h.fail(c, ErrProxyStopped)
		return
	}
	if h.call(c, ctrl.MServiceRestart, nil, nil) {
		httputil.OK(c, gin.H{"restarted": true})
	}
}

// POST /mtproto/nodes/:id/stop: keep telemt off until started; users stay on the list.
func (h *Handler) Stop(c *gin.Context) { h.setStopped(c, true) }

// POST /mtproto/nodes/:id/start
func (h *Handler) Start(c *gin.Context) { h.setStopped(c, false) }

// setStopped saves the choice and pushes it, so it holds across agent and panel restarts.
func (h *Handler) setStopped(c *gin.Context, stopped bool) {
	id, ok := nodeID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if err := h.live.UpdateSettings(ctx, id, Settings{ProxyStopped: &stopped}); err != nil {
		h.fail(c, err)
		return
	}
	out, err := h.view(ctx, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	httputil.OK(c, out)
}

// GET /mtproto/nodes/:id/logs?lines=200: the agent's in-memory log, addresses masked.
func (h *Handler) Logs(c *gin.Context) {
	n, _ := strconv.Atoi(c.DefaultQuery("lines", "200"))
	var out ctrl.LogsResponse
	if h.call(c, ctrl.MLogsTail, ctrl.LogsRequest{Lines: n}, &out) {
		for i, l := range out.Lines {
			out.Lines[i] = hideSecrets(l)
		}
		httputil.OK(c, out)
	}
}

// telemt prints every user's proxy link at start; the agent hides addresses but not those
var secretRE = regexp.MustCompile(`(?i)(secret\s*[=:]\s*)[^\s&"']+`)

func hideSecrets(line string) string {
	return secretRE.ReplaceAllString(line, "${1}[hidden]")
}

// POST /mtproto/nodes/:id/disguise-check  {domain}: can the VPS use this site as cover?
func (h *Handler) DisguiseCheck(c *gin.Context) {
	var b ctrl.DisguiseCheck
	if !bind(c, &b) {
		return
	}
	if !relayops.ValidDomain(b.Domain) {
		httputil.Error(c, http.StatusBadRequest, "that is not a domain name")
		return
	}
	var out ctrl.DisguiseResult
	if h.call(c, ctrl.MDisguiseCheck, b, &out) {
		out.Domestic = h.live.Domestic(out.Addrs)
		httputil.OK(c, out)
	}
}

// GET /mtproto/allowlist
func (h *Handler) Allowlist(c *gin.Context) {
	ok, lastErr := h.allow.Status()
	url := ""
	if h.allow.URL != nil {
		url = h.allow.URL()
	}
	// url is where the daily refresh fetches from; empty means it's turned off
	body := gin.H{"prefixes": len(h.allow.List.Prefixes()), "placeholder": tgnet.PinnedIsPlaceholder(), "last_error": lastErr, "url": url}
	if !ok.IsZero() {
		body["refreshed_at"] = ok
	}
	httputil.OK(c, body)
}

// POST /mtproto/allowlist/refresh
func (h *Handler) RefreshAllowlist(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := h.allow.RefreshNow(ctx); err != nil {
		httputil.Error(c, http.StatusBadGateway, err.Error())
		return
	}
	h.Allowlist(c)
}
