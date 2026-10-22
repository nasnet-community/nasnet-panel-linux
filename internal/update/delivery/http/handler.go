// Package http serves the Settings → Updates API.
package http

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/update/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
)

type Handler struct {
	uc    *usecase.Usecase
	audit auditDomain.AuditLogUsecase
}

func NewHandler(uc *usecase.Usecase, audit auditDomain.AuditLogUsecase) *Handler {
	return &Handler{uc: uc, audit: audit}
}

// RegisterRoutes goes on the admin group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/updates")
	g.GET("", h.Status)
	g.POST("/check", h.Check)
}

// GET /updates
func (h *Handler) Status(c *gin.Context) {
	st, err := h.uc.Status(c.Request.Context())
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.OK(c, st)
}

// POST /updates/check
func (h *Handler) Check(c *gin.Context) {
	res, err := h.uc.CheckNow(c.Request.Context())
	if errors.Is(err, usecase.ErrNoVersion) {
		httputil.Error(c, http.StatusConflict, "This build has no release version to compare.")
		return
	}
	if err != nil {
		httputil.Error(c, http.StatusConflict, err.Error())
		return
	}
	httputil.OK(c, res)
}
