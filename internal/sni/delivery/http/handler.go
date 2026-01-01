package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/sni/usecase"
)

type Handler struct {
	sniUsecase usecase.SNIUsecase
}

func NewHandler(sniUsecase usecase.SNIUsecase) *Handler {
	return &Handler{sniUsecase: sniUsecase}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	sni := rg.Group("/sni")
	{
		// CRUD
		sni.GET("", h.List)
		sni.GET("/:id", h.GetByID)
		sni.GET("/domain/:domain", h.GetByDomain)
		sni.POST("", h.Create)
		sni.POST("/paths", h.CreateWithPaths)
		sni.PUT("/:id", h.Update)
		sni.DELETE("/:id", h.Delete)
		sni.GET("/:id/usage", h.GetUsage)

		// Validation
		sni.POST("/validate", h.ValidateCertificate)

		// ACME Certificate Issuance
		sni.POST("/acme/http01", h.IssueCertHTTP01)
		sni.POST("/acme/dns01/start", h.StartDNS01Challenge)
		sni.POST("/acme/dns01/complete", h.CompleteDNS01Challenge)

		// Certificate Management
		sni.POST("/:id/renew", h.RenewCertificate)
		sni.GET("/expiring", h.GetExpiringCertificates)
	}
}

func (h *Handler) List(c *gin.Context) {
	snis, err := h.sniUsecase.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": snis})
}

func (h *Handler) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	sni, err := h.sniUsecase.GetByID(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": sni})
}

func (h *Handler) GetByDomain(c *gin.Context) {
	domain := c.Param("domain")

	sni, err := h.sniUsecase.GetByDomain(c.Request.Context(), domain)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": sni})
}

type createSNIRequest struct {
	Name        string `json:"name" binding:"required"`
	Domain      string `json:"domain" binding:"required"`
	Certificate string `json:"certificate" binding:"required"`
	PrivateKey  string `json:"private_key" binding:"required"`
	ALPN        string `json:"alpn"`
}

func (h *Handler) Create(c *gin.Context) {
	var req createSNIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	sni, err := h.sniUsecase.Create(
		c.Request.Context(),
		req.Name,
		req.Domain,
		req.Certificate,
		req.PrivateKey,
		req.ALPN,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": sni})
}

type createSNIWithPathsRequest struct {
	Name     string `json:"name" binding:"required"`
	Domain   string `json:"domain" binding:"required"`
	CertPath string `json:"cert_path" binding:"required"`
	KeyPath  string `json:"key_path" binding:"required"`
	ALPN     string `json:"alpn"`
}

func (h *Handler) CreateWithPaths(c *gin.Context) {
	var req createSNIWithPathsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	sni, err := h.sniUsecase.CreateWithPaths(
		c.Request.Context(),
		req.Name,
		req.Domain,
		req.CertPath,
		req.KeyPath,
		req.ALPN,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": sni})
}

type updateSNIRequest struct {
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
	ALPN        string `json:"alpn"`
}

func (h *Handler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req updateSNIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.sniUsecase.Update(
		c.Request.Context(),
		uint(id),
		req.Name,
		req.Domain,
		req.Certificate,
		req.PrivateKey,
		req.ALPN,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "SNI updated"})
}

func (h *Handler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.sniUsecase.Delete(c.Request.Context(), uint(id)); err != nil {
		// In-use guard and other domain errors are client-correctable.
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "SNI deleted"})
}

// GetUsage reports how many inbounds reference this domain's certificate.
func (h *Handler) GetUsage(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	n, err := h.sniUsecase.CountInbounds(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"used_by": n}})
}

type validateCertRequest struct {
	Certificate string `json:"certificate" binding:"required"`
	PrivateKey  string `json:"private_key"`
	Domain      string `json:"domain"`
}

func (h *Handler) ValidateCertificate(c *gin.Context) {
	var req validateCertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// With a key, validate the full pair + domain coverage; without, fall back
	// to the legacy cert-only check.
	if req.PrivateKey != "" {
		expiry, sanWarning, err := h.sniUsecase.ValidateCertKey(req.Certificate, req.PrivateKey, req.Domain)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "valid": true, "expires_at": expiry, "san_warning": sanWarning})
		return
	}

	expiry, err := h.sniUsecase.ValidateCertificate(req.Certificate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "valid": true, "expires_at": expiry})
}

type issueCertHTTP01Request struct {
	Name   string `json:"name" binding:"required"`
	Domain string `json:"domain" binding:"required"`
}

func (h *Handler) IssueCertHTTP01(c *gin.Context) {
	var req issueCertHTTP01Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	sni, err := h.sniUsecase.IssueCertHTTP01(c.Request.Context(), req.Name, req.Domain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": sni})
}

type startDNS01ChallengeRequest struct {
	Domain string `json:"domain" binding:"required"`
}

func (h *Handler) StartDNS01Challenge(c *gin.Context) {
	var req startDNS01ChallengeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	challenge, err := h.sniUsecase.StartDNS01Challenge(c.Request.Context(), req.Domain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    challenge,
		"message": "Add the TXT record to your DNS, then call /acme/dns01/complete",
	})
}

type completeDNS01ChallengeRequest struct {
	Name   string `json:"name" binding:"required"`
	Domain string `json:"domain" binding:"required"`
}

func (h *Handler) CompleteDNS01Challenge(c *gin.Context) {
	var req completeDNS01ChallengeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	sni, err := h.sniUsecase.CompleteDNS01Challenge(c.Request.Context(), req.Name, req.Domain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": sni})
}

func (h *Handler) RenewCertificate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.sniUsecase.RenewCertificate(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "certificate renewed"})
}

func (h *Handler) GetExpiringCertificates(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))

	certs, err := h.sniUsecase.GetExpiringCertificates(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": certs})
}
