package conversation

import "time"

// ConversationState represents the current state in a conversation flow
type ConversationState string

const (
	// General states
	StateIdle ConversationState = "idle"

	// Add Node (Server) Wizard
	StateAddNodeName       ConversationState = "addnode_name"
	StateAddNodeIP         ConversationState = "addnode_ip"
	StateAddNodeCountry    ConversationState = "addnode_country"
	StateAddNodeDatacenter ConversationState = "addnode_dc"
	StateAddNodeAPIPort    ConversationState = "addnode_apiport"

	// Add Inbound Wizard (Enhanced)
	StateAddInboundTag         ConversationState = "addinbound_tag"
	StateAddInboundProtocol    ConversationState = "addinbound_proto"
	StateAddInboundPort        ConversationState = "addinbound_port"
	StateAddInboundNetwork     ConversationState = "addinbound_network"        // Network type selection
	StateAddInboundTransport   ConversationState = "addinbound_transport"      // Path/Host/ServiceName
	StateAddInboundTransportH  ConversationState = "addinbound_transport_host" // Host input after path
	StateAddInboundSecurity    ConversationState = "addinbound_security"
	StateAddInboundRealityDest ConversationState = "addinbound_reality_dest"
	StateAddInboundRealitySNI  ConversationState = "addinbound_reality_sni"
	StateAddInboundRemark      ConversationState = "addinbound_remark"
	StateAddInboundFormat      ConversationState = "addinbound_format"

	// Edit Inbound states - Basic Info
	StateEditInboundRemark  ConversationState = "editinbound_remark"
	StateEditInboundAddress ConversationState = "editinbound_address"
	StateEditInboundPort    ConversationState = "editinbound_port"

	// Edit Inbound states - Network Settings
	StateEditInboundNetwork ConversationState = "editinbound_network"
	StateEditInboundPath    ConversationState = "editinbound_path"
	StateEditInboundHost    ConversationState = "editinbound_host"
	StateEditInboundService ConversationState = "editinbound_service"
	StateEditInboundMode    ConversationState = "editinbound_mode" // XHTTP Mode

	// Edit Inbound states - Security Settings
	StateEditInboundSecurity    ConversationState = "editinbound_security"
	StateEditInboundSNI         ConversationState = "editinbound_sni"
	StateEditInboundFingerprint ConversationState = "editinbound_fp"
	StateEditInboundALPN        ConversationState = "editinbound_alpn"
	StateEditInboundCerts       ConversationState = "editinbound_certs"

	// Edit Inbound states - Link Format
	StateEditInboundLinkFormat ConversationState = "editinbound_linkformat"

	// Add Inbound TLS Flow (new)
	StateAddInboundTLSSNI  ConversationState = "addinbound_tls_sni"
	StateAddInboundTLSALPN ConversationState = "addinbound_tls_alpn"

	// Advanced JSON Editing (new)
	StateEditInboundAdvancedTLS       ConversationState = "editinbound_adv_tls"
	StateEditInboundAdvancedTransport ConversationState = "editinbound_adv_transport"
	StateEditInboundAdvancedSniffing  ConversationState = "editinbound_adv_sniffing"

	// SNI Certificate Management states
	StateAddSNIName     ConversationState = "addsni_name"
	StateAddSNIDomain   ConversationState = "addsni_domain"
	StateAddSNICert     ConversationState = "addsni_cert"
	StateAddSNIKey      ConversationState = "addsni_key"
	StateAddSNIALPN     ConversationState = "addsni_alpn"
	StateAddSNICertPath ConversationState = "addsni_certpath" // For file path mode
	StateAddSNIKeyPath  ConversationState = "addsni_keypath"  // For file path mode

	// Edit SNI states
	StateEditSNIName   ConversationState = "editsni_name"
	StateEditSNIDomain ConversationState = "editsni_domain"
	StateEditSNICert   ConversationState = "editsni_cert"
	StateEditSNIKey    ConversationState = "editsni_key"

	// Admin Set Data conversation states
	StateAdminSetDataValue ConversationState = "admin_set_data_value"

	// Admin Subscription Management states
	StateAdminSetDataLimitValue ConversationState = "admin_set_data_limit_value"
	StateAdminAddDataValue      ConversationState = "admin_add_data_value"
	StateAdminSetEndDateValue   ConversationState = "admin_set_end_date_value"

	// Admin Custom Extension state
	StateAdminExtendCustom ConversationState = "admin_extend_custom"

	// Broadcast conversation states
	StateBroadcastMessage ConversationState = "broadcast_message"
	StateBroadcastConfirm ConversationState = "broadcast_confirm"

	// User lookup states
	StateUserLookup ConversationState = "user_lookup"

	// Subscription Management states
	StateRenameSubscription ConversationState = "sub_rename"

	// Add Outbound Wizard
	StateAddOutboundTag       ConversationState = "addoutbound_tag"
	StateAddOutboundProtocol  ConversationState = "addoutbound_protocol"
	StateAddOutboundAddress   ConversationState = "addoutbound_address"
	StateAddOutboundPort      ConversationState = "addoutbound_port"
	StateAddOutboundUUID      ConversationState = "addoutbound_uuid"
	StateAddOutboundNetwork   ConversationState = "addoutbound_network"
	StateAddOutboundTransport ConversationState = "addoutbound_transport"
	StateAddOutboundSecurity  ConversationState = "addoutbound_security"
	StateAddOutboundTLSSNI    ConversationState = "addoutbound_tls_sni"
	StateAddOutboundRemark    ConversationState = "addoutbound_remark"

	// Add Outbound Wizard Extra
	StateAddOutboundPassword   ConversationState = "addoutbound_password"
	StateAddOutboundUsername   ConversationState = "addoutbound_username"
	StateAddOutboundMethod     ConversationState = "addoutbound_method"
	StateAddOutboundImportLink ConversationState = "addoutbound_import_link"

	// Edit Outbound states
	StateEditOutboundTag      ConversationState = "editoutbound_tag"
	StateEditOutboundAddress  ConversationState = "editoutbound_address"
	StateEditOutboundPort     ConversationState = "editoutbound_port"
	StateEditOutboundUUID     ConversationState = "editoutbound_uuid"
	StateEditOutboundNetwork  ConversationState = "editoutbound_network"
	StateEditOutboundSecurity ConversationState = "editoutbound_security"
	StateEditOutboundRemark   ConversationState = "editoutbound_remark"
	StateEditOutboundProtocol ConversationState = "editoutbound_protocol"
	StateEditOutboundUsername ConversationState = "editoutbound_username"
	StateEditOutboundPassword ConversationState = "editoutbound_password"
	StateEditOutboundFlow     ConversationState = "editoutbound_flow"
	StateEditOutboundMethod   ConversationState = "editoutbound_method"

	// Advanced Outbound States
	StateEditOutboundLevel      ConversationState = "editoutbound_level"
	StateEditOutboundEmail      ConversationState = "editoutbound_email"
	StateEditOutboundInterface  ConversationState = "editoutbound_interface"
	StateEditOutboundMark       ConversationState = "editoutbound_mark"
	StateEditOutboundEncryption ConversationState = "editoutbound_encryption"

	// Add Routing Rule states
	StateAddRuleTag      ConversationState = "addrule_tag"
	StateAddRuleTarget   ConversationState = "addrule_target"
	StateAddRulePriority ConversationState = "addrule_priority"
	StateAddRuleDomains  ConversationState = "addrule_domains"
	StateAddRuleGeoIP    ConversationState = "addrule_geoip"
	StateAddRuleIPs      ConversationState = "addrule_ips"
	StateAddRulePorts    ConversationState = "addrule_ports"
	StateAddRuleNetworks ConversationState = "addrule_networks"
	StateAddRuleProtocol ConversationState = "addrule_protocol"
	StateAddRuleInbounds ConversationState = "addrule_inbounds"
	StateAddRuleUsers    ConversationState = "addrule_users"
	StateAddRuleRemark   ConversationState = "addrule_remark"

	// Edit Routing Rule states
	StateEditRuleRemark   ConversationState = "editrule_remark"
	StateEditRuleDomains  ConversationState = "editrule_domains"
	StateEditRuleGeoIP    ConversationState = "editrule_geoip"
	StateEditRuleIPs      ConversationState = "editrule_ips"
	StateEditRulePorts    ConversationState = "editrule_ports"
	StateEditRuleNetworks ConversationState = "editrule_networks"
	StateEditRuleProtocol ConversationState = "editrule_protocol"
	StateEditRuleInbounds ConversationState = "editrule_inbounds"
	StateEditRuleUsers    ConversationState = "editrule_users"
	StateEditRuleTarget   ConversationState = "editrule_target"
	StateEditRulePriority ConversationState = "editrule_priority"

	// ACME Certificate Issuance states
	StateIssueCertDomain  ConversationState = "issuecert_domain"
	StateIssueCertName    ConversationState = "issuecert_name"
	StateIssueDNS01Verify ConversationState = "issuedns01_verify"

	// Manual User Management
	StateManualAddUserEmail ConversationState = "manual_adduser_email"
	StateManualGetLinkEmail ConversationState = "manual_getlink_email"
	StateManualGetLinkUUID  ConversationState = "manual_getlink_uuid"
)

// SessionTimeout is the default timeout for conversation sessions
const SessionTimeout = 20 * time.Minute

// UserSession holds the current conversation state and data for a user
type UserSession struct {
	UserID      int64
	State       ConversationState
	Data        map[string]interface{}
	CreatedAt   time.Time
	ExpiresAt   time.Time
	JustExpired bool
}

// NewSession creates a new idle session for a user
func NewSession(userID int64) *UserSession {
	now := time.Now()
	return &UserSession{
		UserID:    userID,
		State:     StateIdle,
		Data:      make(map[string]interface{}),
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTimeout),
	}
}

// IsExpired checks if the session has expired
func (s *UserSession) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// Reset resets the session to idle state
func (s *UserSession) Reset() {
	s.State = StateIdle
	s.Data = make(map[string]interface{})
	s.ExpiresAt = time.Now().Add(SessionTimeout)
	s.JustExpired = false
}

// SetState updates the state and refreshes expiration
func (s *UserSession) SetState(state ConversationState) {
	s.State = state
	s.ExpiresAt = time.Now().Add(SessionTimeout)
}

// Set stores a value in session data
func (s *UserSession) Set(key string, value interface{}) {
	s.Data[key] = value
	s.ExpiresAt = time.Now().Add(SessionTimeout)
}

// Get retrieves a value from session data
func (s *UserSession) Get(key string) (interface{}, bool) {
	val, ok := s.Data[key]
	return val, ok
}

// GetString retrieves a string value from session data
func (s *UserSession) GetString(key string) string {
	if val, ok := s.Data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// GetFloat retrieves a float64 value from session data
func (s *UserSession) GetFloat(key string) float64 {
	if val, ok := s.Data[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case int64:
			return float64(v)
		}
	}
	return 0
}

// GetInt retrieves an int value from session data
func (s *UserSession) GetInt(key string) int {
	if val, ok := s.Data[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case uint:
			return int(v)
		case uint32:
			return int(v)
		case uint64:
			return int(v)
		}
	}
	return 0
}

// GetUint retrieves a uint value from session data
func (s *UserSession) GetUint(key string) uint {
	return uint(s.GetInt(key))
}
