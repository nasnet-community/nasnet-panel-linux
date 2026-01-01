package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	mntUC "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/usecase"
	subUC "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
)

const defaultMaintenanceNotice = "Service maintenance in progress. Purchases and renewals are temporarily paused."

type Handler struct {
	uc    mntUC.Usecase
	subUC subUC.SubscriptionUsecase
}

func NewHandler(uc mntUC.Usecase, subUC subUC.SubscriptionUsecase) *Handler {
	return &Handler{uc: uc, subUC: subUC}
}

type globalReq struct {
	Enabled bool   `json:"enabled"`
	Message string `json:"message"`
	Notify  bool   `json:"notify"`
}

type entityReq struct {
	Enabled bool   `json:"enabled"`
	Message string `json:"message"`
}

type apiResp struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
	Code    string `json:"code,omitempty"`
}

// RegisterAdminRoutes mounts admin-only mutate endpoints on the given group.
// Caller is responsible for wrapping with admin auth middleware.
func (h *Handler) RegisterAdminRoutes(rg *gin.RouterGroup) {
	rg.POST("/global", h.SetGlobal)
	rg.POST("/nodes/:id", h.SetNode)
	rg.POST("/subscriptions/:id", h.SetSubscription)
}

// RegisterPublicRoutes mounts the unauth status endpoint under /sub/:uuid/maintenance.
func (h *Handler) RegisterPublicRoutes(rg *gin.RouterGroup) {
	rg.GET("/sub/:uuid/maintenance", h.GetSubStatus)
}

func (h *Handler) SetGlobal(c *gin.Context) {
	var req globalReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiResp{Error: err.Error()})
		return
	}
	if err := h.uc.SetGlobal(c.Request.Context(), req.Enabled, req.Message, req.Notify); err != nil {
		c.JSON(http.StatusInternalServerError, apiResp{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, apiResp{Success: true, Data: gin.H{"enabled": req.Enabled}})
}

func (h *Handler) SetNode(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, apiResp{Error: "invalid node id"})
		return
	}
	var req entityReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiResp{Error: err.Error()})
		return
	}
	if err := h.uc.SetNode(c.Request.Context(), uint(id), req.Enabled, req.Message); err != nil {
		c.JSON(http.StatusInternalServerError, apiResp{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, apiResp{Success: true})
}

func (h *Handler) SetSubscription(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, apiResp{Error: "invalid subscription id"})
		return
	}
	var req entityReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiResp{Error: err.Error()})
		return
	}
	if err := h.uc.SetSubscription(c.Request.Context(), uint(id), req.Enabled, req.Message); err != nil {
		c.JSON(http.StatusInternalServerError, apiResp{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, apiResp{Success: true})
}

func (h *Handler) GetSubStatus(c *gin.Context) {
	uuid := c.Param("uuid")
	sub, err := h.subUC.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, apiResp{Error: "subscription not found"})
		return
	}
	id := sub.ID
	status := h.uc.Resolve(c.Request.Context(), sub.GetUserID(), &id, defaultMaintenanceNotice)
	c.JSON(http.StatusOK, apiResp{Success: true, Data: status})
}
