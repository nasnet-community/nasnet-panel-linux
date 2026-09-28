package usecase

import "errors"

var (
	ErrCAProtected         = errors.New("the CA certificate cannot be revoked or deleted")
	ErrCertConfirmMismatch = errors.New("confirmation text does not match")
	ErrCertAgentOutdated   = errors.New("the node agent is too old to rotate its certificate")
	ErrNodeOffline         = errors.New("node is not connected")
	ErrNodeStealth         = errors.New("stealth nodes get a new certificate on every restart")
	ErrStrictModeNotReady  = errors.New("some nodes are not ready for strict mode")
	ErrNoStoredKey         = errors.New("agent keys never leave the node")
)

// CertInstallRejectedError carries the agent's reason for refusing a cert.
type CertInstallRejectedError struct{ Reason string }

func (e *CertInstallRejectedError) Error() string {
	return "node rejected the certificate: " + e.Reason
}

// StrictBlocker is a node that would be refused (or already is) in strict mode.
type StrictBlocker struct {
	NodeID   uint   `json:"node_id"`
	NodeName string `json:"node_name"`
	Reason   string `json:"reason"` // not_seen | presents_revoked | presents_unknown | id_mismatch
}

type StrictModeNotReadyError struct{ Blockers []StrictBlocker }

func (e *StrictModeNotReadyError) Error() string { return ErrStrictModeNotReady.Error() }
func (e *StrictModeNotReadyError) Unwrap() error { return ErrStrictModeNotReady }

// CertErrorCode maps certificate errors to API codes; "" means no code.
func CertErrorCode(err error) string {
	var rejected *CertInstallRejectedError
	switch {
	case errors.Is(err, ErrCertAgentOutdated):
		return "CERT_AGENT_OUTDATED"
	case errors.Is(err, ErrNodeOffline):
		return "NODE_OFFLINE"
	case errors.Is(err, ErrNodeStealth):
		return "NODE_STEALTH"
	case errors.Is(err, ErrLocalNode):
		return "NODE_LOCAL"
	case errors.As(err, &rejected):
		return "CERT_INSTALL_REJECTED"
	case errors.Is(err, ErrStrictModeNotReady):
		return "STRICT_MODE_NOT_READY"
	case errors.Is(err, ErrCAProtected):
		return "CA_PROTECTED"
	case errors.Is(err, ErrCertConfirmMismatch):
		return "CERT_CONFIRM_MISMATCH"
	}
	return ""
}
