package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
)

// jsonResponse provides consistent API response format
type jsonResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// CertificateHandler handles certificate-related HTTP endpoints. In single-binary
// mode these cover public / ACME (Let's Encrypt) certs used for inbound TLS —
// the agent-mTLS PKI (CA, master, per-agent certs) is gone.
type CertificateHandler struct {
	certUC usecase.CertificateUsecase
}

// NewCertificateHandler creates a new certificate handler
func NewCertificateHandler(certUC usecase.CertificateUsecase) *CertificateHandler {
	return &CertificateHandler{certUC: certUC}
}

// RegisterAdminRoutes registers certificate routes requiring admin access
func (h *CertificateHandler) RegisterAdminRoutes(g *gin.RouterGroup) {
	certs := g.Group("/certificates")
	{
		certs.GET("", h.ListCertificates)
		certs.POST("/:id/revoke", h.RevokeCertificate)
		certs.GET("/expiring", h.ListExpiringSoon)
		certs.POST("/public", h.IssuePublicCert)
		certs.POST("/dns/start", h.StartDNSChallenge)
		certs.POST("/dns/complete", h.CompleteDNSChallenge)
		certs.POST("/:id/renew", h.RenewCertificate)
		certs.DELETE("/:id", h.DeleteCertificate)
		certs.GET("/details/:id", h.GetCertificateByID)
		certs.POST("/:id/auto-renew", h.ToggleAutoRenew)
	}
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

// RevokeCertificate marks a certificate as revoked
func (h *CertificateHandler) RevokeCertificate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid certificate ID"})
		return
	}

	if err := h.certUC.RevokeCertificate(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{Success: true, Data: gin.H{"message": "Certificate revoked"}})
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

// IssuePublicCert issues a public certificate
func (h *CertificateHandler) IssuePublicCert(c *gin.Context) {
	var req struct {
		Domain string `json:"domain" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Domain is required"})
		return
	}

	cert, err := h.certUC.IssuePublicCert(c.Request.Context(), req.Domain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, jsonResponse{
		Success: true,
		Data: gin.H{
			"id":            cert.ID,
			"type":          cert.Type,
			"common_name":   cert.CommonName,
			"not_after":     cert.NotAfter,
			"serial_number": cert.SerialNumber,
		},
	})
}

// StartDNSChallenge initiates a DNS-01 challenge
func (h *CertificateHandler) StartDNSChallenge(c *gin.Context) {
	var req struct {
		Domain string `json:"domain" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Domain is required"})
		return
	}

	challenge, err := h.certUC.StartDNSChallenge(c.Request.Context(), req.Domain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data: gin.H{
			"domain":     challenge.Domain,
			"txt_record": challenge.TXTRecord,
			"txt_value":  challenge.TXTValue,
		},
	})
}

// CompleteDNSChallenge completes a DNS-01 challenge
func (h *CertificateHandler) CompleteDNSChallenge(c *gin.Context) {
	var req struct {
		Domain string `json:"domain" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Domain is required"})
		return
	}

	cert, err := h.certUC.CompleteDNSChallenge(c.Request.Context(), req.Domain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data: gin.H{
			"id":            cert.ID,
			"type":          cert.Type,
			"common_name":   cert.CommonName,
			"not_after":     cert.NotAfter,
			"serial_number": cert.SerialNumber,
		},
	})
}

// RenewCertificate renews a certificate
func (h *CertificateHandler) RenewCertificate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid certificate ID"})
		return
	}

	cert, err := h.certUC.RenewCertificate(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data: gin.H{
			"id":            cert.ID,
			"type":          cert.Type,
			"common_name":   cert.CommonName,
			"not_after":     cert.NotAfter,
			"serial_number": cert.SerialNumber,
		},
	})
}

// DeleteCertificate deletes a certificate
func (h *CertificateHandler) DeleteCertificate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid certificate ID"})
		return
	}

	if err := h.certUC.DeleteCertificate(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{Success: true, Data: gin.H{"message": "Certificate deleted"}})
}

// ToggleAutoRenew toggles the auto-renew flag for a certificate
func (h *CertificateHandler) ToggleAutoRenew(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid certificate ID"})
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid request body"})
		return
	}

	if err := h.certUC.ToggleAutoRenew(c.Request.Context(), uint(id), req.Enabled); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{Success: true, Data: gin.H{"auto_renew": req.Enabled}})
}

// GetCertificateByID returns certificate details including PEM content
func (h *CertificateHandler) GetCertificateByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid certificate ID"})
		return
	}

	cert, err := h.certUC.GetCertificate(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Certificate not found"})
		return
	}

	response := gin.H{
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
		"certificate":       string(cert.Certificate),
	}

	if len(cert.PrivateKey) > 0 {
		response["private_key"] = string(cert.PrivateKey)
	}

	c.JSON(http.StatusOK, jsonResponse{Success: true, Data: response})
}
