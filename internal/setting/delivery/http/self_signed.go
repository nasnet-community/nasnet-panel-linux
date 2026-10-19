package http

import (
	"context"
	"crypto/x509"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/audit"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/selfsigned"
)

// SelfSignedCert is the panel's own HTTPS certificate.
type SelfSignedCert interface {
	// Active reports whether the panel is serving it.
	Active() bool
	Regenerate(ctx context.Context) (old, cur *x509.Certificate, err error)
}

func (h *SettingHandler) SetSelfSigned(c SelfSignedCert, audit auditDomain.AuditLogUsecase) {
	h.selfSigned, h.audit = c, audit
}

// RegenerateCertificate replaces the self-signed certificate with one named
// after the machine's addresses now. It is served from the next connection
// on; every browser that accepted the old one asks again.
func (h *SettingHandler) RegenerateCertificate(c *gin.Context) {
	if h.selfSigned == nil || !h.selfSigned.Active() {
		c.JSON(http.StatusConflict, gin.H{"error": "The panel isn't serving a self-signed certificate"})
		return
	}
	old, cur, err := h.selfSigned.Regenerate(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Couldn't create a new certificate", "details": err.Error()})
		return
	}
	if h.audit != nil {
		ac := audit.FromGinContext(c)
		h.audit.Log(c.Request.Context(), &auditDomain.AuditLog{
			Action:     string(auditDomain.AuditPanelCertificate),
			ActorID:    ac.ActorID,
			ActorName:  ac.ActorName,
			EntityType: "setting",
			OldValues:  selfsigned.AuditJSON(old, ""),
			NewValues:  selfsigned.AuditJSON(cur, "regenerated"),
			IPAddress:  ac.IPAddress,
			RequestID:  ac.RequestID,
			Source:     "http",
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": describeCert(cur)})
}
