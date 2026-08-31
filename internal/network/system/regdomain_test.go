package system

import (
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// "00" is the world domain, not a usable AP configuration
func TestRegDomainIsUnset(t *testing.T) {
	for _, code := range []string{"", "00", "  00  ", "0"} {
		if !RegDomainIsUnset(code) {
			t.Errorf("RegDomainIsUnset(%q) = false; %q is not a usable AP regdomain", code, code)
		}
	}
	for _, code := range []string{"IR", "US", "DE", "ir"} {
		if RegDomainIsUnset(code) {
			t.Errorf("RegDomainIsUnset(%q) = true", code)
		}
	}
}

func TestSetRegDomain_RejectsMalformedCodes(t *testing.T) {
	for _, code := range []string{"", "X", "USA", "12", "us-east"} {
		if err := SetRegDomain(nil, code); err == nil {
			t.Errorf("SetRegDomain accepted %q", code)
		}
	}
}

// The predicate is duplicated so domain never imports this package
func TestRegDomainPredicatesAgree(t *testing.T) {
	for _, code := range []string{"", "00", "0", "IR", "US", " de "} {
		if RegDomainIsUnset(code) != domain.RegDomainUnset(code) {
			t.Errorf("predicates disagree on %q", code)
		}
	}
}
