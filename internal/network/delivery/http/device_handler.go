package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ListDevices reports what is on the LAN bridge.
//
// Always 200 with health flags rather than an error when a source is missing:
// an unexplained empty list is indistinguishable from "nothing is connected",
// which is the question the caller asked. Only a failure to read the bridge
// itself is an error, because then the answer is genuinely unknown.
func (h *Handler) ListDevices(c *gin.Context) {
	list, err := h.uc.ListDevices(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// SetDeviceLabel names a device, or clears the name when label is empty. Mirrors
// the interface SetLabel: stable key in the path, label in the body.
func (h *Handler) SetDeviceLabel(c *gin.Context) {
	var body struct {
		Label string `json:"label"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.uc.SetDeviceLabel(c.Request.Context(), c.Param("mac"), body.Label); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
