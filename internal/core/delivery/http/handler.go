// Package http exposes admin CRUD for the tunnel cores (core_inbounds /
// core_users) and per-user client links.
//
// All responses use the shared pkg/httputil envelope ({success,data} on
// success, {success:false,error} on failure) so the web panel consumes them
// with the same res.data unwrap as every other admin endpoint.
package http

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/core/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
)

type Handler struct {
	uc       *usecase.CoreUsecase
	nodeHost func(c *gin.Context, nodeID uint) string
}

// NewHandler builds the core HTTP handler. hostFn resolves a node's public host
// for client links (may be nil; then the request host is used).
func NewHandler(uc *usecase.CoreUsecase, hostFn func(c *gin.Context, nodeID uint) string) *Handler {
	return &Handler{uc: uc, nodeHost: hostFn}
}

// RegisterRoutes mounts the core routes under an (already-authenticated) group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/cores")
	{
		g.GET("/nodes/:nodeID/inbounds", h.ListInbounds)
		g.GET("/inbounds", h.ListAllInbounds)
		g.POST("/inbounds", h.CreateInbound)
		g.PUT("/inbounds/:id", h.UpdateInbound)
		g.DELETE("/inbounds/:id", h.DeleteInbound)
		g.GET("/inbounds/:id/status", h.GetInboundStatus)
		g.GET("/inbounds/:id/users", h.ListUsers)
		g.POST("/inbounds/:id/users", h.CreateUser)
		g.PUT("/users/:id", h.UpdateUser)
		g.POST("/users/:id/toggle", h.ToggleUser)
		g.DELETE("/users/:id", h.DeleteUser)
		g.GET("/users/:id/config", h.GetUserConfig)

		// Per-node, per-protocol enable/disable.
		g.GET("/nodes/:nodeID/protocols", h.ListNodeProtocols)
		g.POST("/nodes/:nodeID/protocols/:protocol/toggle", h.ToggleNodeProtocol)
	}
}

func atoiParam(c *gin.Context, name string) (uint, bool) {
	v, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		httputil.Error(c, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return uint(v), true
}

func (h *Handler) ListInbounds(c *gin.Context) {
	nodeID, ok := atoiParam(c, "nodeID")
	if !ok {
		return
	}
	list, err := h.uc.ListInbounds(c.Request.Context(), nodeID)
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.OK(c, gin.H{"inbounds": list})
}

// ListAllInbounds returns every core_inbound across all nodes for the
// cross-node tunnels overview. Users are preloaded so the card grid shows
// member counts; Secret is json:"-" so no credential leaks.
func (h *Handler) ListAllInbounds(c *gin.Context) {
	list, err := h.uc.ListAllInbounds(c.Request.Context())
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.OK(c, gin.H{"inbounds": list})
}

type createInboundReq struct {
	NodeID     uint            `json:"node_id" binding:"required"`
	Protocol   string          `json:"protocol" binding:"required"`
	Listen     string          `json:"listen"`
	Port       int             `json:"port" binding:"required"`
	EgressMode string          `json:"egress_mode"`
	Remark     string          `json:"remark"`
	Settings   json.RawMessage `json:"settings"`
	IsDisabled bool            `json:"is_disabled"`
}

func (h *Handler) CreateInbound(c *gin.Context) {
	var req createInboundReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	in := &domain.CoreInbound{
		NodeID:     req.NodeID,
		Protocol:   domain.CoreProtocol(req.Protocol),
		Listen:     req.Listen,
		Port:       req.Port,
		EgressMode: domain.EgressMode(req.EgressMode),
		Remark:     req.Remark,
		Settings:   req.Settings,
		IsDisabled: req.IsDisabled,
	}
	if err := h.uc.CreateInbound(c.Request.Context(), in); err != nil {
		httputil.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	warn := reconcileWarning(h.uc.ReconcileNodeByID(c.Request.Context(), in.NodeID))
	httputil.Created(c, gin.H{"inbound": in, "reconcile_warning": warn})
}

// UpdateInbound edits an existing core_inbound in place (settings, port, listen,
// remark, egress, enabled) and reconciles the node. Protocol/node are immutable.
func (h *Handler) UpdateInbound(c *gin.Context) {
	id, ok := atoiParam(c, "id")
	if !ok {
		return
	}
	var req createInboundReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	in := &domain.CoreInbound{
		ID:         id,
		NodeID:     req.NodeID,
		Protocol:   domain.CoreProtocol(req.Protocol),
		Listen:     req.Listen,
		Port:       req.Port,
		EgressMode: domain.EgressMode(req.EgressMode),
		Remark:     req.Remark,
		Settings:   req.Settings,
		IsDisabled: req.IsDisabled,
	}
	if err := h.uc.UpdateInbound(c.Request.Context(), in); err != nil {
		httputil.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	warn := reconcileWarning(h.uc.ReconcileNodeByID(c.Request.Context(), in.NodeID))
	httputil.OK(c, gin.H{"inbound": in, "reconcile_warning": warn})
}

func (h *Handler) DeleteInbound(c *gin.Context) {
	id, ok := atoiParam(c, "id")
	if !ok {
		return
	}
	in, err := h.uc.GetInbound(c.Request.Context(), id)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if err := h.uc.DeleteInbound(c.Request.Context(), id); err != nil {
		httputil.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	warn := reconcileWarning(h.uc.ReconcileNodeByID(c.Request.Context(), in.NodeID))
	httputil.OK(c, gin.H{"deleted": id, "reconcile_warning": warn})
}

// GetInboundStatus proxies the agent's live status for the inbound's protocol
// core (running/version/live sessions). Returns 503 when no agent is reachable.
func (h *Handler) GetInboundStatus(c *gin.Context) {
	id, ok := atoiParam(c, "id")
	if !ok {
		return
	}
	st, err := h.uc.GetInboundStatus(c.Request.Context(), id)
	if err != nil {
		httputil.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	httputil.OK(c, st)
}

func (h *Handler) ListUsers(c *gin.Context) {
	id, ok := atoiParam(c, "id")
	if !ok {
		return
	}
	list, err := h.uc.ListUsers(c.Request.Context(), id)
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.OK(c, gin.H{"users": list})
}

type createUserReq struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
	Email    string `json:"email" binding:"required"`
	KLimit   int    `json:"k_limit"`
}

func (h *Handler) CreateUser(c *gin.Context) {
	inboundID, ok := atoiParam(c, "id")
	if !ok {
		return
	}
	var req createUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	u := &domain.CoreUser{
		CoreInboundID: inboundID,
		Username:      req.Username,
		Secret:        req.Secret,
		Email:         req.Email,
		KLimit:        req.KLimit,
	}
	if err := h.uc.CreateUser(c.Request.Context(), u); err != nil {
		httputil.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	in, _ := h.uc.GetInbound(c.Request.Context(), inboundID)
	var warn string
	if in != nil {
		warn = reconcileWarning(h.uc.ReconcileNodeByID(c.Request.Context(), in.NodeID))
	}
	httputil.Created(c, gin.H{"user": u, "reconcile_warning": warn})
}

type updateUserReq struct {
	Username string `json:"username"`
	Secret   string `json:"secret"` // blank preserves the existing secret
	Email    string `json:"email"`
	KLimit   int    `json:"k_limit"`
}

// UpdateUser edits a core_user's profile (email/username/k_limit, optionally a
// new secret) in place and reconciles the node. The enabled flag is owned by
// ToggleUser; a blank secret preserves the current one.
func (h *Handler) UpdateUser(c *gin.Context) {
	id, ok := atoiParam(c, "id")
	if !ok {
		return
	}
	var req updateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	u := &domain.CoreUser{
		ID:       id,
		Username: req.Username,
		Secret:   req.Secret,
		Email:    req.Email,
		KLimit:   req.KLimit,
	}
	if err := h.uc.UpdateUser(c.Request.Context(), u); err != nil {
		httputil.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	var warn string
	if in, e := h.uc.GetInbound(c.Request.Context(), u.CoreInboundID); e == nil {
		warn = reconcileWarning(h.uc.ReconcileNodeByID(c.Request.Context(), in.NodeID))
	}
	httputil.OK(c, gin.H{"user": u, "reconcile_warning": warn})
}

// ToggleUser flips a core_user's enabled flag (instant enable/disable) and
// reconciles the node so the agent permits or sweep-kills the credential.
func (h *Handler) ToggleUser(c *gin.Context) {
	id, ok := atoiParam(c, "id")
	if !ok {
		return
	}
	cur, err := h.uc.GetUser(c.Request.Context(), id)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, err.Error())
		return
	}
	u, err := h.uc.SetUserEnabled(c.Request.Context(), id, !cur.Enabled)
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	var warn string
	if in, e := h.uc.GetInbound(c.Request.Context(), u.CoreInboundID); e == nil {
		warn = reconcileWarning(h.uc.ReconcileNodeByID(c.Request.Context(), in.NodeID))
	}
	httputil.OK(c, gin.H{"user": u, "reconcile_warning": warn})
}

func (h *Handler) DeleteUser(c *gin.Context) {
	id, ok := atoiParam(c, "id")
	if !ok {
		return
	}
	u, err := h.uc.GetUser(c.Request.Context(), id)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if err := h.uc.DeleteUser(c.Request.Context(), id); err != nil {
		httputil.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	var warn string
	if in, e := h.uc.GetInbound(c.Request.Context(), u.CoreInboundID); e == nil {
		warn = reconcileWarning(h.uc.ReconcileNodeByID(c.Request.Context(), in.NodeID))
	}
	httputil.OK(c, gin.H{"deleted": id, "reconcile_warning": warn})
}

func (h *Handler) GetUserConfig(c *gin.Context) {
	id, ok := atoiParam(c, "id")
	if !ok {
		return
	}
	u, err := h.uc.GetUser(c.Request.Context(), id)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, err.Error())
		return
	}
	host := c.Query("host")
	if host == "" {
		if in, e := h.uc.GetInbound(c.Request.Context(), u.CoreInboundID); e == nil && h.nodeHost != nil {
			host = h.nodeHost(c, in.NodeID)
		}
	}
	if host == "" {
		// The address the admin reached the panel on, without the panel's port.
		host = c.Request.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	art, err := h.uc.GenerateUserConfig(c.Request.Context(), id, host)
	if err != nil {
		httputil.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	httputil.OK(c, art)
}

// ListNodeProtocols returns every core protocol's enable state + inbound count
// for the node (absent settings reported as enabled).
func (h *Handler) ListNodeProtocols(c *gin.Context) {
	nodeID, ok := atoiParam(c, "nodeID")
	if !ok {
		return
	}
	list, err := h.uc.ListNodeProtocols(c.Request.Context(), nodeID)
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.OK(c, gin.H{"protocols": list})
}

// ToggleNodeProtocol flips a protocol's per-node enable flag and reconciles the
// node so its listeners come up or tear down.
func (h *Handler) ToggleNodeProtocol(c *gin.Context) {
	nodeID, ok := atoiParam(c, "nodeID")
	if !ok {
		return
	}
	proto, enabled, err := h.uc.ToggleNodeProtocol(c.Request.Context(), nodeID, c.Param("protocol"))
	if err != nil {
		httputil.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	warn := reconcileWarning(h.uc.ReconcileNodeByID(c.Request.Context(), nodeID))
	httputil.OK(c, gin.H{"protocol": proto, "enabled": enabled, "reconcile_warning": warn})
}

func reconcileWarning(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
