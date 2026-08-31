package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// wifiAPRequest is the enable-AP body. The PSK binds here and only here: the
// model's json:"-" keeps it out of responses, which also stops it binding in.
// An empty PSK on an edit keeps the stored one.
type wifiAPRequest struct {
	InterfaceID uint   `json:"interface_id" binding:"required"`
	SSID        string `json:"ssid" binding:"required"`
	PSK         string `json:"psk"`
	CountryCode string `json:"country_code" binding:"required"`
	Band        string `json:"band" binding:"required"`
	Channel     int    `json:"channel"`
	Hidden      bool   `json:"hidden"`
}

type wifiConnectRequest struct {
	SSID string `json:"ssid" binding:"required"`
	PSK  string `json:"psk"`
}

func (h *Handler) ListRadios(c *gin.Context) {
	radios, err := h.uc.Radios(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": radios})
}

func (h *Handler) EnableAP(c *gin.Context) {
	var req wifiAPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	verdicts, view, err := h.uc.EnableAP(c.Request.Context(), domain.WifiConfig{
		InterfaceID: req.InterfaceID, SSID: req.SSID, PSK: req.PSK,
		CountryCode: req.CountryCode, Band: req.Band,
		Channel: req.Channel, Hidden: req.Hidden,
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if view == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false,
			"error": "validation failed", "verdicts": verdicts})
		return
	}
	// Warnings ride the happy path, same as every other apply
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view, "verdicts": verdicts})
}

func (h *Handler) DisableWifi(c *gin.Context) {
	view, err := h.uc.DisableWifi(c.Request.Context(), c.Param("key"))
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

func (h *Handler) ScanWifi(c *gin.Context) {
	nets, err := h.uc.ScanWifi(c.Request.Context(), c.Param("key"))
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": nets})
}

func (h *Handler) ConnectWifi(c *gin.Context) {
	var req wifiConnectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	view, err := h.uc.ConnectWifi(c.Request.Context(), c.Param("key"), req.SSID, req.PSK)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}
