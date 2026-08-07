package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
)

type SettingHandler struct {
	usecase domain.SettingUsecase
}

func NewSettingHandler(usecase domain.SettingUsecase) *SettingHandler {
	return &SettingHandler{usecase: usecase}
}

func (h *SettingHandler) RegisterRoutes(router *gin.RouterGroup) {
	settings := router.Group("/settings")
	{
		settings.GET("", h.GetAll)
		settings.PUT("", h.UpdateMany)
		settings.GET("/export", h.Export)
		settings.POST("/import", h.Import)
		settings.POST("/test-tls", h.TestTLS)
		settings.POST("/test-proxy", h.TestProxy)
	}
}

// GetAll godoc
// @Summary      Get all settings
// @Description  Get all system settings grouped by category
// @Tags         settings
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string][]domain.Setting
// @Failure      500  {object}  map[string]string
// @Router       /settings [get]
func (h *SettingHandler) GetAll(c *gin.Context) {
	settings, err := h.usecase.GetAll(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}
	c.JSON(http.StatusOK, settings)
}

// UpdateMany godoc
// @Summary      Update multiple settings
// @Description  Update multiple system settings
// @Tags         settings
// @Accept       json
// @Produce      json
// @Param        settings  body      []domain.Setting  true  "List of settings to update"
// @Success      200       {object}  map[string]string
// @Failure      400       {object}  map[string]string
// @Failure      500       {object}  map[string]string
// @Router       /settings [put]
func (h *SettingHandler) UpdateMany(c *gin.Context) {
	var settings []*domain.Setting
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.usecase.UpdateMany(c.Request.Context(), settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings updated successfully"})
}

// Export godoc
// @Summary      Export all settings
// @Description  Export all system settings as a flat list (excluding sensitive values)
// @Tags         settings
// @Produce      json
// @Success      200  {array}  domain.Setting
// @Failure      500  {object}  map[string]string
// @Router       /settings/export [get]
func (h *SettingHandler) Export(c *gin.Context) {
	settings, err := h.usecase.GetAll(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export settings"})
		return
	}

	// Flatten to a list (already masked by GetAll)
	var flat []*domain.Setting
	for _, categorySettings := range settings {
		flat = append(flat, categorySettings...)
	}

	c.JSON(http.StatusOK, flat)
}

// Import godoc
// @Summary      Import settings
// @Description  Import a list of settings (merges with existing, skips masked values)
// @Tags         settings
// @Accept       json
// @Produce      json
// @Param        settings  body      []domain.Setting  true  "List of settings to import"
// @Success      200       {object}  map[string]interface{}
// @Failure      400       {object}  map[string]string
// @Failure      500       {object}  map[string]string
// @Router       /settings/import [post]
func (h *SettingHandler) Import(c *gin.Context) {
	var settings []*domain.Setting
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.usecase.UpdateMany(c.Request.Context(), settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to import settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings imported successfully", "count": len(settings)})
}

// TestTLS validates that TLS certificate files can be loaded and returns certificate details.
func (h *SettingHandler) TestTLS(c *gin.Context) {
	var req struct {
		CertFile string `json:"cert_file"`
		KeyFile  string `json:"key_file"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request"})
		return
	}
	if req.CertFile == "" || req.KeyFile == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Both cert_file and key_file are required"})
		return
	}

	cert, err := tls.LoadX509KeyPair(req.CertFile, req.KeyFile)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Parse the leaf certificate for details
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Certificate loaded but could not parse details: " + err.Error(),
		})
		return
	}

	now := time.Now()
	expired := now.After(leaf.NotAfter)
	notYetValid := now.Before(leaf.NotBefore)
	daysUntilExpiry := int(time.Until(leaf.NotAfter).Hours() / 24)

	var domains []string
	if leaf.Subject.CommonName != "" {
		domains = append(domains, leaf.Subject.CommonName)
	}
	for _, san := range leaf.DNSNames {
		if san != leaf.Subject.CommonName {
			domains = append(domains, san)
		}
	}

	data := gin.H{
		"subject":           leaf.Subject.CommonName,
		"issuer":            leaf.Issuer.CommonName,
		"domains":           domains,
		"not_before":        leaf.NotBefore.Format(time.RFC3339),
		"not_after":         leaf.NotAfter.Format(time.RFC3339),
		"days_until_expiry": daysUntilExpiry,
		"expired":           expired,
		"not_yet_valid":     notYetValid,
	}

	if expired {
		data["warning"] = "Certificate has expired"
	} else if notYetValid {
		data["warning"] = "Certificate is not yet valid"
	} else if daysUntilExpiry < 30 {
		data["warning"] = "Certificate expires soon"
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// TestProxy validates a candidate outbound proxy URL by constructing a
// throwaway factory and making a HEAD request through it. The admin UI uses
// this to verify reachability before saving the URL.
func (h *SettingHandler) TestProxy(c *gin.Context) {
	var req struct {
		URL        string `json:"url" binding:"required"`
		TestTarget string `json:"test_target"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if req.TestTarget == "" {
		req.TestTarget = "https://www.google.com/generate_204"
	}

	f := httpclient.NewFactory()
	f.Update(httpclient.Config{
		ProxyURL: req.URL,
		Enabled:  map[httpclient.Feature]bool{httpclient.FeatureGeofiles: true},
	})
	if !f.IsProxyConfigured() {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "invalid proxy url (must be socks5:// or socks5h://host:port)",
		})
		return
	}

	client := f.ClientFor(httpclient.FeatureGeofiles, httpclient.EgressForeign, 10*time.Second)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodHead, req.TestTarget, nil)
	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "reachable": false, "error": err.Error()})
		return
	}
	resp.Body.Close()
	c.JSON(http.StatusOK, gin.H{"success": true, "reachable": true, "status": resp.StatusCode})
}
