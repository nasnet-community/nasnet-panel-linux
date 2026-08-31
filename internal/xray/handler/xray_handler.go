package handler

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/audit"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/auth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// XrayHandler handles xray binary distribution endpoints.
type XrayHandler struct {
	bm           *usecase.BinaryManager
	settingUC    settingDomain.SettingUsecase
	tokenManager *auth.TokenManager
	auditUC      auditDomain.AuditLogUsecase
}

// NewXrayHandler creates a new XrayHandler.
func NewXrayHandler(bm *usecase.BinaryManager, settingUC settingDomain.SettingUsecase, tokenManager *auth.TokenManager, auditUC auditDomain.AuditLogUsecase) *XrayHandler {
	return &XrayHandler{
		bm:           bm,
		settingUC:    settingUC,
		tokenManager: tokenManager,
		auditUC:      auditUC,
	}
}

// logAudit records an admin action; mirrors BackupHandler.logAudit.
func (h *XrayHandler) logAudit(c *gin.Context, action auditDomain.AuditAction, detail string) {
	if h.auditUC == nil {
		return
	}
	ac := audit.FromGinContext(c)
	h.auditUC.Log(c.Request.Context(), &auditDomain.AuditLog{
		Action:     string(action),
		ActorID:    ac.ActorID,
		ActorName:  ac.ActorName,
		EntityType: "xray_binary",
		NewValues:  detail,
		IPAddress:  ac.IPAddress,
		RequestID:  ac.RequestID,
		Source:     "http",
	})
}

// RegisterPublicRoutes registers public routes for xray binary distribution.
func (h *XrayHandler) RegisterPublicRoutes(g *gin.RouterGroup) {
	g.GET("/binary", h.GetXrayBinary)
	g.GET("/checksum", h.GetXrayChecksum)
}

// RegisterAdminRoutes registers admin routes for xray binary management.
func (h *XrayHandler) RegisterAdminRoutes(g *gin.RouterGroup) {
	xray := g.Group("/xray")
	{
		xray.GET("/versions", h.ListVersions)
		xray.PUT("/binary", h.UploadBinary)
		xray.DELETE("/binary", h.DeleteVersion)
		xray.POST("/download", h.TriggerDownload)
	}
}

// resolveVersion returns the version from query param, or the default setting, or fallback.
func (h *XrayHandler) resolveVersion(c *gin.Context) string {
	if v := c.Query("version"); v != "" {
		return v
	}
	if h.settingUC != nil {
		if v, err := h.settingUC.GetByKey(c.Request.Context(), "xray_default_version"); err == nil && v != "" {
			return v
		}
	}
	return "26.7.28"
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

// GetXrayBinary serves a cached xray binary file. Requires a valid
// deployment token.
func (h *XrayHandler) GetXrayBinary(c *gin.Context) {
	if !h.requireDeploymentToken(c) {
		return
	}
	log := logger.GetLogger()
	version := h.resolveVersion(c)
	arch := h.resolveArch(c)

	if !usecase.IsValidVersion(version) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid version format"})
		return
	}

	// Single call returns bytes + checksum atomically so a concurrent
	// DeleteVersion cannot race between EnsureCached and ReadFile.
	data, checksum, err := h.bm.EnsureAndLoad(version, arch)
	if err != nil {
		log.WithField("version", version).WithField("arch", arch).WithError(err).Warn("xray binary not available")
		c.JSON(http.StatusNotFound, gin.H{"error": "Xray binary not cached. Upload it manually or ensure the hub has internet access."})
		return
	}
	c.Header("X-Binary-SHA256", checksum)
	c.Data(http.StatusOK, "application/octet-stream", data)
}

// GetXrayChecksum returns the SHA256 checksum for a cached xray binary.
func (h *XrayHandler) GetXrayChecksum(c *gin.Context) {
	if !h.requireDeploymentToken(c) {
		return
	}
	version := h.resolveVersion(c)
	arch := h.resolveArch(c)

	if !usecase.IsValidVersion(version) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid version format"})
		return
	}

	checksum, err := h.bm.GetChecksum(version, arch)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Checksum not available"})
		return
	}

	c.String(http.StatusOK, checksum)
}

// ListVersions returns all cached xray versions with platform info.
func (h *XrayHandler) ListVersions(c *gin.Context) {
	versions := h.bm.ListVersions()
	if versions == nil {
		versions = []usecase.VersionInfo{}
	}

	// Mark default version
	defaultVersion := h.resolveVersion(c)
	for i := range versions {
		if versions[i].Version == defaultVersion {
			versions[i].IsDefault = true
		}
	}

	c.JSON(http.StatusOK, gin.H{"versions": versions})
}

// UploadBinary handles admin binary upload (raw body, max 100MB). Arch is
// detected from the ELF header, never trusted from the client — a mislabeled
// binary would otherwise only fail later at the agent's arch check.
func (h *XrayHandler) UploadBinary(c *gin.Context) {
	version := c.Query("version")
	if !usecase.IsValidVersion(version) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid version format"})
		return
	}

	// Limit request body to 100MB
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 100<<20)

	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body or body too large"})
		return
	}

	arch, err := usecase.DetectELFArch(data)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if q := c.Query("arch"); q != "" && q != arch {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("binary is %s but arch=%s was supplied", arch, q)})
		return
	}

	if err := h.bm.StoreBinary(version, arch, data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logAudit(c, auditDomain.AuditXrayBinaryUpload, fmt.Sprintf("%s/%s", version, arch))
	c.JSON(http.StatusOK, gin.H{"message": "Binary stored successfully", "version": version, "arch": arch})
}

// DeleteVersion removes a cached xray version.
func (h *XrayHandler) DeleteVersion(c *gin.Context) {
	version := c.Query("version")

	if !usecase.IsValidVersion(version) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid version format"})
		return
	}

	// Prevent deleting the default version
	defaultVersion := h.resolveVersion(c)
	if version == defaultVersion {
		c.JSON(http.StatusConflict, gin.H{"error": "Cannot delete the default xray version. Change the default version first."})
		return
	}

	if err := h.bm.DeleteVersion(version); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	h.logAudit(c, auditDomain.AuditXrayBinaryDelete, version)
	c.JSON(http.StatusOK, gin.H{"message": "Version deleted", "version": version})
}

// TriggerDownload triggers a background download of an xray version from GitHub.
func (h *XrayHandler) TriggerDownload(c *gin.Context) {
	version := c.Query("version")
	if version == "" {
		version = h.resolveVersion(c)
	}

	if !usecase.IsValidVersion(version) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid version format"})
		return
	}

	go h.bm.PrefetchVersion(version)

	c.JSON(http.StatusAccepted, gin.H{"message": "Download started in background", "version": version})
}
