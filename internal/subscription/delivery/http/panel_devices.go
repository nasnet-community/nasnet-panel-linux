package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	wireguardUC "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
)

// authedPanelSub resolves the :uuid path param to a subscription and enforces
// the same password gate as every other public panel endpoint. On failure it
// has already written the response (404 / 403) and returns ok=false.
func (h *Handler) authedPanelSub(c *gin.Context) (*domain.Subscription, bool) {
	uuid := c.Param("uuid")
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return nil, false
	}
	if !h.checkSubAuth(c, sub) {
		return nil, false
	}
	return sub, true
}

func parsePanelDeviceID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("deviceId"), 10, 32)
	if err != nil {
		httputil.Error(c, http.StatusBadRequest, "invalid device id")
		return 0, false
	}
	return uint(id), true
}

// PanelWGServers lists the WireGuard servers available to this subscription —
// used to populate the "Add device" server picker. GET .../wg/servers
func (h *Handler) PanelWGServers(c *gin.Context) {
	sub, ok := h.authedPanelSub(c)
	if !ok {
		return
	}
	servers, err := h.deviceUC.ListServers(c.Request.Context(), sub.ID)
	if err != nil {
		if errors.Is(err, wireguardUC.ErrNoWGServer) {
			httputil.OK(c, []wireguardUC.WGServerOption{})
			return
		}
		httputil.Error(c, http.StatusInternalServerError, "failed to load servers")
		return
	}
	httputil.OK(c, servers)
}

// PanelDevices lists this subscription's WireGuard devices plus the device cap
// so the UI can show "used / max" and disable Add when full. GET .../devices
func (h *Handler) PanelDevices(c *gin.Context) {
	sub, ok := h.authedPanelSub(c)
	if !ok {
		return
	}
	devices, err := h.deviceUC.ListDevices(c.Request.Context(), sub.ID)
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, "failed to load devices")
		return
	}
	maxDevices, err := h.deviceUC.MaxDevices(c.Request.Context(), sub.ID)
	if err != nil {
		maxDevices = 0 // unknown cap — UI falls back to error-on-submit
	}
	httputil.OK(c, gin.H{
		"devices":     devices,
		"max_devices": maxDevices,
		"used":        len(devices),
	})
}

type panelAddDeviceBody struct {
	InboundID uint   `json:"inbound_id"`
	Label     string `json:"label"`
}

// PanelAddDevice provisions a new peer and returns its .conf — shown ONCE; the
// private key is never stored server-side. POST .../devices
func (h *Handler) PanelAddDevice(c *gin.Context) {
	sub, ok := h.authedPanelSub(c)
	if !ok {
		return
	}
	var body panelAddDeviceBody
	_ = c.ShouldBindJSON(&body)
	dc, err := h.deviceUC.CreateDevice(c.Request.Context(), sub.ID, body.InboundID, body.Label)
	if err != nil {
		switch {
		case errors.Is(err, wireguardUC.ErrDeviceCapReached):
			httputil.Error(c, http.StatusConflict, "device_limit_reached")
		case errors.Is(err, wireguardUC.ErrNoWGServer):
			httputil.Error(c, http.StatusBadRequest, "no_wireguard_server")
		default:
			httputil.Error(c, http.StatusBadGateway, "could_not_create_device")
		}
		return
	}
	httputil.OK(c, gin.H{"device": dc.Peer, "config": dc.Conf})
}

// PanelRotateDevice regenerates a device's keys; the old .conf stops working.
// POST .../devices/:deviceId/rotate
func (h *Handler) PanelRotateDevice(c *gin.Context) {
	sub, ok := h.authedPanelSub(c)
	if !ok {
		return
	}
	deviceID, ok := parsePanelDeviceID(c)
	if !ok {
		return
	}
	dc, err := h.deviceUC.RotateDevice(c.Request.Context(), sub.ID, deviceID)
	if err != nil {
		httputil.Error(c, http.StatusBadGateway, "could_not_rotate_device")
		return
	}
	httputil.OK(c, gin.H{"device": dc.Peer, "config": dc.Conf})
}

// PanelRemoveDevice deletes a peer; it stops working immediately.
// DELETE .../devices/:deviceId
func (h *Handler) PanelRemoveDevice(c *gin.Context) {
	sub, ok := h.authedPanelSub(c)
	if !ok {
		return
	}
	deviceID, ok := parsePanelDeviceID(c)
	if !ok {
		return
	}
	if err := h.deviceUC.RemoveDevice(c.Request.Context(), sub.ID, deviceID); err != nil {
		httputil.Error(c, http.StatusBadGateway, "could_not_remove_device")
		return
	}
	httputil.OK(c, gin.H{"removed": true})
}
