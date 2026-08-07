package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	xrayUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/xray/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
	"github.com/sirupsen/logrus"
)

// isNodeIP reports whether s is a bare IP literal we can dial a node on. Zoned
// forms like "fe80::1%eth0" are rejected: they're not routable off-link.
func isNodeIP(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Zone() == ""
}

// statusForRoutingError maps usecase routing errors to HTTP status. Validation
// errors carry usecase.ErrInvalidRoutingRule and surface as 400 instead of 500.
func statusForRoutingError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if errors.Is(err, usecase.ErrInvalidRoutingRule) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// statusForDNSError maps DNS / FakeDNS validation errors to 400.
func statusForDNSError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if errors.Is(err, usecase.ErrInvalidDNSConfig) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

type Handler struct {
	nodeUsecase usecase.NodeUsecase
	httpFactory *httpclient.Factory
}

func NewHandler(nodeUsecase usecase.NodeUsecase) *Handler {
	return &Handler{nodeUsecase: nodeUsecase}
}

// SetHTTPClientFactory injects the outbound HTTP factory so endpoints that
// hit external services (e.g. GitHub release list) can honor proxy settings.
func (h *Handler) SetHTTPClientFactory(f *httpclient.Factory) {
	h.httpFactory = f
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	nodes := rg.Group("/nodes")
	{
		// Xray Releases (before :id routes to avoid conflict)
		nodes.GET("/xray/releases", h.GetXrayReleases)

		// Tools
		nodes.GET("/tools/vless-keys", h.GenerateVLESSKeys)
		nodes.GET("/tools/x25519-keys", h.GenerateX25519Keys)

		// Bulk stats (before :id routes to avoid conflict)
		nodes.GET("/stats/bulk", h.GetNodesStatsBulk)
		nodes.GET("/stats/history/bulk", h.GetNodesStatsHistoryBulk)

		// Bulk actions (before :id routes to avoid conflict)
		nodes.POST("/bulk/restart", h.BulkRestartXray)
		nodes.POST("/bulk/push-config", h.BulkPushConfig)
		nodes.POST("/bulk/health-check", h.BulkCheckHealth)
		nodes.POST("/bulk/xray-version", h.BulkUpdateXrayVersion)

		// Node CRUD
		nodes.GET("", h.ListNodes)
		nodes.GET("/:id", h.GetNode)
		nodes.POST("", h.CreateNode)
		nodes.PUT("/:id", h.UpdateNode)
		nodes.DELETE("/:id", h.DeleteNode)
		nodes.POST("/:id/wipe", h.Wipe)
		nodes.POST("/:id/nuke", h.Nuke)
		nodes.GET("/:id/health", h.CheckHealth)
		nodes.GET("/:id/stats", h.GetNodeStats)
		nodes.GET("/:id/stats/history", h.GetNodeStatsHistory)
		nodes.GET("/:id/info", h.GetNodeHostInfo)
		nodes.GET("/:id/users", h.GetNodeRealtimeUsers)
		nodes.GET("/:id/traffic/daily", h.GetNodeDailyTraffic)
		nodes.GET("/:id/uptime", h.GetNodeUptimeEvents)

		// Agent Management
		nodes.GET("/:id/agent/update/stream", h.UpdateAgentStream)
		nodes.POST("/:id/agent/update", h.UpdateAgent)
		nodes.POST("/:id/agent/start", h.StartXray)
		nodes.POST("/:id/agent/stop", h.StopXray)
		nodes.POST("/:id/agent/restart", h.RestartXray)
		nodes.POST("/:id/agent/push-config", h.PushConfig)
		nodes.POST("/:id/agent/xray-version", h.UpdateXrayVersion)
		nodes.POST("/:id/agent/geofiles", h.UpdateGeoFiles)

		// Inbound Operations
		nodes.GET("/:id/inbounds", h.ListInbounds)
		nodes.POST("/:id/inbounds", h.AddInbound)
		nodes.POST("/:id/inbounds/discover", h.DiscoverInbounds)
		nodes.POST("/:id/inbounds/sync", h.SyncInbounds)

		// Xray Config Editor
		nodes.GET("/:id/xray-config", h.GetXrayConfig)
		nodes.PUT("/:id/xray-config", h.UpdateXrayConfig)
		nodes.POST("/:id/xray-config/validate", h.ValidateXrayConfig)
		nodes.GET("/:id/xray-config/diff", h.GetXrayConfigDiff)

		// Logs
		nodes.GET("/:id/logs/stream", h.StreamLogs)
		nodes.GET("/:id/logs/tail", h.TailLogs)

		// Terminal (WebSocket)
		nodes.GET("/:id/terminal/ws", h.TerminalWebSocket)

		// SSH Management
		nodes.GET("/:id/ssh", h.GetNodeSSHStatus)
		nodes.PUT("/:id/ssh", h.UpdateNodeSSHConfig)
		nodes.DELETE("/:id/ssh/logs", h.ClearNodeSSHLogs)
		nodes.POST("/:id/ssh/restart", h.RestartNodeSSH)

		// Access Logs (per-subscription DNS logs)
		nodes.GET("/:id/access-logs", h.GetAccessLogs)

		// Starlink Monitoring
		nodes.GET("/:id/starlink/status", h.GetStarlinkStatus)
		nodes.GET("/:id/starlink/obstruction-map", h.GetStarlinkObstructionMap)
		nodes.GET("/:id/starlink/history", h.GetStarlinkHistory)

		// Migration
		nodes.POST("/:id/migrate", h.MigrateNode)
	}

	// Aggregated Access Logs (cross-node)
	rg.GET("/access-logs", h.GetAggregatedAccessLogs)
	rg.GET("/access-logs/analytics", h.GetAccessLogAnalytics)
	rg.GET("/access-logs/top-domains", h.GetAccessLogTopDomains)

	inbounds := rg.Group("/inbounds")
	{
		inbounds.GET("/:id", h.GetInbound)
		inbounds.PUT("/:id", h.UpdateInbound)
		inbounds.DELETE("/:id", h.DeleteInbound)
		inbounds.POST("/:id/toggle", h.ToggleInboundDisabled)
		inbounds.GET("/:id/stats", h.GetInboundStats)
		inbounds.POST("/:id/migrate", h.MigrateInbound)

		// Host Operations (scoped under inbound)
		inbounds.GET("/:id/hosts", h.ListHosts)
		inbounds.POST("/:id/hosts", h.AddHost)
	}

	hosts := rg.Group("/hosts")
	{
		hosts.GET("", h.ListAllHosts)
		hosts.POST("", h.CreateHost)
		hosts.POST("/bulk-create", h.BulkCreateInfoHosts)
		hosts.PUT("/bulk", h.BulkUpdateHosts)
		hosts.GET("/tags", h.ListHostTags)
		hosts.GET("/:id", h.GetHost)
		hosts.PUT("/:id", h.UpdateHost)
		hosts.DELETE("/:id", h.DeleteHost)
		hosts.POST("/:id/duplicate", h.DuplicateHost)
	}

	templates := rg.Group("/host-templates")
	{
		templates.GET("", h.ListHostTemplates)
		templates.POST("", h.CreateHostTemplate)
		templates.GET("/:id", h.GetHostTemplate)
		templates.PUT("/:id", h.UpdateHostTemplate)
		templates.DELETE("/:id", h.DeleteHostTemplate)
		templates.POST("/:id/apply", h.ApplyHostTemplate)
	}

	// Outbound Operations
	nodes.GET("/:id/outbounds", h.ListOutbounds)
	nodes.POST("/:id/outbounds", h.AddOutbound)
	nodes.POST("/:id/outbounds/discover", h.DiscoverOutbounds)
	nodes.POST("/:id/outbounds/sync", h.SyncOutbounds)

	outbounds := rg.Group("/outbounds")
	{
		outbounds.GET("/:id", h.GetOutbound)
		outbounds.PUT("/:id", h.UpdateOutbound)
		outbounds.DELETE("/:id", h.DeleteOutbound)
		outbounds.POST("/:id/test", h.TestOutbound)
		outbounds.POST("/:id/toggle", h.ToggleOutboundDisabled)
	}

	// Routing Settings (per-node basic routing presets)
	nodes.GET("/:id/routing-settings", h.GetRoutingSettings)
	nodes.PUT("/:id/routing-settings", h.UpdateRoutingSettings)

	// DNS Settings (per-node DNS configuration)
	nodes.GET("/:id/dns-settings", h.GetDNSSettings)
	nodes.PUT("/:id/dns-settings", h.UpdateDNSSettings)
	nodes.DELETE("/:id/dns-settings", h.DeleteDNSSettings)

	// FakeDNS Settings
	nodes.GET("/:id/fakedns-settings", h.GetFakeDNSSettings)
	nodes.PUT("/:id/fakedns-settings", h.UpdateFakeDNSSettings)
	nodes.DELETE("/:id/fakedns-settings", h.DeleteFakeDNSSettings)

	// Routing Rules Operations
	nodes.GET("/:id/routing", h.ListRoutingRules)
	nodes.POST("/:id/routing", h.AddRoutingRule)
	nodes.POST("/:id/routing/sync", h.SyncRoutingRules)
	nodes.POST("/:id/routing/reorder", h.ReorderRoutingRules)

	routing := rg.Group("/routing")
	{
		routing.GET("/:id", h.GetRoutingRule)
		routing.PUT("/:id", h.UpdateRoutingRule)
		routing.DELETE("/:id", h.DeleteRoutingRule)
		routing.POST("/:id/move", h.MoveRoutingRule)
		routing.POST("/:id/toggle", h.ToggleRoutingRule)
	}

	// Balancing Rule Operations
	nodes.GET("/:id/balancing", h.ListBalancingRules)
	nodes.POST("/:id/balancing", h.AddBalancingRule)

	balancing := rg.Group("/balancing")
	{
		balancing.PUT("/:id", h.UpdateBalancingRule)
		balancing.DELETE("/:id", h.DeleteBalancingRule)
	}

	// Reverse Proxy Operations
	nodes.GET("/:id/reverse-proxies", h.ListReverseProxies)
	nodes.POST("/:id/reverse-proxies", h.AddReverseProxy)

	reverseProxies := rg.Group("/reverse-proxies")
	{
		reverseProxies.GET("/:id", h.GetReverseProxy)
		reverseProxies.PUT("/:id", h.UpdateReverseProxy)
		reverseProxies.DELETE("/:id", h.DeleteReverseProxy)
	}
}

// ==================== Node Operations ====================

func (h *Handler) ListNodes(c *gin.Context) {
	nodes, err := h.nodeUsecase.ListNodes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": nodes})
}

func (h *Handler) GetNode(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	node, err := h.nodeUsecase.GetNode(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": node})
}

type createNodeRequest struct {
	Name                string `json:"name" binding:"required"`
	IP                  string `json:"ip" binding:"required"`
	Country             string `json:"country"`
	Datacenter          string `json:"datacenter"`
	APIPort             int    `json:"api_port" binding:"required"`
	AgentPort           int    `json:"agent_port"`
	ConnectMode         string `json:"connect_mode"`
	IsStealth           bool   `json:"is_stealth"`
	IsPersistentStealth bool   `json:"is_persistent_stealth"`
}

func (h *Handler) CreateNode(c *gin.Context) {
	var req createNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Validate IP
	if !isNodeIP(req.IP) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid IP address"})
		return
	}
	// Validate ports
	if req.APIPort < 1 || req.APIPort > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "api_port must be between 1 and 65535"})
		return
	}
	if req.AgentPort != 0 && (req.AgentPort < 1 || req.AgentPort > 65535) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "agent_port must be between 1 and 65535"})
		return
	}

	// Single binary: the node is always the local machine.
	node, err := h.nodeUsecase.CreateNode(
		c.Request.Context(),
		req.Name,
		req.IP,
		req.Country,
		req.Datacenter,
		req.APIPort,
		req.AgentPort,
		"local",
		req.IsStealth,
		req.IsPersistentStealth,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": node})
}

type updateNodeRequest struct {
	Name                  *string                       `json:"name"`
	IP                    *string                       `json:"ip"`
	APIPort               *int                          `json:"api_port"`
	AgentPort             *int                          `json:"agent_port"`
	Country               *string                       `json:"country_code"`
	IsActive              *bool                         `json:"is_active"`
	LogLevel              *string                       `json:"log_level"`
	Datacenter            *string                       `json:"datacenter"`
	IsStealth             *bool                         `json:"is_stealth"`
	IsPersistentStealth   *bool                         `json:"is_persistent_stealth"`
	BandwidthSettings     *domain.BandwidthSettings     `json:"bandwidth_settings"`
	StarlinkSettings      *domain.StarlinkSettings      `json:"starlink_settings"`
	CrashRecoverySettings *domain.CrashRecoverySettings `json:"crash_recovery_settings"`
	EnableAccessLog       *bool                         `json:"enable_access_log"`
}

func (h *Handler) UpdateNode(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	node, err := h.nodeUsecase.GetNode(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}

	var req updateNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Selectively apply non-nil fields
	if req.Name != nil {
		node.Name = *req.Name
	}
	if req.IP != nil {
		if !isNodeIP(*req.IP) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid IP address"})
			return
		}
		node.IP = *req.IP
	}
	if req.APIPort != nil {
		if *req.APIPort < 1 || *req.APIPort > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "api_port must be between 1 and 65535"})
			return
		}
		node.APIPort = *req.APIPort
	}
	if req.AgentPort != nil {
		if *req.AgentPort < 1 || *req.AgentPort > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "agent_port must be between 1 and 65535"})
			return
		}
		node.AgentPort = *req.AgentPort
	}
	if req.Country != nil {
		node.CountryCode = *req.Country
	}
	if req.IsActive != nil {
		node.IsActive = *req.IsActive
	}
	if req.LogLevel != nil {
		node.LogLevel = *req.LogLevel
	}
	if req.Datacenter != nil {
		node.Datacenter = *req.Datacenter
	}
	stealthChanged := false
	if req.IsStealth != nil {
		if node.IsStealth != *req.IsStealth {
			stealthChanged = true
		}
		node.IsStealth = *req.IsStealth
	}
	if req.IsPersistentStealth != nil {
		node.IsPersistentStealth = *req.IsPersistentStealth
	}
	if req.BandwidthSettings != nil {
		node.BandwidthSettings = req.BandwidthSettings
	}
	if req.StarlinkSettings != nil {
		node.StarlinkSettings = req.StarlinkSettings
	}
	if req.CrashRecoverySettings != nil {
		node.CrashRecoverySettings = req.CrashRecoverySettings
	}
	if req.EnableAccessLog != nil {
		node.EnableAccessLog = *req.EnableAccessLog
	}

	if err := h.nodeUsecase.UpdateNode(c.Request.Context(), node); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	if stealthChanged {
		go func() {
			if err := h.nodeUsecase.PushConfigViaAgent(context.Background(), node.ID); err != nil {
				logrus.WithError(err).WithField("node_id", node.ID).Warn("Failed to re-push config after stealth flag change")
			}
		}()
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": node})
}

func (h *Handler) DeleteNode(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	forceStr := c.DefaultQuery("force", "false")
	force, _ := strconv.ParseBool(forceStr)

	if err := h.nodeUsecase.DeleteNode(c.Request.Context(), uint(id), force); err != nil {
		if err == usecase.ErrNodeHasChildren {
			c.JSON(http.StatusConflict, gin.H{"success": false, "error": err.Error(), "code": "NODE_HAS_CHILDREN"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "node deleted"})
}

type migrateNodeRequest struct {
	TargetNodeID    uint `json:"target_node_id" binding:"required"`
	TargetInboundID uint `json:"target_inbound_id" binding:"required"`
}

type migrateInboundRequest struct {
	TargetInboundID uint `json:"target_inbound_id" binding:"required"`
}

func (h *Handler) MigrateNode(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req migrateNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.nodeUsecase.MigrateNodeAccounts(c.Request.Context(), uint(id), req.TargetNodeID, req.TargetInboundID); err != nil {
		if err == usecase.ErrInvalidTargetNode || err == usecase.ErrInboundNotFound {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "node accounts migrated successfully"})
}

func (h *Handler) MigrateInbound(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid inbound id"})
		return
	}

	var req migrateInboundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	result, err := h.nodeUsecase.MigrateInbound(c.Request.Context(), uint(id), req.TargetInboundID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *Handler) CheckHealth(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	status, err := h.nodeUsecase.CheckNodeHealth(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"healthy": status.Healthy,
			"message": status.Message,
			"latency": status.Latency,
		},
	})
}

func (h *Handler) GetNodeStats(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	stats, err := h.nodeUsecase.GetNodeStats(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stats})
}

// GetNodesStatsBulk: GET /nodes/stats/bulk[?ids=1,2,3] (empty = all).
// Response: { success, data: { "<nodeID>": { stats?, error? } } }.
func (h *Handler) GetNodesStatsBulk(c *gin.Context) {
	var ids []uint
	if raw := strings.TrimSpace(c.Query("ids")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": fmt.Sprintf("invalid id %q", part)})
				return
			}
			ids = append(ids, uint(id))
		}
	}

	results, err := h.nodeUsecase.GetNodesStatsBulk(c.Request.Context(), ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	// JSON map keys must be strings.
	out := make(map[string]*usecase.NodeStatsResult, len(results))
	for id, r := range results {
		out[strconv.FormatUint(uint64(id), 10)] = r
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// GetNodesStatsHistoryBulk: GET /nodes/stats/history/bulk?ids=1,2,3&limit=15
// Powers the Nodes-page sparklines in one round-trip.
// Response: { success, data: { "<nodeID>": [NodeStat...] } }.
func (h *Handler) GetNodesStatsHistoryBulk(c *gin.Context) {
	raw := strings.TrimSpace(c.Query("ids"))
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "ids query param is required"})
		return
	}
	var ids []uint
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": fmt.Sprintf("invalid id %q", part)})
			return
		}
		ids = append(ids, uint(id))
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "ids must not be empty"})
		return
	}

	limit := 60
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	// Bound per-node limit to prevent a pathological caller pulling
	// thousands of rows per node.
	const maxLimit = 500
	if limit > maxLimit {
		limit = maxLimit
	}

	results, err := h.nodeUsecase.GetNodesStatsHistoryBulk(c.Request.Context(), ids, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Ensure every requested id is present in the response (empty
	// slice when the node has no history yet) so clients don't have
	// to distinguish "missing key" from "no data".
	out := make(map[string][]*domain.NodeStat, len(ids))
	for _, id := range ids {
		out[strconv.FormatUint(uint64(id), 10)] = results[id]
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// bulkActionRequest is the shared body for /nodes/bulk/* endpoints.
// ids is required and non-empty; an empty list is rejected to avoid
// accidentally restarting the whole fleet.
type bulkActionRequest struct {
	IDs []uint `json:"ids" binding:"required"`
}

// bulkActionResponse formats a per-node result map with string keys (JSON).
func bulkActionResponse(results map[uint]*usecase.NodeBulkActionResult) gin.H {
	out := make(map[string]*usecase.NodeBulkActionResult, len(results))
	var successCount int
	for id, r := range results {
		out[strconv.FormatUint(uint64(id), 10)] = r
		if r.Success {
			successCount++
		}
	}
	return gin.H{
		"success": true,
		"data": gin.H{
			"total":     len(results),
			"succeeded": successCount,
			"results":   out,
		},
	}
}

// parseBulkActionIDs validates the request body and returns the id slice.
func parseBulkActionIDs(c *gin.Context) ([]uint, bool) {
	var req bulkActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid request body: " + err.Error()})
		return nil, false
	}
	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "ids must not be empty"})
		return nil, false
	}
	// Cap to avoid abusive requests that could tie up the RPC pool.
	const maxBulkIDs = 500
	if len(req.IDs) > maxBulkIDs {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": fmt.Sprintf("too many ids (max %d)", maxBulkIDs)})
		return nil, false
	}
	return req.IDs, true
}

// BulkRestartXray restarts Xray on every node in the request body.
// POST /nodes/bulk/restart  { "ids": [1,2,3] }
func (h *Handler) BulkRestartXray(c *gin.Context) {
	ids, ok := parseBulkActionIDs(c)
	if !ok {
		return
	}
	results := h.nodeUsecase.BulkRestartXray(c.Request.Context(), ids)
	c.JSON(http.StatusOK, bulkActionResponse(results))
}

// BulkPushConfig pushes the full Xray config to every listed node.
// POST /nodes/bulk/push-config  { "ids": [1,2,3] }
func (h *Handler) BulkPushConfig(c *gin.Context) {
	ids, ok := parseBulkActionIDs(c)
	if !ok {
		return
	}
	results := h.nodeUsecase.BulkPushConfig(c.Request.Context(), ids)
	c.JSON(http.StatusOK, bulkActionResponse(results))
}

// BulkCheckHealth probes every listed node.
// POST /nodes/bulk/health-check  { "ids": [1,2,3] }
func (h *Handler) BulkCheckHealth(c *gin.Context) {
	ids, ok := parseBulkActionIDs(c)
	if !ok {
		return
	}
	results := h.nodeUsecase.BulkCheckHealth(c.Request.Context(), ids)
	c.JSON(http.StatusOK, bulkActionResponse(results))
}

type bulkUpdateXrayVersionRequest struct {
	IDs     []uint `json:"ids" binding:"required"`
	Version string `json:"version" binding:"required"`
}

// BulkUpdateXrayVersion updates xray-core to a target version on every listed node.
// POST /nodes/bulk/xray-version  { "ids": [1,2,3], "version": "26.2.6" }
func (h *Handler) BulkUpdateXrayVersion(c *gin.Context) {
	var req bulkUpdateXrayVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid request body: " + err.Error()})
		return
	}
	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "ids must not be empty"})
		return
	}
	const maxBulkIDs = 500
	if len(req.IDs) > maxBulkIDs {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": fmt.Sprintf("too many ids (max %d)", maxBulkIDs)})
		return
	}
	if !isValidXrayVersion(req.Version) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid version format"})
		return
	}

	results := h.nodeUsecase.BulkUpdateXrayVersion(c.Request.Context(), req.IDs, req.Version)
	c.JSON(http.StatusOK, bulkActionResponse(results))
}

func (h *Handler) GetNodeHostInfo(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	info, err := h.nodeUsecase.GetNodeHostInfo(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": info})
}

func (h *Handler) GetNodeRealtimeUsers(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	users, err := h.nodeUsecase.GetRealtimeUsers(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": users})
}

// ==================== Inbound Operations ====================

func (h *Handler) ListInbounds(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	inbounds, err := h.nodeUsecase.ListInbounds(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": inbounds})
}

func (h *Handler) GetInbound(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	inbound, err := h.nodeUsecase.GetInbound(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": inbound})
}

func (h *Handler) AddInbound(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var inbound domain.Inbound
	if err := c.ShouldBindJSON(&inbound); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	inbound.NodeID = uint(nodeID)

	if err := h.nodeUsecase.AddInbound(c.Request.Context(), &inbound); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": inbound})
}

func (h *Handler) UpdateInbound(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	inbound, err := h.nodeUsecase.GetInbound(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Clear the JSONB settings groups before binding: the panel sends complete
	// objects, so merging into the loaded row would resurrect removed map entries
	// and disabled sub-objects. Omitted scalar columns still survive.
	inbound.TLSSettings = nil
	inbound.RealitySettings = nil
	inbound.TransportSettings = nil
	inbound.SniffingSettings = nil
	inbound.ShadowsocksSettings = nil
	inbound.WireGuardSettings = nil
	inbound.HTTPSettings = nil
	inbound.SOCKSSettings = nil
	inbound.SockoptSettings = nil
	inbound.VLESSSettings = nil
	inbound.VMessSettings = nil
	inbound.TrojanSettings = nil
	inbound.DokodemoSettings = nil
	inbound.HysteriaSettings = nil
	inbound.FinalMask = nil

	if err := c.ShouldBindJSON(inbound); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.nodeUsecase.UpdateInbound(c.Request.Context(), inbound); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": inbound})
}

func (h *Handler) DeleteInbound(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.nodeUsecase.DeleteInbound(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "inbound deleted"})
}

func (h *Handler) ToggleInboundDisabled(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	inbound, err := h.nodeUsecase.ToggleInboundDisabled(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": inbound})
}

func (h *Handler) GetInboundStats(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	stats, err := h.nodeUsecase.GetInboundStats(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stats})
}

func (h *Handler) DiscoverInbounds(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	inbounds, err := h.nodeUsecase.DiscoverInbounds(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": inbounds})
}

func (h *Handler) SyncInbounds(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	result, err := h.nodeUsecase.SyncInbounds(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// ==================== Outbound Operations ====================

func (h *Handler) ListOutbounds(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	outbounds, err := h.nodeUsecase.ListOutbounds(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": outbounds})
}

func (h *Handler) GetOutbound(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	outbound, err := h.nodeUsecase.GetOutbound(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": outbound})
}

func (h *Handler) AddOutbound(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var outbound domain.Outbound
	if err := c.ShouldBindJSON(&outbound); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	outbound.NodeID = uint(nodeID)

	if err := h.nodeUsecase.AddOutbound(c.Request.Context(), &outbound); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": outbound})
}

func (h *Handler) UpdateOutbound(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	outbound, err := h.nodeUsecase.GetOutbound(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Clear the JSONB settings groups before binding: the panel sends complete
	// objects, so merging into the loaded row would resurrect removed map entries
	// and disabled sub-objects. Omitted scalar columns still survive.
	outbound.TLSSettings = nil
	outbound.RealitySettings = nil
	outbound.TransportSettings = nil
	outbound.SockoptSettings = nil
	outbound.FreedomSettings = nil
	outbound.BlackholeSettings = nil
	outbound.ShadowsocksSettings = nil
	outbound.WireGuardSettings = nil
	outbound.HTTPSettings = nil
	outbound.SOCKSSettings = nil
	outbound.VMessSettings = nil
	outbound.VLESSSettings = nil
	outbound.TrojanSettings = nil
	outbound.DNSOutboundSettings = nil
	outbound.LoopbackSettings = nil
	outbound.HysteriaSettings = nil
	outbound.MuxSettings = nil
	outbound.ProxySettings = nil
	outbound.FinalMask = nil

	if err := c.ShouldBindJSON(outbound); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.nodeUsecase.UpdateOutbound(c.Request.Context(), outbound); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": outbound})
}

func (h *Handler) DeleteOutbound(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.nodeUsecase.DeleteOutbound(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "outbound deleted"})
}

func (h *Handler) ToggleOutboundDisabled(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	outbound, err := h.nodeUsecase.ToggleOutboundDisabled(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": outbound})
}

func (h *Handler) DiscoverOutbounds(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	outbounds, err := h.nodeUsecase.DiscoverOutbounds(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": outbounds})
}

func (h *Handler) SyncOutbounds(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	result, err := h.nodeUsecase.SyncOutbounds(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// ==================== Outbound Testing ====================

func (h *Handler) TestOutbound(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var body struct {
		TestURL string `json:"test_url"`
	}
	_ = c.ShouldBindJSON(&body) // optional body

	result, err := h.nodeUsecase.TestOutbound(c.Request.Context(), uint(id), body.TestURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// ==================== Routing Rules Operations ====================

func (h *Handler) ListRoutingRules(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	rules, err := h.nodeUsecase.ListRoutingRules(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rules})
}

func (h *Handler) GetRoutingRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	rule, err := h.nodeUsecase.GetRoutingRule(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rule})
}

func (h *Handler) AddRoutingRule(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var rule domain.RoutingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Pin server-side fields — body cannot set ID/NodeID.
	rule.ID = 0
	rule.NodeID = uint(nodeID)

	ctx := c.Request.Context()
	if c.Query("skip_push") == "true" {
		ctx = usecase.ContextWithSkipPush(ctx)
	}

	if err := h.nodeUsecase.AddRoutingRule(ctx, &rule); err != nil {
		c.JSON(statusForRoutingError(err), gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": rule})
}

func (h *Handler) UpdateRoutingRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	existing, err := h.nodeUsecase.GetRoutingRule(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Bind body OVER the loaded record, then re-pin ID from URL. The
	// usecase re-pins NodeID from the canonical DB record before saving,
	// so a crafted body cannot relocate the rule across nodes.
	if err := c.ShouldBindJSON(existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	existing.ID = uint(id)

	ctx := c.Request.Context()
	if c.Query("skip_push") == "true" {
		ctx = usecase.ContextWithSkipPush(ctx)
	}

	if err := h.nodeUsecase.UpdateRoutingRule(ctx, existing); err != nil {
		c.JSON(statusForRoutingError(err), gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": existing})
}

func (h *Handler) DeleteRoutingRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	ctx := c.Request.Context()
	if c.Query("skip_push") == "true" {
		ctx = usecase.ContextWithSkipPush(ctx)
	}

	if err := h.nodeUsecase.DeleteRoutingRule(ctx, uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "routing rule deleted"})
}

func (h *Handler) SyncRoutingRules(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	result, err := h.nodeUsecase.SyncRoutingRules(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

type moveRoutingRuleRequest struct {
	MoveUp bool `json:"move_up"`
}

func (h *Handler) MoveRoutingRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req moveRoutingRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.nodeUsecase.MoveRoutingRule(c.Request.Context(), uint(id), req.MoveUp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "routing rule moved"})
}

func (h *Handler) ToggleRoutingRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	rule, err := h.nodeUsecase.ToggleRoutingRule(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rule})
}

type reorderRoutingRulesRequest struct {
	RuleIDs []uint `json:"rule_ids" binding:"required"`
}

func (h *Handler) ReorderRoutingRules(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	var req reorderRoutingRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if len(req.RuleIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "rule_ids cannot be empty"})
		return
	}

	if err := h.nodeUsecase.ReorderRoutingRules(c.Request.Context(), uint(nodeID), req.RuleIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "routing rules reordered"})
}

// ==================== Routing Settings ====================

func (h *Handler) GetRoutingSettings(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	node, err := h.nodeUsecase.GetNode(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": node.GetRoutingSettingsOrDefault()})
}

func (h *Handler) UpdateRoutingSettings(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	node, err := h.nodeUsecase.GetNode(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}

	var settings domain.RoutingSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	node.RoutingSettings = &settings
	if err := h.nodeUsecase.UpdateNode(c.Request.Context(), node); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	// No push here — FE syncs routing rules via separate calls, then
	// fires POST /push-config. Pushing now would race those writes.

	c.JSON(http.StatusOK, gin.H{"success": true, "data": node.RoutingSettings})
}

func (h *Handler) GetDNSSettings(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	node, err := h.nodeUsecase.GetNode(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": node.GetDNSSettingsOrDefault()})
}

func (h *Handler) UpdateDNSSettings(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var settings domain.DNSSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.nodeUsecase.UpdateNodeDNSSettings(c.Request.Context(), uint(id), &settings); err != nil {
		c.JSON(statusForDNSError(err), gin.H{"success": false, "error": err.Error()})
		return
	}

	// Re-fetch to return the persisted state
	node, err := h.nodeUsecase.GetNode(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": node.GetDNSSettingsOrDefault()})
}

func (h *Handler) DeleteDNSSettings(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.nodeUsecase.ClearNodeDNSSettings(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "DNS settings cleared"})
}

func (h *Handler) GetFakeDNSSettings(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	node, err := h.nodeUsecase.GetNode(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": node.GetFakeDNSSettingsOrDefault()})
}

func (h *Handler) UpdateFakeDNSSettings(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var pools []domain.FakeDNSPool
	if err := c.ShouldBindJSON(&pools); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if err := h.nodeUsecase.UpdateNodeFakeDNSSettings(c.Request.Context(), uint(id), pools); err != nil {
		c.JSON(statusForDNSError(err), gin.H{"success": false, "error": err.Error()})
		return
	}
	node, err := h.nodeUsecase.GetNode(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": node.GetFakeDNSSettingsOrDefault()})
}

func (h *Handler) DeleteFakeDNSSettings(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	if err := h.nodeUsecase.UpdateNodeFakeDNSSettings(c.Request.Context(), uint(id), nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "FakeDNS settings cleared"})
}

// ==================== Agent Management ====================

func (h *Handler) UpdateAgent(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	// Automated update using local binaries
	if err := h.nodeUsecase.AutoUpdateAgent(c.Request.Context(), uint(nodeID), nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Agent update initiated successfully"})
}

func (h *Handler) UpdateAgentStream(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	// Set headers for SSE
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	progressCh := make(chan usecase.UpdateProgress, 10)

	// Start update in background
	go func() {
		defer close(progressCh)
		if err := h.nodeUsecase.AutoUpdateAgent(c.Request.Context(), uint(nodeID), progressCh); err != nil {
			// Error is already sent via channel by u.AutoUpdateAgent usually,
			// but if it returns error logic that didn't send validation, we catch here.
			progressCh <- usecase.UpdateProgress{
				Step:    "complete",
				Status:  "error",
				Error:   err.Error(),
				Message: "Update process failed",
			}
		}
	}()

	// Stream events with a 30s comment heartbeat so intermediaries don't
	// close the connection during long-running steps (e.g. binary upload).
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case p, ok := <-progressCh:
			if !ok {
				return
			}
			jsonData, err := json.Marshal(p)
			if err != nil {
				continue
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", jsonData)
			c.Writer.Flush()
		case <-heartbeat.C:
			fmt.Fprint(c.Writer, ": heartbeat\n\n")
			c.Writer.Flush()
		case <-ctx.Done():
			return
		}
	}
}

func (h *Handler) StartXray(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	if err := h.nodeUsecase.StartXray(c.Request.Context(), uint(nodeID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Xray start command sent"})
}

func (h *Handler) StopXray(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	if err := h.nodeUsecase.StopXray(c.Request.Context(), uint(nodeID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Xray stop command sent"})
}

func (h *Handler) RestartXray(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	if err := h.nodeUsecase.RestartXray(c.Request.Context(), uint(nodeID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Xray restart command sent"})
}

func (h *Handler) PushConfig(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	if err := h.nodeUsecase.PushFullConfig(c.Request.Context(), uint(nodeID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Config pushed successfully"})
}

func (h *Handler) GetNodeStatsHistory(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	limitStr := c.DefaultQuery("limit", "60")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 60
	}

	stats, err := h.nodeUsecase.GetNodeStatsHistory(c.Request.Context(), uint(id), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stats})
}

func (h *Handler) GetNodeDailyTraffic(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	daysStr := c.DefaultQuery("days", "30")
	days, _ := strconv.Atoi(daysStr)
	if days <= 0 {
		days = 30
	}

	data, err := h.nodeUsecase.GetNodeDailyTraffic(c.Request.Context(), uint(id), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func (h *Handler) GetNodeUptimeEvents(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	hoursStr := c.DefaultQuery("hours", "168")
	hours, _ := strconv.Atoi(hoursStr)
	if hours <= 0 {
		hours = 168
	}

	data, err := h.nodeUsecase.GetNodeUptimeEvents(c.Request.Context(), uint(id), hours)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// ==================== Xray Config Operations ====================

func (h *Handler) GetXrayConfig(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	config, err := h.nodeUsecase.GetNodeXrayConfig(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": config})
}

type updateXrayConfigRequest struct {
	Content string `json:"content" binding:"required"`
}

func (h *Handler) UpdateXrayConfig(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	var req updateXrayConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.nodeUsecase.UpdateNodeXrayConfig(c.Request.Context(), uint(nodeID), req.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Configuration updated"})
}

// GetXrayConfigDiff returns the running vs would-push diff for a node.
// GET /nodes/:id/xray-config/diff
// Response: { success, data: { running, generated, running_hash, generated_hash, differs, running_error? } }
func (h *Handler) GetXrayConfigDiff(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	diff, err := h.nodeUsecase.GetNodeXrayConfigDiff(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": diff})
}

func (h *Handler) ValidateXrayConfig(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	var req updateXrayConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	valid, errors, warnings, err := h.nodeUsecase.ValidateNodeXrayConfig(c.Request.Context(), uint(nodeID), req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"valid":    valid,
			"errors":   errors,
			"warnings": warnings,
		},
	})
}

func (h *Handler) TailLogs(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	linesStr := c.DefaultQuery("lines", "50")
	lines, _ := strconv.Atoi(linesStr)
	if lines <= 0 {
		lines = 50
	}
	if lines > 500 {
		lines = 500
	}

	entries, err := h.nodeUsecase.GetNodeRecentLogs(c.Request.Context(), uint(nodeID), lines)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": entries})
}

func (h *Handler) StreamLogs(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	tailStr := c.DefaultQuery("tail", "100")
	tail, _ := strconv.Atoi(tailStr)

	followStr := c.DefaultQuery("follow", "true")
	follow := followStr == "true"

	// Set headers for SSE
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	// Create a context that is cancelled when the client disconnects
	ctx := c.Request.Context()

	logs, errs, err := h.nodeUsecase.StreamNodeLogs(ctx, uint(nodeID), tail, follow)
	if err != nil {
		// If we haven't written headers yet, return JSON error
		// But headers might be written by gin if we started streaming.
		// Safe to write a structured SSE error event
		c.SSEvent("error", err.Error())
		c.Writer.Flush()
		return
	}

	// 30s comment heartbeat to keep idle SSE alive through proxies
	// (':' lines discarded by EventSource, no message event fired).
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	c.Stream(func(w io.Writer) bool {
		select {
		case entry, ok := <-logs:
			if !ok {
				return false
			}
			jsonData, err := json.Marshal(entry)
			if err != nil {
				return true
			}
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", jsonData)
			c.Writer.Flush()
			return true
		case err, ok := <-errs:
			if !ok {
				return false
			}
			c.SSEvent("error", err.Error())
			c.Writer.Flush()
			return false
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			c.Writer.Flush()
			return true
		case <-ctx.Done():
			return false
		}
	})
}

// ==================== Tools ====================

func (h *Handler) GenerateVLESSKeys(c *gin.Context) {
	keys, err := h.nodeUsecase.GenerateVLESSKeys(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": keys})
}

func (h *Handler) GenerateX25519Keys(c *gin.Context) {
	keys, err := xray.GenerateX25519Keys()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"privateKey": keys.PrivateKey,
		"publicKey":  keys.PublicKey,
	}})
}

// ==================== Xray Version Management ====================

// isValidXrayVersion: BinaryManager validator so REST, cache, and agent
// accept the same set ("26.A" / "26.2.6rc1" rejected here, not deeper).
func isValidXrayVersion(v string) bool {
	return xrayUsecase.IsValidVersion(v)
}

type updateXrayVersionRequest struct {
	Version string `json:"version" binding:"required"`
}

func (h *Handler) UpdateXrayVersion(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	var req updateXrayVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Validate version format
	if !isValidXrayVersion(req.Version) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid version format"})
		return
	}

	if err := h.nodeUsecase.UpdateXrayVersion(c.Request.Context(), uint(nodeID), req.Version); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Xray version updated successfully"})
}

// ==================== Geofile Management ====================

type updateGeoFilesRequest struct {
	Region           string `json:"region" binding:"required"`
	CustomGeoIPURL   string `json:"custom_geoip_url"`
	CustomGeoSiteURL string `json:"custom_geosite_url"`
}

func (h *Handler) UpdateGeoFiles(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	var req updateGeoFilesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Validate region
	validRegions := map[string]bool{"iran": true, "china": true, "russia": true, "custom": true}
	if !validRegions[req.Region] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid region, must be one of: iran, china, russia, custom"})
		return
	}

	// Custom region requires both URLs
	if req.Region == "custom" {
		if req.CustomGeoIPURL == "" || req.CustomGeoSiteURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "custom region requires both custom_geoip_url and custom_geosite_url"})
			return
		}
	}

	if err := h.nodeUsecase.UpdateGeoFiles(c.Request.Context(), uint(nodeID), req.Region, req.CustomGeoIPURL, req.CustomGeoSiteURL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Geofiles updated successfully"})
}

// xrayRelease represents a single GitHub release
type xrayRelease struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
}

// In-memory cache for GitHub releases
var (
	xrayReleaseCacheMu   sync.Mutex
	xrayReleaseCacheData []xrayRelease
	xrayReleaseCacheTime time.Time
)

func (h *Handler) GetXrayReleases(c *gin.Context) {
	xrayReleaseCacheMu.Lock()
	if time.Since(xrayReleaseCacheTime) < time.Hour && xrayReleaseCacheData != nil {
		cached := xrayReleaseCacheData
		xrayReleaseCacheMu.Unlock()
		c.JSON(http.StatusOK, gin.H{"success": true, "data": cached})
		return
	}
	xrayReleaseCacheMu.Unlock()

	// Fetch from GitHub
	var client *http.Client
	if h.httpFactory != nil {
		client = h.httpFactory.ClientFor(httpclient.FeatureGitHubAPI, httpclient.EgressForeign, 15*time.Second)
	} else {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Get("https://api.github.com/repos/XTLS/Xray-core/releases?per_page=50")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "failed to fetch releases from GitHub"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": fmt.Sprintf("GitHub API returned status %d", resp.StatusCode)})
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "failed to read GitHub response"})
		return
	}

	var ghReleases []struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		PublishedAt string `json:"published_at"`
		Prerelease  bool   `json:"prerelease"`
	}

	if err := json.Unmarshal(body, &ghReleases); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "failed to parse GitHub response"})
		return
	}

	// Filter out pre-releases, strip v prefix, limit to 30
	var releases []xrayRelease
	for _, r := range ghReleases {
		if r.Prerelease {
			continue
		}
		version := strings.TrimPrefix(r.TagName, "v")
		releases = append(releases, xrayRelease{
			Version:     version,
			Tag:         r.TagName,
			Name:        r.Name,
			PublishedAt: r.PublishedAt,
		})
		if len(releases) >= 30 {
			break
		}
	}

	// Update cache
	xrayReleaseCacheMu.Lock()
	xrayReleaseCacheData = releases
	xrayReleaseCacheTime = time.Now()
	xrayReleaseCacheMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"success": true, "data": releases})
}

// ==================== Host Operations ====================

func (h *Handler) ListHosts(c *gin.Context) {
	inboundID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	hosts, err := h.nodeUsecase.ListHosts(c.Request.Context(), uint(inboundID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": hosts})
}

func (h *Handler) AddHost(c *gin.Context) {
	inboundID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var host domain.Host
	if err := c.ShouldBindJSON(&host); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	ibID := uint(inboundID)
	host.ID = 0
	host.InboundID = &ibID
	host.PlanID = nil // This endpoint is for inbound-scoped hosts only

	if err := h.nodeUsecase.AddHost(c.Request.Context(), &host); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": host})
}

func (h *Handler) GetHost(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	host, err := h.nodeUsecase.GetHost(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": host})
}

func (h *Handler) UpdateHost(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	host, err := h.nodeUsecase.GetHost(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Preserve immutable fields before binding
	origID := host.ID
	origInboundID := host.InboundID
	origPlanID := host.PlanID
	origCreatedAt := host.CreatedAt

	if err := c.ShouldBindJSON(host); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Restore immutable fields to prevent injection
	host.ID = origID
	host.InboundID = origInboundID
	host.PlanID = origPlanID
	host.CreatedAt = origCreatedAt

	if err := h.nodeUsecase.UpdateHost(c.Request.Context(), host); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": host})
}

func (h *Handler) DeleteHost(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.nodeUsecase.DeleteHost(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "host deleted"})
}

func (h *Handler) ListAllHosts(c *gin.Context) {
	search := c.Query("search")
	var nodeID, inboundID, planID uint
	if v := c.Query("node_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			nodeID = uint(n)
		}
	}
	if v := c.Query("inbound_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			inboundID = uint(n)
		}
	}
	if v := c.Query("plan_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			planID = uint(n)
		}
	}
	var isDisabled *bool
	if v := c.Query("disabled"); v != "" {
		b := v == "true"
		isDisabled = &b
	}
	offset := 0
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	hostType := c.Query("host_type") // "server", "info", or "" (all)
	tag := c.Query("tag")
	sortBy := c.DefaultQuery("sort_by", "priority")
	sortOrder := c.DefaultQuery("sort_order", "asc")
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "asc"
	}

	hosts, total, err := h.nodeUsecase.ListAllHosts(c.Request.Context(), search, nodeID, inboundID, planID, isDisabled, hostType, tag, sortBy, sortOrder, offset, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    hosts,
		"meta":    gin.H{"total": total, "offset": offset, "limit": limit},
	})
}

func (h *Handler) CreateHost(c *gin.Context) {
	var host domain.Host
	if err := c.ShouldBindJSON(&host); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Prevent user-specified ID
	host.ID = 0

	// XOR validation is handled in usecase; just pass through
	if err := h.nodeUsecase.AddHost(c.Request.Context(), &host); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": host})
}

func (h *Handler) BulkCreateInfoHosts(c *gin.Context) {
	var req struct {
		Host    domain.Host `json:"host"`
		PlanIDs []uint      `json:"plan_ids" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Ensure host is info-only (no inbound_id)
	req.Host.ID = 0
	req.Host.InboundID = nil

	hosts, err := h.nodeUsecase.BulkCreateInfoHosts(c.Request.Context(), &req.Host, req.PlanIDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": hosts})
}

func (h *Handler) DuplicateHost(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	clone, err := h.nodeUsecase.DuplicateHost(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": clone})
}

// === Bulk Host Operations ===

func (h *Handler) BulkUpdateHosts(c *gin.Context) {
	var req struct {
		IDs    []uint         `json:"ids" binding:"required,min=1"`
		Fields map[string]any `json:"fields" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	affected, err := h.nodeUsecase.BulkUpdateHosts(c.Request.Context(), req.IDs, req.Fields)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"updated": affected}})
}

func (h *Handler) ListHostTags(c *gin.Context) {
	tags, err := h.nodeUsecase.ListHostTags(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": tags})
}

// === Host Template Operations ===

func (h *Handler) ListHostTemplates(c *gin.Context) {
	templates, err := h.nodeUsecase.ListHostTemplates(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": templates})
}

func (h *Handler) CreateHostTemplate(c *gin.Context) {
	var template domain.HostTemplate
	if err := c.ShouldBindJSON(&template); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	template.ID = 0
	if err := h.nodeUsecase.CreateHostTemplate(c.Request.Context(), &template); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": template})
}

func (h *Handler) GetHostTemplate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	template, err := h.nodeUsecase.GetHostTemplate(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": template})
}

func (h *Handler) UpdateHostTemplate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var template domain.HostTemplate
	if err := c.ShouldBindJSON(&template); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	template.ID = uint(id)
	if err := h.nodeUsecase.UpdateHostTemplate(c.Request.Context(), &template); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": template})
}

func (h *Handler) DeleteHostTemplate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	if err := h.nodeUsecase.DeleteHostTemplate(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *Handler) ApplyHostTemplate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var req struct {
		HostIDs []uint `json:"host_ids" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	affected, err := h.nodeUsecase.ApplyHostTemplate(c.Request.Context(), uint(id), req.HostIDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"updated": affected}})
}

// ==================== Access Logs ====================

func (h *Handler) GetAccessLogs(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}

	email := c.Query("email")

	limit := int32(100)
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "100")); err == nil && l > 0 {
		limit = int32(l)
	}
	if limit > 1000 {
		limit = 1000
	}

	entries, err := h.nodeUsecase.GetAccessLogs(c.Request.Context(), uint(id), email, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": entries})
}

// GetAggregatedAccessLogs returns access logs from multiple nodes merged and sorted.
func (h *Handler) GetAggregatedAccessLogs(c *gin.Context) {
	email := c.Query("email")

	limit := int32(500)
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "500")); err == nil && l > 0 {
		limit = int32(l)
	}
	if limit > 1000 {
		limit = 1000
	}

	// Parse optional node_ids filter (comma-separated)
	var nodeIDs []uint
	if ids := c.Query("node_ids"); ids != "" {
		for _, s := range strings.Split(ids, ",") {
			if id, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32); err == nil {
				nodeIDs = append(nodeIDs, uint(id))
			}
		}
	}

	entries, err := h.nodeUsecase.GetAggregatedAccessLogs(c.Request.Context(), nodeIDs, email, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": entries})
}

// GetAccessLogAnalytics returns persisted hourly access log summaries from the DB.
func (h *Handler) GetAccessLogAnalytics(c *gin.Context) {
	filter := h.parseAccessLogFilter(c)

	summaries, total, err := h.nodeUsecase.GetAccessLogAnalytics(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": summaries, "total": total})
}

// GetAccessLogTopDomains returns aggregated top domains from access log summaries.
func (h *Handler) GetAccessLogTopDomains(c *gin.Context) {
	filter := h.parseAccessLogFilter(c)

	topN := 50
	if n, err := strconv.Atoi(c.DefaultQuery("top", "50")); err == nil && n > 0 {
		topN = n
	}

	domains, err := h.nodeUsecase.GetAccessLogTopDomains(c.Request.Context(), filter, topN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": domains})
}

func (h *Handler) parseAccessLogFilter(c *gin.Context) repository.AccessLogSummaryFilter {
	var filter repository.AccessLogSummaryFilter

	if ids := c.Query("node_ids"); ids != "" {
		for _, s := range strings.Split(ids, ",") {
			if id, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32); err == nil {
				filter.NodeIDs = append(filter.NodeIDs, uint(id))
			}
		}
	}
	filter.Email = c.Query("email")
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			filter.From = t
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			filter.To = t
		}
	}
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "168")); err == nil && l > 0 {
		filter.Limit = l
	}
	if o, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && o >= 0 {
		filter.Offset = o
	}

	return filter
}

// ==================== Balancing Rules Operations ====================

func (h *Handler) ListBalancingRules(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	rules, err := h.nodeUsecase.ListBalancingRules(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rules})
}

func (h *Handler) AddBalancingRule(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var rule domain.BalancingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	// Pin server-side identity — body cannot set ID/NodeID.
	rule.ID = 0
	rule.NodeID = uint(nodeID)
	if err := h.nodeUsecase.AddBalancingRule(c.Request.Context(), &rule); err != nil {
		c.JSON(statusForRoutingError(err), gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": rule})
}

func (h *Handler) UpdateBalancingRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var rule domain.BalancingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	// Pin ID from URL; usecase re-pins NodeID from DB so body cannot relocate.
	rule.ID = uint(id)
	if err := h.nodeUsecase.UpdateBalancingRule(c.Request.Context(), &rule); err != nil {
		c.JSON(statusForRoutingError(err), gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rule})
}

func (h *Handler) DeleteBalancingRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	if err := h.nodeUsecase.DeleteBalancingRule(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "balancing rule deleted"})
}

// ==================== Reverse Proxy Operations ====================

func (h *Handler) ListReverseProxies(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node ID"})
		return
	}
	rps, err := h.nodeUsecase.ListReverseProxies(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rps})
}

func (h *Handler) GetReverseProxy(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ID"})
		return
	}
	rp, err := h.nodeUsecase.GetReverseProxy(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "reverse proxy not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rp})
}

func (h *Handler) AddReverseProxy(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node ID"})
		return
	}
	var rp domain.ReverseProxy
	if err := c.ShouldBindJSON(&rp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	rp.NodeID = uint(nodeID)
	if err := h.nodeUsecase.AddReverseProxy(c.Request.Context(), &rp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": rp})
}

func (h *Handler) UpdateReverseProxy(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ID"})
		return
	}
	var rp domain.ReverseProxy
	if err := c.ShouldBindJSON(&rp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	rp.ID = uint(id)
	if err := h.nodeUsecase.UpdateReverseProxy(c.Request.Context(), &rp); err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rp})
}

func (h *Handler) DeleteReverseProxy(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ID"})
		return
	}
	if err := h.nodeUsecase.DeleteReverseProxy(c.Request.Context(), uint(id)); err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "reverse proxy deleted"})
}
