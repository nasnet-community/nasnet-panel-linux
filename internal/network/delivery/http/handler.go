package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
)

type Handler struct {
	uc         usecase.NetworkUsecase
	routerMode bool
}

// routerMode is captured at construction (off = 404)
func NewHandler(uc usecase.NetworkUsecase, routerMode bool) *Handler {
	return &Handler{uc: uc, routerMode: routerMode}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	n := rg.Group("/network")
	n.Use(h.requireRouterMode)
	{
		n.GET("/interfaces", h.ListInterfaces)
		n.PUT("/interfaces/:key", h.SetLabel)
		n.POST("/interfaces/:key/identify", h.Identify)
		n.GET("/state", h.State)
		n.GET("/groups", h.Groups)
		n.POST("/plan", h.Plan)
		n.POST("/apply", h.Apply)
		n.POST("/confirm", h.Confirm)
		n.POST("/rollback", h.Rollback)

		n.GET("/lan", h.GetLAN)
		n.PUT("/lan", h.UpdateLAN)
		n.GET("/lan/devices", h.ListDevices)
		n.PUT("/lan/devices/:mac/label", h.SetDeviceLabel)

		n.GET("/wifi/radios", h.ListRadios)
		n.PUT("/wifi/ap", h.EnableAP)
		n.DELETE("/wifi/:key", h.DisableWifi)
		n.POST("/wifi/scan/:key", h.ScanWifi)
		n.POST("/wifi/connect/:key", h.ConnectWifi)
		// Only enable and disable touch packets; role changes redistribute
		// flows, and the rest is plain storage.
		n.GET("/vpn/profiles", h.ListVPNProfiles)
		n.POST("/vpn/profiles", h.CreateVPNProfile)
		n.PUT("/vpn/profiles/:id", h.UpdateVPNProfile)
		n.DELETE("/vpn/profiles/:id", h.DeleteVPNProfile)
		n.POST("/vpn/parse", h.ParseVPNInput)
		n.POST("/vpn/keypair", h.GenerateVPNKeypair)
		n.POST("/vpn/profiles/:id/enable", h.EnableVPNProfile)
		n.POST("/vpn/profiles/:id/disable", h.DisableVPNProfile)
		n.PATCH("/vpn/profiles/:id/role", h.SetVPNProfileRole)
		n.PATCH("/vpn/profiles/:id/transport", h.SetVPNProfileTransport)
		n.PATCH("/vpn/pool/strategy", h.SetPoolStrategy)
		n.PATCH("/vpn/pool/order", h.SetPoolOrder)
		n.GET("/vpn/status", h.VPNStatus)

		// The probe ladder. Assembly only — never dials.
		n.GET("/health", h.Health)
		n.PUT("/uplinks/:ifname/force", h.SetUplinkForce)

		// The flow page. Read-only: nothing here touches a packet.
		n.GET("/flow", h.FlowGraph)
		n.POST("/flow/trace", h.TraceFlow)
		n.GET("/flow/conns", h.FlowConns)
		n.GET("/flow/events", h.FlowEvents)

		n.GET("/port-forwards", h.ListPortForwards)
		n.POST("/port-forwards", h.CreatePortForward)
		n.PUT("/port-forwards/:id", h.UpdatePortForward)
		n.DELETE("/port-forwards/:id", h.DeletePortForward)
	}
}

func (h *Handler) requireRouterMode(c *gin.Context) {
	if !h.routerMode {
		c.AbortWithStatusJSON(http.StatusNotFound,
			gin.H{"success": false, "error": "router mode is not enabled"})
		return
	}
	c.Next()
}

func fail(c *gin.Context, code int, err error) {
	c.JSON(code, gin.H{"success": false, "error": err.Error()})
}

func (h *Handler) ListInterfaces(c *gin.Context) {
	views, err := h.uc.Enumerate(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": views})
}

func (h *Handler) State(c *gin.Context) {
	st, err := h.uc.State(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": st})
}

func (h *Handler) Groups(c *gin.Context) {
	groups, err := h.uc.Groups(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": groups})
}

// SetLabel edits operator text only. Roles change through plan/apply so they
// pass validation and the dead-man.
func (h *Handler) SetLabel(c *gin.Context) {
	var body struct {
		Label string `json:"label"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.uc.SetLabel(c.Request.Context(), c.Param("key"), body.Label); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Identify is a UI affordance (blink the port); stage 1 has nothing to blink.
func (h *Handler) Identify(c *gin.Context) {
	c.JSON(http.StatusNotImplemented,
		gin.H{"success": false, "error": "identify is not available in stage 1"})
}

func (h *Handler) Plan(c *gin.Context) {
	var req domain.ChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	view, err := h.uc.Plan(c.Request.Context(), req)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// Apply validates first and 400s with the verdicts, so the UI names the rule.
func (h *Handler) Apply(c *gin.Context) {
	var req domain.ChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}

	plan, err := h.uc.Plan(c.Request.Context(), req)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	if domain.Rejected(plan.Verdicts) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false, "error": "validation failed", "verdicts": plan.Verdicts,
		})
		return
	}

	view, err := h.uc.Apply(c.Request.Context(), req)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view, "verdicts": plan.Verdicts})
}

// Confirm accepts an empty body: the box may have re-addressed itself mid-apply,
// so the UI polls both addresses and can't always know the plan id.
func (h *Handler) Confirm(c *gin.Context) {
	var body struct {
		PlanID uint `json:"plan_id"`
	}
	_ = c.ShouldBindJSON(&body) // empty body is valid

	if err := h.uc.Confirm(c.Request.Context(), body.PlanID); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *Handler) Rollback(c *gin.Context) {
	if err := h.uc.Rollback(c.Request.Context()); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
