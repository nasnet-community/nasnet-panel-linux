package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/conversation"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
	"gopkg.in/telebot.v3"
)

// === Wizard: Add Inbound (Enhanced) ===

func (h *Handler) HandleAddInboundStart(c telebot.Context) error {
	utils.AnswerCallback(c)
	nodeID := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateAddInboundTag)
	h.stateManager.SetData(userID, "node_id", nodeID)

	return c.Send("➕ *Add Inbound*\n\nStep 1/6: Enter *Inbound Tag* (unique ID, e.g. `vless-reality-1`):", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddInboundTag(c telebot.Context) error {
	userID := c.Sender().ID
	tag := c.Text()
	h.stateManager.SetData(userID, "tag", tag)
	h.stateManager.SetState(userID, conversation.StateAddInboundProtocol)
	return c.Send("Step 2/6: Enter *Protocol* (vless, vmess, trojan):", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddInboundProtocol(c telebot.Context) error {
	userID := c.Sender().ID
	proto := strings.ToLower(c.Text())
	h.stateManager.SetData(userID, "proto", proto)
	h.stateManager.SetState(userID, conversation.StateAddInboundPort)
	return c.Send("Step 3/6: Enter *Public Port* (e.g. 443):", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddInboundPort(c telebot.Context) error {
	userID := c.Sender().ID
	port, err := strconv.Atoi(c.Text())
	if err != nil || port < 1 || port > 65535 {
		return c.Send("❌ Invalid port. Must be a number between 1 and 65535:")
	}
	h.stateManager.SetData(userID, "port", port)
	h.stateManager.SetState(userID, conversation.StateAddInboundNetwork)

	msg := "Step 4/7: Choose *Network/Transport* Type:\n\n" +
		"• *TCP*: Direct connection (default for Reality)\n" +
		"• *WebSocket*: CDN-friendly, requires path\n" +
		"• *gRPC*: High performance, requires service name\n" +
		"• *XHTTP*: Modern CDN-friendly (SplitHTTP)"

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("🔌 TCP", "inbound_net_tcp")),
		kb.Row(kb.Data("🌐 WebSocket", "inbound_net_ws")),
		kb.Row(kb.Data("⚡ gRPC", "inbound_net_grpc")),
		kb.Row(kb.Data("🚀 XHTTP", "inbound_net_xhttp")),
	)

	return c.Send(msg, telebot.ModeMarkdown, kb)
}

// HandleInboundNetworkCallback handles network type selection
func (h *Handler) HandleInboundNetworkCallback(c telebot.Context) error {
	utils.AnswerCallback(c)
	netType := strings.TrimPrefix(c.Data(), "inbound_net_") // tcp, ws, grpc, xhttp
	userID := c.Sender().ID

	h.stateManager.SetData(userID, "network", netType)

	switch netType {
	case "ws":
		h.stateManager.SetState(userID, conversation.StateAddInboundTransport)
		return c.Edit("🌐 *WebSocket Config*\n\nEnter **Path** (e.g. `/ws`):", telebot.ModeMarkdown)
	case "grpc":
		h.stateManager.SetState(userID, conversation.StateAddInboundTransport)
		return c.Edit("⚡ *gRPC Config*\n\nEnter **Service Name** (e.g. `grpc-svc`):", telebot.ModeMarkdown)
	case "xhttp":
		h.stateManager.SetState(userID, conversation.StateAddInboundTransport)
		return c.Edit("🚀 *XHTTP Config*\n\nEnter **Path** (e.g. `/download`):", telebot.ModeMarkdown)
	default: // tcp
		return h.showSecuritySelection(c)
	}
}

// HandleAddInboundTransport handles path/serviceName input based on network
func (h *Handler) HandleAddInboundTransport(c telebot.Context) error {
	userID := c.Sender().ID
	network := h.stateManager.GetStringData(userID, "network")
	input := c.Text()

	switch network {
	case "ws":
		h.stateManager.SetData(userID, "transport_path", input)
		h.stateManager.SetState(userID, conversation.StateAddInboundTransportH)
		return c.Send("Enter **Host** header (or send `-` to skip):", telebot.ModeMarkdown, keyboards.Cancel())
	case "grpc":
		h.stateManager.SetData(userID, "transport_service", input)
		return h.showSecuritySelection(c)
	case "xhttp":
		h.stateManager.SetData(userID, "transport_path", input)
		h.stateManager.SetState(userID, conversation.StateAddInboundTransportH)
		return c.Send("Enter **Host** header (or send `-` to skip):", telebot.ModeMarkdown, keyboards.Cancel())
	}
	return nil
}

// HandleAddInboundTransportHost handles host input for WS/XHTTP
func (h *Handler) HandleAddInboundTransportHost(c telebot.Context) error {
	userID := c.Sender().ID
	network := h.stateManager.GetStringData(userID, "network")
	input := c.Text()

	if input != "-" {
		h.stateManager.SetData(userID, "transport_host", input)
	}

	if network == "xhttp" {
		return h.showXHTTPModeSelection(c)
	}
	return h.showSecuritySelection(c)
}

func (h *Handler) showXHTTPModeSelection(c telebot.Context) error {
	msg := "🚀 *XHTTP Mode*\n\n" +
		"• *packet-up*: Best for CDN (recommended)\n" +
		"• *stream-up*: Raw streaming\n" +
		"• *auto*: Auto-detect"

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("📦 packet-up (CDN)", "inbound_xhttp_packet-up")),
		kb.Row(kb.Data("📡 stream-up", "inbound_xhttp_stream-up")),
		kb.Row(kb.Data("🔄 auto", "inbound_xhttp_auto")),
	)
	return c.Send(msg, telebot.ModeMarkdown, kb)
}

// HandleXHTTPModeCallback handles XHTTP mode selection
func (h *Handler) HandleXHTTPModeCallback(c telebot.Context) error {
	utils.AnswerCallback(c)
	mode := strings.TrimPrefix(c.Data(), "inbound_xhttp_")
	userID := c.Sender().ID

	h.stateManager.SetData(userID, "xhttp_mode", mode)
	return h.showSecuritySelection(c)
}

func (h *Handler) showSecuritySelection(c telebot.Context) error {
	userID := c.Sender().ID
	h.stateManager.SetState(userID, conversation.StateAddInboundSecurity)

	msg := "Step 5/7: Choose *Security* Type:\n\n" +
		"• *Reality*: Best for stealth (VLESS/Trojan)\n" +
		"• *None*: Standard (no encryption layer)\n" +
		"• *TLS*: Standard TLS (requires certs)"

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("🛡 Reality", "inbound_sec_reality")),
		kb.Row(kb.Data("🔓 None", "inbound_sec_none")),
		kb.Row(kb.Data("🔒 TLS", "inbound_sec_tls")),
	)

	return c.Send(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleInboundSecurityCallback(c telebot.Context) error {
	utils.AnswerCallback(c)
	secType := strings.TrimPrefix(c.Data(), "inbound_sec_") // reality, none, tls
	userID := c.Sender().ID

	h.stateManager.SetData(userID, "security", secType)

	if secType == "reality" {
		h.stateManager.SetState(userID, conversation.StateAddInboundRealityDest)
		return c.Edit("🛡 *Reality Config*\n\nEnter **Dest** (Fallback site, e.g. `www.google.com:443`):", telebot.ModeMarkdown)
	}

	if secType == "tls" {
		// Show saved SNIs/certificates as options
		ctx, cancel := tgctx.FromTelebot(c)
		defer cancel()
		snis, err := h.sniUsecase.List(ctx)

		if err == nil && len(snis) > 0 {
			kb := &telebot.ReplyMarkup{}
			var rows []telebot.Row
			for _, sni := range snis {
				rows = append(rows, kb.Row(kb.Data(fmt.Sprintf("🌐 %s (%s)", sni.Name, sni.Domain), "inbound_use_sni", fmt.Sprintf("%d", sni.ID))))
			}
			rows = append(rows, kb.Row(kb.Data("➕ Enter Manually", "inbound_tls_manual")))
			rows = append(rows, kb.Row(kb.Data("❌ Cancel", "admin")))
			kb.Inline(rows...)

			return c.Edit("🔐 *Select TLS Certificate*\n\nChoose a saved certificate or enter manually:", telebot.ModeMarkdown, kb)
		}

		// No saved SNIs, go to manual entry
		h.stateManager.SetState(userID, conversation.StateAddInboundTLSSNI)
		return c.Edit("🔒 *TLS Config*\n\n_No saved certificates. Please enter manually._\n\nEnter **SNI** (Server Name, e.g. `example.com`):", telebot.ModeMarkdown)
	}

	h.stateManager.SetState(userID, conversation.StateAddInboundRemark)
	return c.Edit("Step 6/7: Enter *Remark/Label* (e.g. '🇩🇪 High Speed'):", telebot.ModeMarkdown)
}

// HandleUseSNI uses a saved SNI for the inbound
func (h *Handler) HandleUseSNI(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	userID := c.Sender().ID

	id, err := strconv.ParseUint(c.Data(), 10, 32)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Invalid SNI ID"})
	}

	sni, err := h.sniUsecase.GetByID(ctx, uint(id))
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "SNI not found"})
	}

	// Reference the saved SNI by id; the cert is resolved live at push time, so
	// no cert/key content is copied into the inbound.
	h.stateManager.SetData(userID, "tls_sni", sni.Domain)
	h.stateManager.SetData(userID, "tls_alpn", sni.ALPN)
	h.stateManager.SetData(userID, "sni_id", fmt.Sprintf("%d", sni.ID))

	// Auto-set host to SNI domain (can be overridden later in Network Settings)
	h.stateManager.SetData(userID, "transport_host", sni.Domain)

	h.stateManager.SetState(userID, conversation.StateAddInboundRemark)
	return c.Edit(fmt.Sprintf("✅ Using certificate: **%s** (`%s`)\n_Host automatically set to `%s`_\n\nStep 6/7: Enter *Remark/Label* (e.g. '🇩🇪 High Speed'):", sni.Name, sni.Domain, sni.Domain), telebot.ModeMarkdown)
}

// HandleTLSManual continues with manual TLS entry
func (h *Handler) HandleTLSManual(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID := c.Sender().ID
	h.stateManager.SetState(userID, conversation.StateAddInboundTLSSNI)
	return c.Edit("🔒 *TLS Config*\n\nEnter **SNI** (Server Name, e.g. `example.com`):", telebot.ModeMarkdown)
}

func (h *Handler) HandleAddInboundRealityDest(c telebot.Context) error {
	userID := c.Sender().ID
	dest := c.Text()
	h.stateManager.SetData(userID, "reality_dest", dest)
	h.stateManager.SetState(userID, conversation.StateAddInboundRealitySNI)
	return c.Send("Enter **SNI** (Server Name, e.g. `www.google.com`):", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddInboundRealitySNI(c telebot.Context) error {
	userID := c.Sender().ID
	sni := c.Text()

	// Auto-generate keys
	keys, err := xray.GenerateX25519Keys()
	if err != nil {
		return c.Send("❌ Key generation failed: " + err.Error())
	}
	shortID, _ := xray.GenerateShortID(16) // 8 bytes

	h.stateManager.SetData(userID, "reality_sni", sni)
	h.stateManager.SetData(userID, "reality_pk", keys.PrivateKey)
	h.stateManager.SetData(userID, "reality_pub", keys.PublicKey)
	h.stateManager.SetData(userID, "reality_short", shortID)

	msg := fmt.Sprintf("✅ *Reality Keys Generated*\n\n"+
		"🔑 **Private:** `%s`\n"+
		"📢 **Public:** `%s`\n"+
		"🆔 **ShortId:** `%s`\n\n"+
		"Moving to final step...", keys.PrivateKey, keys.PublicKey, shortID)
	c.Send(msg, telebot.ModeMarkdown)

	h.stateManager.SetState(userID, conversation.StateAddInboundRemark)
	return c.Send("Step 6/7: Enter *Remark/Label*:", telebot.ModeMarkdown, keyboards.Cancel())
}

// HandleAddInboundTLSSNI handles TLS SNI input
func (h *Handler) HandleAddInboundTLSSNI(c telebot.Context) error {
	userID := c.Sender().ID
	sni := c.Text()
	h.stateManager.SetData(userID, "tls_sni", sni)
	h.stateManager.SetState(userID, conversation.StateAddInboundTLSALPN)
	return c.Send("Enter **ALPN** (comma-separated, e.g. `h2,http/1.1`) or `-` for default:", telebot.ModeMarkdown, keyboards.Cancel())
}

// HandleAddInboundTLSALPN handles TLS ALPN input
func (h *Handler) HandleAddInboundTLSALPN(c telebot.Context) error {
	userID := c.Sender().ID
	alpn := c.Text()
	if alpn != "-" && alpn != "" {
		h.stateManager.SetData(userID, "tls_alpn", alpn)
	} else {
		h.stateManager.SetData(userID, "tls_alpn", "h2,http/1.1")
	}
	h.stateManager.SetState(userID, conversation.StateAddInboundRemark)
	return c.Send("Step 6/7: Enter *Remark/Label* (e.g. '🇩🇪 High Speed'):", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddInboundRemark(c telebot.Context) error {
	userID := c.Sender().ID
	remark := c.Text()
	h.stateManager.SetData(userID, "remark", remark)

	proto := h.stateManager.GetStringData(userID, "proto")
	sec := h.stateManager.GetStringData(userID, "security")
	network := h.stateManager.GetStringData(userID, "network")
	if network == "" {
		network = "xhttp"
	}

	defaultFormat := ""
	if sec == "reality" {
		pub := h.stateManager.GetStringData(userID, "reality_pub")
		sni := h.stateManager.GetStringData(userID, "reality_sni")
		sid := h.stateManager.GetStringData(userID, "reality_short")
		defaultFormat = fmt.Sprintf("vless://{uuid}@{host}:{port}?security=reality&sni=%s&fp=chrome&pbk=%s&sid=%s&type=%s&flow=xtls-rprx-vision#{name}", sni, pub, sid, network)
	} else {
		// Build query params based on network
		params := fmt.Sprintf("type=%s", network)
		switch network {
		case "ws":
			path := h.stateManager.GetStringData(userID, "transport_path")
			host := h.stateManager.GetStringData(userID, "transport_host")
			if path != "" {
				params += "&path=" + path
			}
			if host != "" {
				params += "&host=" + host
			}
		case "grpc":
			svc := h.stateManager.GetStringData(userID, "transport_service")
			if svc != "" {
				params += "&serviceName=" + svc
			}
		case "xhttp":
			path := h.stateManager.GetStringData(userID, "transport_path")
			host := h.stateManager.GetStringData(userID, "transport_host")
			mode := h.stateManager.GetStringData(userID, "xhttp_mode")
			if path != "" {
				params += "&path=" + path
			}
			if host != "" {
				params += "&host=" + host
			}
			if mode != "" {
				params += "&mode=" + mode
			}
		}
		if sec == "tls" {
			params += "&security=tls"
			sni := h.stateManager.GetStringData(userID, "tls_sni")
			if sni != "" {
				params += "&sni=" + sni
			}
		} else if sec != "" && sec != "none" {
			params += "&security=" + sec
		}
		defaultFormat = fmt.Sprintf("%s://{uuid}@{host}:{port}?%s#{name}", proto, params)
	}

	h.stateManager.SetState(userID, conversation.StateAddInboundFormat)
	h.stateManager.SetData(userID, "link_format", defaultFormat)

	msg := fmt.Sprintf("Step 7/7: *Link Format Template*\n\n"+
		"Suggested:\n`%s`\n\n"+
		"Reply with a new format OR click **Use Suggested**.", defaultFormat)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("✅ Use Suggested", "inbound_use_format")))

	return c.Send(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleAddInboundFormat(c telebot.Context) error {
	return h.finishAddInbound(c, c.Text())
}

func (h *Handler) HandleUseSuggestedFormat(c telebot.Context) error {
	userID := c.Sender().ID
	format := h.stateManager.GetStringData(userID, "link_format")
	return h.finishAddInbound(c, format)
}

func (h *Handler) finishAddInbound(c telebot.Context, format string) error {
	userID := c.Sender().ID
	nodeIDStr := h.stateManager.GetStringData(userID, "node_id")
	nodeID, _ := strconv.Atoi(nodeIDStr)

	tag := h.stateManager.GetStringData(userID, "tag")
	proto := h.stateManager.GetStringData(userID, "proto")
	port := h.stateManager.GetIntData(userID, "port")
	remark := h.stateManager.GetStringData(userID, "remark")
	security := h.stateManager.GetStringData(userID, "security")
	network := h.stateManager.GetStringData(userID, "network")
	if network == "" {
		network = "xhttp"
	}

	inbound := &domain.Inbound{
		NodeID:     uint(nodeID),
		Tag:        tag,
		Protocol:   proto,
		Port:       port,
		Remark:     remark,
		LinkFormat: format,
		Network:    network,
		Security:   security,
		Listen:     "0.0.0.0",
	}

	// Transport Settings
	if network != "tcp" {
		transport := &domain.TransportSettings{}
		switch network {
		case "ws":
			transport.Path = h.stateManager.GetStringData(userID, "transport_path")
			if transport.Path == "" {
				transport.Path = "/" // Default path for ws
			}
			transport.Host = h.stateManager.GetStringData(userID, "transport_host")
		case "grpc":
			transport.ServiceName = h.stateManager.GetStringData(userID, "transport_service")
			if transport.ServiceName == "" {
				transport.ServiceName = "grpc" // Default service name
			}
		case "xhttp":
			transport.Path = h.stateManager.GetStringData(userID, "transport_path")
			if transport.Path == "" {
				transport.Path = "/" // Default path for xhttp
			}
			transport.Host = h.stateManager.GetStringData(userID, "transport_host")
			transport.Mode = h.stateManager.GetStringData(userID, "xhttp_mode")
			if transport.Mode == "" {
				transport.Mode = "auto" // Default mode for xhttp
			}
		case "httpupgrade":
			transport.Path = h.stateManager.GetStringData(userID, "transport_path")
			if transport.Path == "" {
				transport.Path = "/" // Default path
			}
			transport.Host = h.stateManager.GetStringData(userID, "transport_host")
		}
		inbound.TransportSettings = transport
	}

	if security == "reality" {
		realSettings := &domain.RealitySettings{
			Show:        false,
			Dest:        h.stateManager.GetStringData(userID, "reality_dest"),
			ServerNames: []string{h.stateManager.GetStringData(userID, "reality_sni")},
			PrivateKey:  h.stateManager.GetStringData(userID, "reality_pk"),
			PublicKey:   h.stateManager.GetStringData(userID, "reality_pub"),
			ShortID:     h.stateManager.GetStringData(userID, "reality_short"),
			Fingerprint: "chrome",
		}
		inbound.RealitySettings = realSettings
	}

	if security == "tls" {
		alpnStr := h.stateManager.GetStringData(userID, "tls_alpn")
		var alpnList []string
		if alpnStr != "" {
			alpnList = strings.Split(alpnStr, ",")
		} else {
			alpnList = []string{"h2", "http/1.1"}
		}

		tlsSettings := &domain.TLSSettings{
			ServerName: h.stateManager.GetStringData(userID, "tls_sni"),
			ALPN:       alpnList,
		}

		// Reference the saved SNI by id; the cert is resolved live at push time.
		if sniIDStr := h.stateManager.GetStringData(userID, "sni_id"); sniIDStr != "" {
			if sniID, e := strconv.ParseUint(sniIDStr, 10, 32); e == nil {
				tlsSettings.Certificates = []domain.Certificate{
					{SNIId: uint(sniID), Usage: "encipherment"},
				}
			}
		}

		inbound.TLSSettings = tlsSettings
	}

	h.stateManager.ResetSession(userID)

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	if err := h.nodeUsecase.AddInbound(ctx, inbound); err != nil {
		return c.Send("❌ Failed to add inbound: "+err.Error(), keyboards.AdminMenu())
	}

	return c.Send("✅ Inbound Added & Pushed to Xray!", keyboards.AdminMenu())
}
