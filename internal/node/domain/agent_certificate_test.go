package domain

import (
	"testing"
	"time"
)

func TestAgentCertificate_IsExpired(t *testing.T) {
	past := &AgentCertificate{NotAfter: time.Now().Add(-time.Hour)}
	if !past.IsExpired() {
		t.Error("cert past NotAfter should be expired")
	}
	future := &AgentCertificate{NotAfter: time.Now().Add(time.Hour)}
	if future.IsExpired() {
		t.Error("cert before NotAfter should not be expired")
	}
}

func TestAgentCertificate_IsValid(t *testing.T) {
	now := time.Now()
	valid := &AgentCertificate{NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	if !valid.IsValid() {
		t.Error("cert inside its window should be valid")
	}

	tests := []struct {
		name string
		cert *AgentCertificate
	}{
		{"revoked", &AgentCertificate{IsRevoked: true, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}},
		{"not yet active", &AgentCertificate{NotBefore: now.Add(time.Hour), NotAfter: now.Add(2 * time.Hour)}},
		{"already expired", &AgentCertificate{NotBefore: now.Add(-2 * time.Hour), NotAfter: now.Add(-time.Hour)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cert.IsValid() {
				t.Errorf("%s cert should not be valid", tt.name)
			}
		})
	}
}

func TestAgentCertificate_DaysUntilExpiry(t *testing.T) {
	// ~30 days out; integer-hours division lands on 29 or 30.
	c := &AgentCertificate{NotAfter: time.Now().Add(30 * 24 * time.Hour)}
	if d := c.DaysUntilExpiry(); d < 29 || d > 30 {
		t.Errorf("DaysUntilExpiry() = %d, want ~30", d)
	}
	expired := &AgentCertificate{NotAfter: time.Now().Add(-48 * time.Hour)}
	if d := expired.DaysUntilExpiry(); d >= 0 {
		t.Errorf("expired cert should report negative days, got %d", d)
	}
}
