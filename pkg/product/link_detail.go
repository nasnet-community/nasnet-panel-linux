package product

import (
	"fmt"
	"strings"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// LinkDetail maps an inbound on node to the settings its client links carry.
// Subscriptions add agent credentials on top; reverse tunnels derive the
// bridge's outbound from it, so the bridge dials exactly like a customer.
func LinkDetail(in *nodeDomain.Inbound, node *nodeDomain.Node) InboundDetail {
	nodeIP := node.IP
	if in.Address != "" {
		nodeIP = in.Address
	}

	detail := InboundDetail{
		NodeID:      node.ID,
		Tag:         in.Tag,
		Protocol:    in.Protocol,
		LinkFormat:  in.LinkFormat,
		NodeIP:      nodeIP,
		ProvisionIP: node.IP, // Always the real node IP for provisioning
		APIPort:     node.APIPort,
		PublicPort:  in.Port,
		Remark:      fmt.Sprintf("%s - %s", node.Name, in.Remark),
		NodeName:    node.Name,
		CountryCode: node.CountryCode,
		Network:     in.Network,
		Security:    in.Security,
		PortRange:   in.PortRange,
		AgentPort:   node.AgentPort,
	}

	if tlsSettings := in.GetTLSSettingsOrDefault(); tlsSettings != nil {
		detail.TLSSni = tlsSettings.ServerName
		detail.TLSALPN = tlsSettings.ALPN
		detail.TLSFingerprint = tlsSettings.Fingerprint
	}

	if realitySettings := in.GetRealitySettingsOrDefault(); realitySettings != nil {
		detail.RealityPublicKey = realitySettings.PublicKey
		detail.RealityShortID = realitySettings.ShortID
		if len(realitySettings.ServerNames) > 0 {
			detail.RealitySNI = realitySettings.ServerNames[0]
		}
		detail.RealityFingerprint = realitySettings.Fingerprint
		detail.RealitySpiderX = realitySettings.SpiderX
	}

	if transportSettings := in.GetTransportSettingsOrDefault(); transportSettings != nil {
		detail.TransportPath = transportSettings.Path
		detail.TransportHost = transportSettings.Host
		detail.TransportServiceName = transportSettings.ServiceName
		detail.TransportHeaderType = transportSettings.HeaderType
		detail.TransportMode = transportSettings.Mode
	}

	// VLESS settings (for MLKEM encryption)
	if strings.ToLower(in.Protocol) == "vless" {
		if vlessSettings := in.GetVLESSSettingsOrDefault(); vlessSettings != nil {
			detail.VLESSFlow = vlessSettings.Flow
			detail.VLESSEncryption = vlessSettings.Encryption
			detail.VLESSDecryption = vlessSettings.Decryption
		}
	}

	// VMess settings (for AlterId / scy in URI)
	if strings.ToLower(in.Protocol) == "vmess" {
		if vmessSettings := in.GetVMessSettingsOrDefault(); vmessSettings != nil {
			if vmessSettings.AlterId > 0 {
				detail.VMessAlterId = uint32(vmessSettings.AlterId)
			}
			detail.VMessSecurity = vmessSettings.Security
		}
	}

	return detail
}
