package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/audit"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/auth"
	"gorm.io/gorm"
)

// jsonResponse provides consistent API response format
type jsonResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// CertificateHandler handles certificate-related HTTP endpoints
type CertificateHandler struct {
	certUC       usecase.CertificateUsecase
	nodeUC       usecase.NodeUsecase
	auditUC      auditDomain.AuditLogUsecase
	tokenManager *auth.TokenManager
	usedTokens   repository.UsedTokenRepository
}

// SetUsedTokens makes the agent bundle one-time per deployment token.
func (h *CertificateHandler) SetUsedTokens(r repository.UsedTokenRepository) { h.usedTokens = r }

func NewCertificateHandler(certUC usecase.CertificateUsecase, nodeUC usecase.NodeUsecase, auditUC auditDomain.AuditLogUsecase, tokenManager *auth.TokenManager) *CertificateHandler {
	return &CertificateHandler{certUC: certUC, nodeUC: nodeUC, auditUC: auditUC, tokenManager: tokenManager}
}

// RegisterAdminRoutes registers certificate routes requiring admin access
func (h *CertificateHandler) RegisterAdminRoutes(g *gin.RouterGroup) {
	c := g.Group("/certificates")
	{
		c.GET("", h.ListCertificates)
		c.GET("/overview", h.Overview)
		c.GET("/status", h.GetCAStatus)
		c.GET("/expiring", h.ListExpiringSoon)
		c.GET("/details/:id", h.GetCertificateByID)
		c.POST("/ca", h.InitializeCA)
		c.POST("/ca/reinitialize", h.ReinitializeCA)
		c.GET("/ca", h.GetCA)
		c.POST("/nodes/:node_id/rotate", h.RotateNodeCert)
		c.POST("/rotate-expiring", h.RotateExpiring)
		c.POST("/orphans/revoke", h.RevokeOrphans)
		c.POST("/purge", h.Purge)
		c.PUT("/strict-mode", h.SetStrictMode)
		c.POST("/:id/revoke", h.RevokeCertificate)
		c.POST("/:id/private-key", h.RevealPrivateKey)
		c.DELETE("/:id", h.DeleteCertificate)
	}
}

// RegisterPublicRoutes registers certificate routes that use token authentication
func (h *CertificateHandler) RegisterPublicRoutes(g *gin.RouterGroup) {
	g.Group("/certificates").GET("/agent/:node_id/bundle", h.GetCertBundle)
}

// certError maps usecase errors: coded ones → 422, missing → 404, rest → 500.
func certError(c *gin.Context, err error) {
	if errors.Is(err, usecase.ErrNoStoredKey) || errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	if code := usecase.CertErrorCode(err); code != "" {
		body := gin.H{"success": false, "error": err.Error(), "code": code}
		var notReady *usecase.StrictModeNotReadyError
		if errors.As(err, &notReady) {
			body["data"] = gin.H{"blockers": notReady.Blockers}
		}
		c.JSON(http.StatusUnprocessableEntity, body)
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
}

func uintParam(c *gin.Context, name string) (uint, bool) {
	v, err := strconv.ParseUint(c.Param(name), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid " + name})
		return 0, false
	}
	return uint(v), true
}

func (h *CertificateHandler) audit(c *gin.Context, action string, entityID uint, values any) {
	if h.auditUC == nil {
		return
	}
	ac := audit.FromGinContext(c)
	payload, _ := json.Marshal(values)
	h.auditUC.Log(c.Request.Context(), &auditDomain.AuditLog{
		Action:     action,
		ActorID:    ac.ActorID,
		ActorName:  ac.ActorName,
		EntityType: "certificate",
		EntityID:   entityID,
		NewValues:  string(payload),
		IPAddress:  ac.IPAddress,
		RequestID:  ac.RequestID,
		Source:     "admin_api",
	})
}

type confirmBody struct {
	Confirm string `json:"confirm"`
}

func (h *CertificateHandler) Overview(c *gin.Context) {
	ov, err := h.nodeUC.CertificateOverview(c.Request.Context(), c.Query("refresh_domains") == "1")
	if err != nil {
		certError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": ov})
}

func (h *CertificateHandler) RotateNodeCert(c *gin.Context) {
	nodeID, ok := uintParam(c, "node_id")
	if !ok {
		return
	}
	rec, err := h.nodeUC.RotateAgentCert(c.Request.Context(), nodeID)
	if err != nil {
		certError(c, err)
		return
	}
	h.audit(c, "cert_rotate", rec.ID, gin.H{"node_id": nodeID, "serial": rec.SerialNumber})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"id": rec.ID, "serial": rec.SerialNumber, "common_name": rec.CommonName, "not_after": rec.NotAfter,
	}})
}

func (h *CertificateHandler) RotateExpiring(c *gin.Context) {
	results, err := h.nodeUC.RotateExpiringCerts(c.Request.Context())
	if err != nil {
		certError(c, err)
		return
	}
	h.audit(c, "cert_rotate_expiring", 0, results)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": results})
}

func (h *CertificateHandler) RevokeOrphans(c *gin.Context) {
	n, err := h.certUC.RevokeOrphans(c.Request.Context())
	if err != nil {
		certError(c, err)
		return
	}
	h.audit(c, "cert_revoke_orphans", 0, gin.H{"revoked": n})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"revoked": n}})
}

func (h *CertificateHandler) Purge(c *gin.Context) {
	n, err := h.certUC.PurgeRevokedExpired(c.Request.Context())
	if err != nil {
		certError(c, err)
		return
	}
	h.audit(c, "cert_purge", 0, gin.H{"purged": n})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"purged": n}})
}

func (h *CertificateHandler) SetStrictMode(c *gin.Context) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "enabled is required"})
		return
	}
	if err := h.nodeUC.SetCertStrictMode(c.Request.Context(), *body.Enabled); err != nil {
		certError(c, err)
		return
	}
	h.audit(c, "cert_strict_mode", 0, gin.H{"enabled": *body.Enabled})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"enabled": *body.Enabled}})
}

func (h *CertificateHandler) RevokeCertificate(c *gin.Context) {
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	var body confirmBody
	_ = c.ShouldBindJSON(&body) // only a node's current cert needs it
	if err := h.certUC.RevokeByAdmin(c.Request.Context(), id, body.Confirm); err != nil {
		certError(c, err)
		return
	}
	h.audit(c, "cert_revoke", id, gin.H{"id": id})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"message": "Certificate revoked"}})
}

func (h *CertificateHandler) DeleteCertificate(c *gin.Context) {
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	if err := h.certUC.DeleteCertificate(c.Request.Context(), id, c.Query("confirm")); err != nil {
		certError(c, err)
		return
	}
	h.audit(c, "cert_delete", id, gin.H{"id": id})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"message": "Certificate deleted"}})
}

func (h *CertificateHandler) RevealPrivateKey(c *gin.Context) {
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	var body confirmBody
	_ = c.ShouldBindJSON(&body)
	key, err := h.certUC.RevealPrivateKey(c.Request.Context(), id, body.Confirm)
	if err != nil {
		certError(c, err)
		return
	}
	h.audit(c, "cert_key_reveal", id, gin.H{"id": id})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"private_key": key}})
}

// GetCertificateByID returns metadata, PEM and parsed fields, never the key.
func (h *CertificateHandler) GetCertificateByID(c *gin.Context) {
	id, ok := uintParam(c, "id")
	if !ok {
		return
	}
	cert, err := h.certUC.GetCertificate(c.Request.Context(), id)
	if err != nil {
		certError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": certDetails(cert)})
}

func certDetails(cert *domain.AgentCertificate) gin.H {
	d := gin.H{
		"id":              cert.ID,
		"type":            cert.Type,
		"node_id":         cert.NodeID,
		"common_name":     cert.CommonName,
		"serial":          cert.SerialNumber,
		"not_before":      cert.NotBefore,
		"not_after":       cert.NotAfter,
		"days_left":       cert.DaysUntilExpiry(),
		"is_revoked":      cert.IsRevoked,
		"revoked_at":      cert.RevokedAt,
		"certificate":     string(cert.Certificate),
		"has_private_key": len(cert.PrivateKey) > 0,
	}
	if leaf, err := certs.ParseCertificate(cert.Certificate); err == nil {
		ips := make([]string, 0, len(leaf.IPAddresses))
		for _, ip := range leaf.IPAddresses {
			ips = append(ips, ip.String())
		}
		d["dns_names"] = leaf.DNSNames
		d["ip_addresses"] = ips
		d["issuer"] = leaf.Issuer.CommonName
		d["fingerprint_sha256"] = usecase.FingerprintOf(cert.Certificate)
	}
	return d
}

// ListCertificates returns all certificates
func (h *CertificateHandler) ListCertificates(c *gin.Context) {
	certs, err := h.certUC.ListCertificates(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	// Return without private keys
	result := make([]gin.H, len(certs))
	for i, cert := range certs {
		result[i] = gin.H{
			"id":                cert.ID,
			"type":              cert.Type,
			"node_id":           cert.NodeID,
			"common_name":       cert.CommonName,
			"serial_number":     cert.SerialNumber,
			"not_before":        cert.NotBefore,
			"not_after":         cert.NotAfter,
			"is_revoked":        cert.IsRevoked,
			"is_valid":          cert.IsValid(),
			"days_until_expiry": cert.DaysUntilExpiry(),
			"auto_renew":        cert.AutoRenew,
			"created_at":        cert.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, jsonResponse{Success: true, Data: gin.H{"certificates": result}})
}

// InitializeCA creates a new CA certificate
func (h *CertificateHandler) InitializeCA(c *gin.Context) {
	var req struct {
		CommonName   string `json:"common_name"`
		Organization string `json:"organization"`
		Country      string `json:"country"`
		ValidYears   int    `json:"valid_years"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		// Use defaults
	}

	cfg := certs.DefaultCAConfig()
	if req.CommonName != "" {
		cfg.CommonName = req.CommonName
	}
	if req.Organization != "" {
		cfg.Organization = req.Organization
	}
	if req.Country != "" {
		cfg.Country = req.Country
	}
	if req.ValidYears > 0 {
		cfg.ValidYears = req.ValidYears
	}

	cert, err := h.certUC.InitializeCA(c.Request.Context(), cfg)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, jsonResponse{
		Success: true,
		Data: gin.H{
			"id":          cert.ID,
			"common_name": cert.CommonName,
			"not_before":  cert.NotBefore,
			"not_after":   cert.NotAfter,
		},
	})
}

// GetCA returns the CA certificate
func (h *CertificateHandler) GetCA(c *gin.Context) {
	cert, err := h.certUC.GetCA(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "CA not found"})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data: gin.H{
			"id":                cert.ID,
			"type":              cert.Type,
			"common_name":       cert.CommonName,
			"serial_number":     cert.SerialNumber,
			"not_before":        cert.NotBefore,
			"not_after":         cert.NotAfter,
			"is_revoked":        cert.IsRevoked,
			"is_valid":          cert.IsValid(),
			"days_until_expiry": cert.DaysUntilExpiry(),
			"certificate":       string(cert.Certificate),
		},
	})
}

// ReinitializeCA deletes all certificates and creates a new CA
func (h *CertificateHandler) ReinitializeCA(c *gin.Context) {
	var req struct {
		CommonName string `json:"common_name"`
		ValidYears int    `json:"valid_years"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		// Use defaults if not provided
		req.CommonName = "NasNet Root CA"
		req.ValidYears = 10
	}

	if req.CommonName == "" {
		req.CommonName = "NasNet Root CA"
	}
	if req.ValidYears <= 0 {
		req.ValidYears = 10
	}

	cert, err := h.certUC.ReinitializeCA(c.Request.Context(), &certs.CAConfig{
		CommonName: req.CommonName,
		ValidYears: req.ValidYears,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, jsonResponse{
		Success: true,
		Data: gin.H{
			"id":          cert.ID,
			"common_name": cert.CommonName,
			"not_before":  cert.NotBefore,
			"not_after":   cert.NotAfter,
			"message":     "CA reinitialized. All previous certificates have been deleted. Please redeploy all agents.",
		},
	})
}

// GetCAStatus returns the current CA and master cert status
func (h *CertificateHandler) GetCAStatus(c *gin.Context) {
	ctx := c.Request.Context()

	status := gin.H{
		"ca_initialized": false,
	}

	// Check CA
	ca, err := h.certUC.GetCA(ctx)
	if err == nil && ca != nil {
		status["ca_initialized"] = true
		status["ca_expires"] = ca.NotAfter
		status["ca_common_name"] = ca.CommonName
	}

	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data:    status,
	})
}

// GetCertBundle returns all certificates needed for agent deployment
func (h *CertificateHandler) GetCertBundle(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("node_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid node ID"})
		return
	}

	// Validate token
	token := ""
	authHeader := c.GetHeader("Authorization")
	if len(authHeader) >= 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	} else {
		token = c.Query("token")
	}

	if token == "" {
		c.JSON(http.StatusUnauthorized, jsonResponse{Success: false, Error: "Missing or invalid authorization token"})
		return
	}
	claims, err := h.tokenManager.ValidateDeploymentToken(token)
	if err != nil || uint(claims.NodeID) != uint(nodeID) {
		c.JSON(http.StatusUnauthorized, jsonResponse{Success: false, Error: "Invalid or expired deployment token"})
		return
	}
	if !MarkDeploymentTokenUsed(c, h.usedTokens, claims, "bundle") {
		return
	}

	bundle, err := h.certUC.GetCertificateBundle(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data: gin.H{
			"node_id":    bundle.NodeID,
			"node_name":  bundle.NodeName,
			"node_uuid":  bundle.NodeUUID,
			"ca_cert":    string(bundle.CACert),
			"agent_cert": string(bundle.AgentCert),
			"agent_key":  string(bundle.AgentKey),
		},
	})
}

// ListExpiringSoon returns certificates expiring within N days
func (h *CertificateHandler) ListExpiringSoon(c *gin.Context) {
	days := 30 // default
	if d := c.Query("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	certs, err := h.certUC.ListExpiringSoon(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	result := make([]gin.H, len(certs))
	for i, cert := range certs {
		result[i] = gin.H{
			"id":                cert.ID,
			"type":              cert.Type,
			"node_id":           cert.NodeID,
			"common_name":       cert.CommonName,
			"not_after":         cert.NotAfter,
			"days_until_expiry": cert.DaysUntilExpiry(),
		}
	}

	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data:    gin.H{"certificates": result, "days_threshold": days},
	})
}
