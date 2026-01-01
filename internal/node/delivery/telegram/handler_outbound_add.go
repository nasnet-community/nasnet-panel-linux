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
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/link"
	"gopkg.in/telebot.v3"
)

// === Wizard: Add Outbound ===

func (h *Handler) HandleAddOutboundStart(c telebot.Context) error {
	utils.AnswerCallback(c)
	nodeID := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateAddOutboundTag)
	h.stateManager.SetData(userID, "node_id", nodeID)

	return c.Edit("➕ *Add Outbound*\n\nStep 1: Enter *Outbound Tag* (unique ID, e.g. `direct-out` or `proxy-out`):", telebot.ModeMarkdown, keyboards.CancelInline())
}

func (h *Handler) HandleAddOutboundTag(c telebot.Context) error {
	userID := c.Sender().ID
	tag := c.Text()
	h.stateManager.SetData(userID, "tag", tag)
	h.stateManager.SetState(userID, conversation.StateAddOutboundProtocol)

	msg := "Step 2: Choose *Protocol Type*:\n\n" +
		"• *freedom*: Direct connection (exit traffic)\n" +
		"• *blackhole*: Block/drop traffic\n" +
		"• *socks*: Standard Socks5 proxy\n" +
		"• *http*: Standard HTTP proxy\n" +
		"• *shadowsocks*: Shadowsocks proxy\n" +
		"• *vless*: Proxy chain to VLESS server\n" +
		"• *vmess*: Proxy chain to VMess server\n" +
		"• *trojan*: Proxy chain to Trojan server"

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("🌐 Freedom (Direct)", "outbound_proto_freedom")),
		kb.Row(kb.Data("🕳 Blackhole (Block)", "outbound_proto_blackhole")),
		kb.Row(kb.Data("🧦 Socks", "outbound_proto_socks"), kb.Data("🌐 HTTP", "outbound_proto_http")),
		kb.Row(kb.Data("👻 Shadowsocks", "outbound_proto_shadowsocks")),
		kb.Row(kb.Data("⚡ VLESS", "outbound_proto_vless")),
		kb.Row(kb.Data("📡 VMess", "outbound_proto_vmess")),
		kb.Row(kb.Data("🔐 Trojan", "outbound_proto_trojan")),
	)

	return c.Send(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleOutboundProtocolCallback(c telebot.Context) error {
	utils.AnswerCallback(c)
	proto := strings.TrimPrefix(c.Data(), "outbound_proto_")
	userID := c.Sender().ID

	h.stateManager.SetData(userID, "protocol", proto)

	// BRANCH 1: Direct/Block (No address/port needed)
	if proto == "freedom" || proto == "blackhole" {
		h.stateManager.SetState(userID, conversation.StateAddOutboundRemark)
		// Set defaults for db constraints
		h.stateManager.SetData(userID, "address", "127.0.0.1")
		h.stateManager.SetData(userID, "port", 0)
		return c.Edit("Step 3: Enter *Remark/Label* (e.g. 'Direct Exit'):", telebot.ModeMarkdown)
	}

	// BRANCH 2: Proxies (Need Server Info)
	h.stateManager.SetState(userID, conversation.StateAddOutboundAddress)
	return c.Edit("Step 3: Enter *Server Address* (IP or domain):", telebot.ModeMarkdown)
}

func (h *Handler) HandleAddOutboundAddress(c telebot.Context) error {
	userID := c.Sender().ID
	address := c.Text()
	h.stateManager.SetData(userID, "address", address)
	h.stateManager.SetState(userID, conversation.StateAddOutboundPort)
	return c.Send("Step 4: Enter *Server Port* (e.g. 443):", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddOutboundPort(c telebot.Context) error {
	userID := c.Sender().ID
	port, err := strconv.Atoi(c.Text())
	if err != nil || port < 1 || port > 65535 {
		return c.Send("❌ Invalid port (1-65535). Try again:")
	}
	h.stateManager.SetData(userID, "port", port)

	proto := h.stateManager.GetStringData(userID, "protocol")

	// Branching based on protocol Auth needs
	switch proto {
	case "vmess", "vless":
		h.stateManager.SetState(userID, conversation.StateAddOutboundUUID)
		return c.Send("Step 5: Enter *UUID*:", telebot.ModeMarkdown, keyboards.Cancel())
	case "trojan":
		h.stateManager.SetState(userID, conversation.StateAddOutboundPassword)
		return c.Send("Step 5: Enter *Password*:", telebot.ModeMarkdown, keyboards.Cancel())
	case "shadowsocks":
		h.stateManager.SetState(userID, conversation.StateAddOutboundMethod)
		kb := &telebot.ReplyMarkup{}
		kb.Inline(
			kb.Row(kb.Data("AES-128-GCM", "outbound_ss_method", "aes-128-gcm")),
			kb.Row(kb.Data("AES-256-GCM", "outbound_ss_method", "aes-256-gcm")),
			kb.Row(kb.Data("ChaCha20-Poly1305", "outbound_ss_method", "chacha20-poly1305")),
		)
		return c.Send("Step 5: Select *Encryption Method*:", telebot.ModeMarkdown, kb)
	case "socks", "http":
		h.stateManager.SetState(userID, conversation.StateAddOutboundUsername)
		return c.Send("Step 5: Enter *Username* (or send `-` for none):", telebot.ModeMarkdown, keyboards.Cancel())
	}

	// Fallback
	h.stateManager.SetState(userID, conversation.StateAddOutboundRemark)
	return c.Send("Step 6: Enter *Remark*:", telebot.ModeMarkdown)
}

func (h *Handler) HandleOutboundSSMethod(c telebot.Context) error {
	utils.AnswerCallback(c)
	method := "aes-128-gcm"
	if len(c.Data()) > 0 {
		method = c.Data()
	}

	userID := c.Sender().ID
	h.stateManager.SetData(userID, "method", method)
	h.stateManager.SetState(userID, conversation.StateAddOutboundPassword)
	return c.Edit("Step 6: Enter *Password*:", telebot.ModeMarkdown)
}

func (h *Handler) HandleAddOutboundMethod(c telebot.Context) error {
	utils.AnswerCallback(c)
	method := c.Data()
	userID := c.Sender().ID
	h.stateManager.SetData(userID, "method", method)

	h.stateManager.SetState(userID, conversation.StateAddOutboundPassword)
	return c.Edit("Step 6: Enter *Password*:", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddOutboundUUID(c telebot.Context) error {
	userID := c.Sender().ID
	uuid := c.Text()

	h.stateManager.SetData(userID, "uuid", uuid)
	return h.goToSecurityStep(c)
}

func (h *Handler) HandleAddOutboundPassword(c telebot.Context) error {
	userID := c.Sender().ID
	pass := c.Text()
	h.stateManager.SetData(userID, "password", pass)
	return h.goToSecurityStep(c)
}

func (h *Handler) HandleAddOutboundUsername(c telebot.Context) error {
	userID := c.Sender().ID
	user := c.Text()
	if user == "-" {
		user = ""
	}
	h.stateManager.SetData(userID, "username", user)

	// If no username, skip password
	if user == "" {
		return h.goToSecurityStep(c)
	}

	// Next is password
	h.stateManager.SetState(userID, conversation.StateAddOutboundPassword)
	return c.Send("Step 6: Enter *Password*:", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) goToSecurityStep(c telebot.Context) error {
	userID := c.Sender().ID
	h.stateManager.SetState(userID, conversation.StateAddOutboundSecurity)

	msg := "Step 7: Choose *Security Type*:\n\n" +
		"• *none*: No TLS\n" +
		"• *tls*: Standard TLS\n" +
		"• *reality*: Reality (client mode)"

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("🔓 None", "outbound_sec_none")),
		kb.Row(kb.Data("🔒 TLS", "outbound_sec_tls")),
		kb.Row(kb.Data("🛡 Reality", "outbound_sec_reality")),
	)

	return c.Send(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleOutboundSecurityCallback(c telebot.Context) error {
	utils.AnswerCallback(c)
	sec := strings.TrimPrefix(c.Data(), "outbound_sec_")
	userID := c.Sender().ID

	h.stateManager.SetData(userID, "security", sec)

	if sec == "tls" || sec == "reality" {
		h.stateManager.SetState(userID, conversation.StateAddOutboundTLSSNI)
		return c.Edit("Enter *SNI* (Server Name Indication, e.g. `google.com`):", telebot.ModeMarkdown)
	}

	h.stateManager.SetState(userID, conversation.StateAddOutboundRemark)
	return c.Edit("Step 7: Enter *Remark/Label*:", telebot.ModeMarkdown)
}

func (h *Handler) HandleAddOutboundTLSSNI(c telebot.Context) error {
	userID := c.Sender().ID
	sni := c.Text()
	h.stateManager.SetData(userID, "sni", sni)
	h.stateManager.SetState(userID, conversation.StateAddOutboundRemark)
	return c.Send("Step 7: Enter *Remark/Label*:", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddOutboundImport(c telebot.Context) error {
	utils.AnswerCallback(c)
	nodeID := c.Data()
	userID := c.Sender().ID

	h.stateManager.SetData(userID, "node_id", nodeID)
	h.stateManager.SetState(userID, conversation.StateAddOutboundImportLink)
	return c.Send("🔗 Paste your **Share Link** (vless://, vmess://, etc.):\n\n_Supports most standard formats._", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddOutboundImportLink(c telebot.Context) error {
	linkStr := c.Text()
	userID := c.Sender().ID

	outbound, err := link.Parse(linkStr)
	if err != nil {
		return c.Send(fmt.Sprintf("❌ Invalid link: %v\n\nPlease try again or use manual mode.", err))
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	nodeIDStr := h.stateManager.GetStringData(userID, "node_id")
	if nodeIDStr == "" {
		return c.Send("❌ Session expired. Please start from the Node menu.")
	}
	nodeID, _ := strconv.ParseUint(nodeIDStr, 10, 32)
	outbound.NodeID = uint(nodeID)

	if err := h.nodeUsecase.AddOutbound(ctx, outbound); err != nil {
		return c.Send("❌ Failed to save outbound: " + err.Error())
	}

	h.stateManager.ResetSession(userID)
	return c.Send(fmt.Sprintf("✅ Imported **%s** (%s) successfully!", outbound.Remark, outbound.Protocol), telebot.ModeMarkdown, keyboards.AdminMenu())
}

func (h *Handler) HandleAddOutboundRemark(c telebot.Context) error {
	userID := c.Sender().ID
	remark := c.Text()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	// Gather all data
	nodeIDStr := h.stateManager.GetStringData(userID, "node_id")
	nodeID, _ := strconv.ParseUint(nodeIDStr, 10, 32)
	tag := h.stateManager.GetStringData(userID, "tag")
	protocol := h.stateManager.GetStringData(userID, "protocol")
	address := h.stateManager.GetStringData(userID, "address")
	port := h.stateManager.GetIntData(userID, "port")
	uuid := h.stateManager.GetStringData(userID, "uuid")
	username := h.stateManager.GetStringData(userID, "username")
	password := h.stateManager.GetStringData(userID, "password")
	security := h.stateManager.GetStringData(userID, "security")
	sni := h.stateManager.GetStringData(userID, "sni")
	method := h.stateManager.GetStringData(userID, "method")

	h.stateManager.ResetSession(userID)

	// Build outbound
	outbound := &domain.Outbound{
		NodeID:   uint(nodeID),
		Tag:      tag,
		Protocol: protocol,
		Address:  address,
		Port:     port,
		Remark:   remark,
		Security: security,
	}

	// Set protocol settings based on protocol
	switch outbound.Protocol {
	case "vmess":
		outbound.VMessSettings = &domain.VMessSettings{
			UUID:     uuid,
			AlterId:  0,
			Security: "auto",
		}
	case "vless":
		outbound.VLESSSettings = &domain.VLESSSettings{
			UUID:       uuid,
			Flow:       "",
			Encryption: "none",
		}
	case "trojan":
		outbound.TrojanSettings = &domain.TrojanSettings{
			Password: password,
		}
	case "shadowsocks":
		outbound.ShadowsocksSettings = &domain.ShadowsocksSettings{
			Password: password,
			Method:   method,
			Network:  "tcp,udp",
		}
	case "socks":
		if username != "" || password != "" {
			outbound.SOCKSSettings = &domain.SOCKSSettings{
				Auth: "password",
				Accounts: []domain.SOCKSAccount{
					{User: username, Pass: password},
				},
			}
		} else {
			outbound.SOCKSSettings = &domain.SOCKSSettings{Auth: "noauth"}
		}
	case "http":
		if username != "" || password != "" {
			outbound.HTTPSettings = &domain.HTTPSettings{
				Accounts: []domain.HTTPAccount{
					{User: username, Pass: password},
				},
			}
		}
	}

	// Set TLS/Reality settings
	if security == "tls" && sni != "" {
		outbound.TLSSettings = &domain.TLSSettings{
			ServerName:  sni,
			Fingerprint: "chrome",
		}
	} else if security == "reality" && sni != "" {
		outbound.RealitySettings = &domain.RealitySettings{
			ServerName:  sni,
			ServerNames: []string{sni},
			Fingerprint: "chrome",
		}
	}

	err := h.nodeUsecase.AddOutbound(ctx, outbound)
	if err != nil {
		return c.Send("❌ Failed to create outbound: " + err.Error())
	}

	msg := fmt.Sprintf("✅ *Outbound Created!*\n\n"+
		"🏷 Tag: `%s`\n"+
		"⚙️ Protocol: %s\n"+
		"📝 Remark: %s",
		tag, protocol, remark)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Outbounds", "admin_node_outbounds", nodeIDStr)))

	return c.Send(msg, telebot.ModeMarkdown, kb)
}
