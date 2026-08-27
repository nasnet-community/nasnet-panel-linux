package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
)

func (h *Handler) ListVPNProfiles(c *gin.Context) {
	rows, err := h.uc.ListVPNProfiles(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

func (h *Handler) CreateVPNProfile(c *gin.Context) {
	var req usecase.CreateVPNProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	v, err := h.uc.CreateVPNProfile(c.Request.Context(), req)
	if err != nil {
		// Everything the parser refuses is the operator's input, not our fault.
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": v})
}

func (h *Handler) UpdateVPNProfile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var req usecase.CreateVPNProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	v, err := h.uc.UpdateVPNProfile(c.Request.Context(), uint(id), req)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": v})
}

func (h *Handler) DeleteVPNProfile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	err = h.uc.DeleteVPNProfile(c.Request.Context(), uint(id))
	switch {
	case errors.Is(err, domain.ErrProfileActive):
		// Deleting the row under a live tunnel leaves nothing to turn it off.
		fail(c, http.StatusBadRequest, err)
	case errors.Is(err, domain.ErrProfileNotFound):
		fail(c, http.StatusNotFound, err)
	case err != nil:
		fail(c, http.StatusInternalServerError, err)
	default:
		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// ParseVPNInput shows what a pasted URI or config file means before anything is
// stored, so the operator sees what was dropped and what was filled in.
func (h *Handler) ParseVPNInput(c *gin.Context) {
	var req struct {
		Raw string `json:"raw"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	cfg, verdicts, err := h.uc.ParseVPNInput(c.Request.Context(), req.Raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": cfg, "verdicts": verdicts})
}

// GenerateVPNKeypair is for an operator standing up their own server: they need
// the public half to put on it.
func (h *Handler) GenerateVPNKeypair(c *gin.Context) {
	priv, pub, err := h.uc.GenerateVPNKeypair()
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"private_key": priv, "public_key": pub,
	}})
}

// EnableVPNProfile and DisableVPNProfile move packets, so both ride the
// two-phase apply and are settled through the existing confirm endpoint.
func (h *Handler) EnableVPNProfile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	verdicts, view, err := h.uc.EnableVPNProfile(c.Request.Context(), uint(id))
	respondVPNApply(c, verdicts, view, err)
}

func (h *Handler) DisableVPNProfile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	verdicts, view, err := h.uc.DisableVPNProfile(c.Request.Context(), uint(id))
	respondVPNApply(c, verdicts, view, err)
}

// SetVPNProfileRole skips the confirm pipeline: weight and priority only
// redistribute flows among working tunnels.
func (h *Handler) SetVPNProfileRole(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Priority int `json:"priority"`
		Weight   int `json:"weight"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.uc.SetVPNProfileRole(c.Request.Context(), uint(id), req.Priority, req.Weight); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, usecase.ErrValidationFailed) || errors.Is(err, domain.ErrProfileNotFound) {
			code = http.StatusBadRequest
		}
		fail(c, code, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// SetVPNProfileTransport skips the confirm pipeline too: a mis-pin costs one
// tunnel, and the rest of the pool keeps carrying.
func (h *Handler) SetVPNProfileTransport(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var req struct {
		UplinkKey string `json:"uplink_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.uc.SetVPNProfileTransport(c.Request.Context(), uint(id), req.UplinkKey); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, usecase.ErrValidationFailed) || errors.Is(err, domain.ErrProfileNotFound) {
			code = http.StatusBadRequest
		}
		fail(c, code, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Instant like the pin: it only moves traffic between tunnels already enabled.
func (h *Handler) SetPoolStrategy(c *gin.Context) {
	var req struct {
		Strategy string `json:"strategy"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.uc.SetPoolStrategy(c.Request.Context(), req.Strategy); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, usecase.ErrValidationFailed) {
			code = http.StatusBadRequest
		}
		fail(c, code, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// The whole order, first to last. A partial one is not an order.
func (h *Handler) SetPoolOrder(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.uc.SetPoolOrder(c.Request.Context(), req.IDs); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, usecase.ErrValidationFailed) || errors.Is(err, domain.ErrProfileNotFound) {
			code = http.StatusBadRequest
		}
		fail(c, code, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *Handler) VPNStatus(c *gin.Context) {
	st, err := h.uc.VPNStatus(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": st})
}

// respondVPNApply mirrors UpdateLAN: verdicts ride alongside the envelope, not
// inside it, so a rejection still explains itself.
func respondVPNApply(c *gin.Context, verdicts []domain.Verdict, view *usecase.ApplyView, err error) {
	if verdicts == nil {
		verdicts = []domain.Verdict{}
	}
	switch {
	case errors.Is(err, usecase.ErrValidationFailed):
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false, "error": err.Error(), "verdicts": verdicts})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false, "error": err.Error(), "verdicts": verdicts})
	case view == nil && domain.Rejected(verdicts):
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false, "error": "validation failed", "verdicts": verdicts})
	default:
		// A deactivate with nothing active applies nothing and is still a success.
		c.JSON(http.StatusOK, gin.H{"success": true, "data": view, "verdicts": verdicts})
	}
}
