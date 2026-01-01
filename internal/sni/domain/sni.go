package domain

import (
	"time"

	"gorm.io/gorm"
)

// SNI represents a saved TLS Server Name Indication with its certificate
type SNI struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"size:100;not null" json:"name"`               // Display name (e.g., "Main Domain")
	Domain      string `gorm:"size:255;not null;uniqueIndex" json:"domain"` // The actual SNI domain
	Certificate string `gorm:"type:text" json:"certificate"`                // Full PEM certificate content (if UsePathMode=false)
	PrivateKey  string `gorm:"type:text" json:"-"`                          // Full PEM private key — never serialized over the API
	CertPath    string `gorm:"size:512" json:"cert_path"`                   // Path to cert file on server (if UsePathMode=true)
	KeyPath     string `gorm:"size:512" json:"key_path"`                    // Path to key file on server (if UsePathMode=true)
	UsePathMode bool   `gorm:"default:false" json:"use_path_mode"`          // true = use file paths, false = use content
	ALPN        string `gorm:"size:100;default:'h2,http/1.1'" json:"alpn"`  // Default ALPN

	// Auto-issuance tracking (Let's Encrypt / ACME)
	IsAutoIssued  bool      `gorm:"default:false" json:"is_auto_issued"` // true if issued via ACME
	ChallengeType string    `gorm:"size:20" json:"challenge_type"`       // "http-01", "dns-01"
	ExpiresAt     time.Time `json:"expires_at"`                          // Certificate expiry date
	AutoRenew     bool      `gorm:"default:true" json:"auto_renew"`      // Enable auto-renewal
	IssueError    string    `gorm:"size:500" json:"issue_error"`         // Last issuance/renewal error

	// Smallest expiry day-threshold (30/7/1) already alerted to admins; 0 = none.
	// Reset whenever the cert material changes so a renewed cert can alert again.
	ExpiryNotifyLevel int `gorm:"default:0" json:"-"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (SNI) TableName() string {
	return "snis"
}

// GetALPNList returns ALPN as a slice
func (s *SNI) GetALPNList() []string {
	if s.ALPN == "" {
		return []string{"h2", "http/1.1"}
	}
	// Simple split - could enhance later
	result := []string{}
	current := ""
	for _, c := range s.ALPN {
		if c == ',' {
			if current != "" {
				result = append(result, current)
			}
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// MaskPrivateKey returns a masked version of the private key for display
func (s *SNI) MaskPrivateKey() string {
	if len(s.PrivateKey) < 100 {
		return "***INVALID***"
	}
	return s.PrivateKey[:50] + "...[REDACTED]..." + s.PrivateKey[len(s.PrivateKey)-30:]
}
