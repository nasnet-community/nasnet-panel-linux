package product

import (
	"strings"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// ApplyHostOverrides mutates an InboundDetail by applying every override field
// from the given Host. This is the single source of truth used by every
// subscription/payment/panel path. New host fields must be wired here.
func ApplyHostOverrides(detail *InboundDetail, host *nodeDomain.Host) {
	if detail == nil || host == nil {
		return
	}

	detail.HostID = host.ID
	detail.Priority = host.Priority

	if host.Remark != "" {
		detail.Remark = host.Remark
	} else {
		detail.Remark = DefaultRemarkTemplate
	}
	detail.RemarkIsTemplate = true

	if host.Address != "" {
		detail.NodeIP = host.Address
	}
	if host.Port != nil {
		detail.PublicPort = *host.Port
	}

	// Effective security determines whether SNI/Fingerprint apply to TLS or Reality.
	effSecurity := detail.Security
	if host.Security != "" {
		effSecurity = host.Security
	}

	if host.SNI != "" {
		if effSecurity == "reality" {
			detail.RealitySNI = host.SNI
		} else {
			detail.TLSSni = host.SNI
		}
	}
	if host.Fingerprint != "" {
		if effSecurity == "reality" {
			detail.RealityFingerprint = host.Fingerprint
		} else {
			detail.TLSFingerprint = host.Fingerprint
		}
	}
	if host.ALPN != "" {
		detail.TLSALPN = strings.Split(host.ALPN, ",")
	}

	// Reality key material only means anything on a reality link; applying it
	// elsewhere would put pbk/sid into a TLS config the client then rejects.
	if effSecurity == "reality" {
		if host.RealityPublicKey != "" {
			detail.RealityPublicKey = host.RealityPublicKey
		}
		if host.RealityShortID != "" {
			detail.RealityShortID = host.RealityShortID
		}
		if host.RealitySpiderX != "" {
			detail.RealitySpiderX = host.RealitySpiderX
		}
	}

	if host.Host != "" {
		detail.TransportHost = host.Host
	}
	if host.Path != "" {
		detail.TransportPath = host.Path
		// gRPC stores its identifier in TransportServiceName, not TransportPath.
		// Mirror Path -> ServiceName so a single Host.Path field covers all transports.
		if strings.EqualFold(detail.Network, "grpc") {
			detail.TransportServiceName = host.Path
		}
	}
	// Explicit gRPC serviceName wins over the Path mirror above.
	if host.ServiceName != "" {
		detail.TransportServiceName = host.ServiceName
	}
	if host.Mode != "" {
		detail.TransportMode = host.Mode
	}
	if host.HeaderType != "" {
		detail.TransportHeaderType = host.HeaderType
	}

	// Protocol-scoped overrides. Gated on the inbound's protocol so a host
	// reused across inbounds can't inject vless flow into a trojan link.
	switch strings.ToLower(detail.Protocol) {
	case "vless":
		if host.Flow != "" {
			detail.VLESSFlow = host.Flow
		}
		if host.Encryption != "" {
			detail.VLESSEncryption = host.Encryption
		}
	case "vmess":
		if host.VMessSecurity != "" {
			detail.VMessSecurity = host.VMessSecurity
		}
	case "hysteria2", "hysteria":
		if host.ObfsPassword != "" {
			detail.HysteriaObfsPassword = host.ObfsPassword
		}
		if host.PortRange != "" {
			detail.PortRange = host.PortRange
		}
	}

	if host.Security != "" {
		detail.Security = host.Security
	}
	if host.AllowInsecure != nil {
		detail.AllowInsecure = host.AllowInsecure
	}

	if host.FragmentSettings != nil {
		detail.Fragment = &FragmentInfo{
			Packets:  host.FragmentSettings.Packets,
			Length:   host.FragmentSettings.Length,
			Interval: host.FragmentSettings.Interval,
		}
	}
}
