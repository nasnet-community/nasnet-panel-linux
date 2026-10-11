package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/auth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// XrayHandler serves verified xray-core builds to nodes being deployed or updated.
type XrayHandler struct {
	bm           *usecase.BinaryManager
	tokenManager *auth.TokenManager
}

// NewXrayHandler creates a new XrayHandler.
func NewXrayHandler(bm *usecase.BinaryManager, tokenManager *auth.TokenManager) *XrayHandler {
	return &XrayHandler{bm: bm, tokenManager: tokenManager}
}

// RegisterPublicRoutes registers the deployment-token routes nodes download xray from.
func (h *XrayHandler) RegisterPublicRoutes(g *gin.RouterGroup) {
	g.GET("/binary", h.GetXrayBinary)
	g.GET("/checksum", h.GetXrayChecksum)
}

// resolveVersion takes the requested release, or the pinned one when none is named.
func (h *XrayHandler) resolveVersion(c *gin.Context) (string, bool) {
	v := c.Query("version")
	if v == "" {
		return h.bm.Pinned(), true
	}
	if !usecase.IsValidVersion(v) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid version format"})
		return "", false
	}
	return v, true
}

// resolveArch returns the arch from query param, defaulting to amd64.
func (h *XrayHandler) resolveArch(c *gin.Context) string {
	arch := c.Query("arch")
	if arch == "arm64" || arch == "amd64" {
		return arch
	}
	return "amd64"
}

// requireDeploymentToken: Authorization: Bearer (or ?token=) → deployment
// JWT issuer. Gates xray-binary routes (not under JWT-admin). 401 on fail.
func (h *XrayHandler) requireDeploymentToken(c *gin.Context) bool {
	if h.tokenManager == nil {
		// Token manager not wired — refuse rather than serve open.
		c.JSON(http.StatusUnauthorized, gin.H{"error": "deployment auth not configured"})
		return false
	}
	token := ""
	if hdr := c.GetHeader("Authorization"); len(hdr) >= 7 && hdr[:7] == "Bearer " {
		token = hdr[7:]
	} else {
		token = c.Query("token")
	}
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing deployment token"})
		return false
	}
	if _, err := h.tokenManager.ValidateDeploymentToken(token); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired deployment token"})
		return false
	}
	return true
}

// GetXrayBinary serves a verified xray binary with its hash. Requires a valid
// deployment token.
func (h *XrayHandler) GetXrayBinary(c *gin.Context) {
	if !h.requireDeploymentToken(c) {
		return
	}
	version, ok := h.resolveVersion(c)
	if !ok {
		return
	}
	arch := h.resolveArch(c)
	data, checksum, err := h.bm.EnsureAndLoad(version, arch)
	if err != nil {
		logger.GetLogger().WithField("version", version).WithField("arch", arch).WithError(err).Warn("xray binary not available")
		c.JSON(http.StatusNotFound, gin.H{"error": "Xray binary not available; the panel needs internet access to fetch it"})
		return
	}
	c.Header("X-Binary-SHA256", checksum)
	c.Data(http.StatusOK, "application/octet-stream", data)
}

// GetXrayChecksum returns a verified binary's SHA256, fetching it first so the hash is never missing.
func (h *XrayHandler) GetXrayChecksum(c *gin.Context) {
	if !h.requireDeploymentToken(c) {
		return
	}
	version, ok := h.resolveVersion(c)
	if !ok {
		return
	}
	arch := h.resolveArch(c)
	if _, checksum, err := h.bm.EnsureAndLoad(version, arch); err == nil {
		c.String(http.StatusOK, checksum)
		return
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Checksum not available"})
}
