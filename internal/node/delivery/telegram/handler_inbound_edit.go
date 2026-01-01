package telegram

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/conversation"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"gopkg.in/telebot.v3"
)

// === Edit Inbound Menu ===

func (h *Handler) HandleEditInbound(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	inbound, err := h.nodeUsecase.GetInbound(ctx, id)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Inbound not found"})
	}

	msg := fmt.Sprintf("✏️ *Edit Inbound*\n\n🏷 Tag: `%s`\nSelect what to edit:", inbound.Tag)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("📝 Basic Info", "admin_inbound_edit_basic", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("🌐 Network Settings", "admin_inbound_edit_network", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("🔒 Security Settings", "admin_inbound_edit_security", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("🔗 Link Format", "admin_inbound_edit_link", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("⚙️ Advanced Settings", "admin_inbound_advanced", fmt.Sprintf("%d", id))),
		keyboards.BackRowID(kb, "admin_inbound_view", id),
	)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleEditBasicInfo(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	inbound, err := h.nodeUsecase.GetInbound(ctx, id)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Error"})
	}

	// Show actual address that will be used in links
	displayAddress := inbound.Address
	if displayAddress == "" && inbound.Node != nil {
		displayAddress = fmt.Sprintf("(%s - from Node)", inbound.Node.IP)
	} else if displayAddress == "" {
		displayAddress = "(uses Node IP)"
	}

	msg := fmt.Sprintf("📝 *Edit Basic Info*\n\n"+
		"📝 Remark: %s\n"+
		"🌐 Domain/IP: %s\n"+
		"🚪 Port: %d",
		inbound.Remark, displayAddress, inbound.Port)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("📝 Edit Remark", "admin_inbound_set_remark", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("🌐 Edit Domain/IP", "admin_inbound_set_address", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("🚪 Edit Port", "admin_inbound_set_port", fmt.Sprintf("%d", id))),
		keyboards.BackRowID(kb, "admin_inbound_edit", id),
	)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleEditNetworkSettings(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	inbound, err := h.nodeUsecase.GetInbound(ctx, id)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Error"})
	}

	transportInfo := "None"
	if inbound.TransportSettings != nil {
		if inbound.TransportSettings.Path != "" {
			transportInfo = fmt.Sprintf("Path: %s", inbound.TransportSettings.Path)
		} else if inbound.TransportSettings.ServiceName != "" {
			transportInfo = fmt.Sprintf("Service: %s", inbound.TransportSettings.ServiceName)
		} else {
			transportInfo = "Configured"
		}
	}

	msg := fmt.Sprintf("🌐 *Edit Network Settings*\n\n"+
		"📡 Network: %s\n"+
		"📂 Transport: %s",
		inbound.Network, transportInfo)

	kb := &telebot.ReplyMarkup{}
	// Build network-specific buttons
	var rows []telebot.Row
	rows = append(rows, kb.Row(kb.Data("📡 Network Type", "admin_inbound_set_network", fmt.Sprintf("%d", id))))

	// Path: relevant for ws, xhttp, httpupgrade
	if inbound.Network == "ws" || inbound.Network == "xhttp" || inbound.Network == "splithttp" || inbound.Network == "httpupgrade" {
		rows = append(rows, kb.Row(kb.Data("📂 Path", "admin_inbound_set_path", fmt.Sprintf("%d", id))))
	}

	// Host: relevant for ws, xhttp, httpupgrade
	if inbound.Network == "ws" || inbound.Network == "xhttp" || inbound.Network == "splithttp" || inbound.Network == "httpupgrade" {
		rows = append(rows, kb.Row(kb.Data("🏠 Host", "admin_inbound_set_host", fmt.Sprintf("%d", id))))
	}

	// Service Name: only for grpc
	if inbound.Network == "grpc" {
		rows = append(rows, kb.Row(kb.Data("🔧 Service Name", "admin_inbound_set_service", fmt.Sprintf("%d", id))))
	}

	// XHTTP Mode: only for xhttp/splithttp
	if inbound.Network == "xhttp" || inbound.Network == "splithttp" {
		rows = append(rows, kb.Row(kb.Data("⚡ XHTTP Mode", "admin_inbound_set_mode", fmt.Sprintf("%d", id))))
	}

	rows = append(rows, keyboards.BackRowID(kb, "admin_inbound_edit", id))
	kb.Inline(rows...)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleEditSecuritySettings(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	inbound, err := h.nodeUsecase.GetInbound(ctx, id)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Error"})
	}

	// Parse TLS settings for display
	tlsInfo := "Not configured"
	if inbound.TLSSettings != nil {
		tls := inbound.GetTLSSettingsOrDefault()
		if tls.ServerName != "" {
			certInfo := "No certificates"
			if len(tls.Certificates) > 0 {
				certInfo = fmt.Sprintf("%d certificate(s)", len(tls.Certificates))
			}
			tlsInfo = fmt.Sprintf("SNI: %s, ALPN: %v, %s", tls.ServerName, tls.ALPN, certInfo)
		}
	}

	// Parse Reality settings for display
	realityInfo := "Not configured"
	if inbound.RealitySettings != nil {
		reality := inbound.GetRealitySettingsOrDefault()
		if reality.Dest != "" {
			realityInfo = fmt.Sprintf("Dest: %s, SNI: %v", reality.Dest, reality.ServerNames)
		}
	}

	secType := inbound.Security
	if secType == "" {
		secType = "none"
	}

	msg := fmt.Sprintf("🔒 *Edit Security Settings*\n\n"+
		"🔑 Security: %s\n"+
		"📄 TLS: %s\n"+
		"✨ Reality: %s",
		secType, tlsInfo, realityInfo)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("🔑 Security Type", "admin_inbound_set_security", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("🌍 SNI", "admin_inbound_set_sni", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("👆 Fingerprint", "admin_inbound_set_fp", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("📋 ALPN", "admin_inbound_set_alpn", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("📜 Certificates", "admin_inbound_set_certs", fmt.Sprintf("%d", id))),
		keyboards.BackRowID(kb, "admin_inbound_edit", id),
	)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// === Edit Field Starters ===

func (h *Handler) HandleSetRemark(c telebot.Context) error {
	return h.startEdit(c, conversation.StateEditInboundRemark, "📝 Enter new *Remark*:")
}
func (h *Handler) HandleSetAddress(c telebot.Context) error {
	return h.startEdit(c, conversation.StateEditInboundAddress, "🌐 Enter *Domain/IP* for config links:\n\n(Leave empty or send `-` to use Node IP)")
}
func (h *Handler) HandleSetPort(c telebot.Context) error {
	return h.startEdit(c, conversation.StateEditInboundPort, "🚪 Enter new *Port*:")
}
func (h *Handler) HandleSetNetwork(c telebot.Context) error {
	return h.startEdit(c, conversation.StateEditInboundNetwork, "📡 Enter *Network* type (tcp, ws, grpc, xhttp, httpupgrade):")
}
func (h *Handler) HandleSetPath(c telebot.Context) error {
	return h.startEdit(c, conversation.StateEditInboundPath, "📂 Enter *Path* (e.g. /ws):")
}
func (h *Handler) HandleSetHost(c telebot.Context) error {
	return h.startEdit(c, conversation.StateEditInboundHost, "🏠 Enter *Host*:")
}
func (h *Handler) HandleSetSecurity(c telebot.Context) error {
	return h.startEdit(c, conversation.StateEditInboundSecurity, "🔑 Enter *Security* type (tls, reality, none):")
}
func (h *Handler) HandleSetSNI(c telebot.Context) error {
	return h.startEdit(c, conversation.StateEditInboundSNI, "🌍 Enter *SNI* (Server Name Indication):")
}

// HandleSetFingerprint shows fingerprint selection buttons
func (h *Handler) HandleSetFingerprint(c telebot.Context) error {
	utils.AnswerCallback(c)
	id := c.Data()

	// Get current fingerprint
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	inboundID, _ := strconv.ParseUint(id, 10, 32)
	inbound, _ := h.nodeUsecase.GetInbound(ctx, uint(inboundID))

	currentFP := "chrome"
	if inbound != nil && inbound.TLSSettings != nil {
		tls := inbound.GetTLSSettingsOrDefault()
		if tls.Fingerprint != "" {
			currentFP = tls.Fingerprint
		}
	}

	fingerprints := []string{"chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized"}

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, fp := range fingerprints {
		icon := ""
		if fp == currentFP {
			icon = "✅ "
		}
		rows = append(rows, kb.Row(kb.Data(icon+fp, "inbound_set_fp_val", fmt.Sprintf("%s:%s", id, fp))))
	}
	rows = append(rows, keyboards.BackRow(kb, "admin_inbound_edit_security", id))
	kb.Inline(rows...)

	return c.Edit(fmt.Sprintf("👆 *Select Fingerprint*\n\nCurrent: `%s`", currentFP), telebot.ModeMarkdown, kb)
}

// HandleSetFingerprintValue saves the selected fingerprint
func (h *Handler) HandleSetFingerprintValue(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	// Parse data: "inboundID:fingerprint"
	parts := strings.Split(c.Data(), ":")
	if len(parts) != 2 {
		return c.Respond(&telebot.CallbackResponse{Text: "Invalid data"})
	}

	inboundID, _ := strconv.ParseUint(parts[0], 10, 32)
	fingerprint := parts[1]

	inbound, err := h.nodeUsecase.GetInbound(ctx, uint(inboundID))
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Inbound not found"})
	}

	// Get or create TLS settings
	tlsSettings := inbound.GetTLSSettingsOrDefault()
	if tlsSettings == nil {
		tlsSettings = &domain.TLSSettings{}
	}
	tlsSettings.Fingerprint = fingerprint
	inbound.TLSSettings = tlsSettings

	if err := h.nodeUsecase.UpdateInbound(ctx, inbound); err != nil {
		return c.Send("❌ Failed to update: "+err.Error(), keyboards.AdminMenu())
	}

	c.Respond(&telebot.CallbackResponse{Text: "Fingerprint set!"})
	return c.Send(fmt.Sprintf("✅ Fingerprint set to `%s`", fingerprint), telebot.ModeMarkdown, keyboards.AdminMenu())
}

func (h *Handler) HandleSetCerts(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	inboundID := c.Data()

	// Get current inbound to check which SNI is applied
	id, _ := strconv.ParseUint(inboundID, 10, 32)
	inbound, _ := h.nodeUsecase.GetInbound(ctx, uint(id))

	// Get current SNI domain from inbound TLS settings
	currentSNI := ""
	if inbound != nil && inbound.TLSSettings != nil {
		tls := inbound.GetTLSSettingsOrDefault()
		currentSNI = tls.ServerName
	}

	// Show saved SNIs as options
	snis, _ := h.sniUsecase.List(ctx)

	if len(snis) == 0 {
		return c.Edit("📜 *No saved certificates*\n\nGo to Admin Menu → 🔐 TLS Certs to add certificates first.", telebot.ModeMarkdown, keyboards.AdminMenu())
	}

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, sni := range snis {
		// Mark current certificate with ✅
		icon := "🌐"
		if sni.Domain == currentSNI {
			icon = "✅"
		}
		rows = append(rows, kb.Row(kb.Data(fmt.Sprintf("%s %s (%s)", icon, sni.Name, sni.Domain), "inbound_apply_sni", fmt.Sprintf("%s:%d", inboundID, sni.ID))))
	}
	rows = append(rows, keyboards.BackRow(kb, "admin_inbound_edit_security", inboundID))
	kb.Inline(rows...)

	return c.Edit("📜 *Select Certificate*\n\nChoose a saved certificate to apply:", telebot.ModeMarkdown, kb)
}
func (h *Handler) HandleSetLinkFormat(c telebot.Context) error {
	return h.startEdit(c, conversation.StateEditInboundLinkFormat, "🔗 Enter new *Link Format* template:\n\nPlaceholders: `{uuid}`, `{host}`, `{port}`, `{name}`")
}
func (h *Handler) HandleSetMode(c telebot.Context) error {
	return h.startEdit(c, conversation.StateEditInboundMode, "🚀 Enter *XHTTP Mode* (packet-up, stream-up, stream-one, auto):")
}

// HandleApplySNI applies a saved SNI certificate to an existing inbound
func (h *Handler) HandleApplySNI(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	// Parse data: "inboundID:sniID"
	parts := strings.Split(c.Data(), ":")
	if len(parts) != 2 {
		return c.Respond(&telebot.CallbackResponse{Text: "Invalid data"})
	}

	inboundID, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Invalid inbound ID"})
	}

	sniID, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Invalid SNI ID"})
	}

	// Get the inbound
	inbound, err := h.nodeUsecase.GetInbound(ctx, uint(inboundID))
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Inbound not found"})
	}

	// Get the SNI
	sni, err := h.sniUsecase.GetByID(ctx, uint(sniID))
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "SNI not found"})
	}

	// Apply SNI settings to inbound TLS
	tlsSettings := &domain.TLSSettings{
		ServerName: sni.Domain,
		ALPN:       sni.GetALPNList(),
	}

	// Reference the SNI by id; its cert (content or path mode) is resolved live
	// at push time, so a later renewal or edit propagates automatically.
	tlsSettings.Certificates = []domain.Certificate{
		{SNIId: sni.ID, Usage: "encipherment"},
	}

	inbound.TLSSettings = tlsSettings
	inbound.Security = "tls"

	// Save the updated inbound
	if err := h.nodeUsecase.UpdateInbound(ctx, inbound); err != nil {
		return c.Send("❌ Failed to update inbound: "+err.Error(), keyboards.AdminMenu())
	}

	modeStr := "Content"
	if sni.UsePathMode {
		modeStr = "File Path"
	}

	c.Respond(&telebot.CallbackResponse{Text: "Certificate applied!"})
	// Use Send with reply keyboard since Edit requires inline keyboard
	return c.Send(fmt.Sprintf("✅ Certificate **%s** (`%s`) applied to inbound!\n\nMode: %s\nSecurity set to TLS.", sni.Name, sni.Domain, modeStr), telebot.ModeMarkdown, keyboards.AdminMenu())
}
func (h *Handler) startEdit(c telebot.Context, state conversation.ConversationState, msg string) error {
	utils.AnswerCallback(c)
	id := utils.CallbackID(c)
	userID := c.Sender().ID
	h.stateManager.StartConversation(userID, state)
	h.stateManager.SetData(userID, "inbound_id", id)
	return c.Send(msg, telebot.ModeMarkdown, keyboards.Cancel())
}

// === Edit Input Processors ===

func (h *Handler) HandleRemarkInput(c telebot.Context) error {
	return h.processEdit(c, func(in *domain.Inbound) { in.Remark = c.Text() })
}
func (h *Handler) HandleAddressInput(c telebot.Context) error {
	addr := c.Text()
	if addr == "-" {
		addr = "" // Clear = use Node IP
	}
	return h.processEdit(c, func(in *domain.Inbound) { in.Address = addr })
}
func (h *Handler) HandlePortInput(c telebot.Context) error {
	port, _ := strconv.Atoi(c.Text())
	return h.processEdit(c, func(in *domain.Inbound) { in.Port = port })
}
func (h *Handler) HandleNetworkInput(c telebot.Context) error {
	return h.processEdit(c, func(in *domain.Inbound) { in.Network = strings.ToLower(c.Text()) })
}
func (h *Handler) HandleSecurityInput(c telebot.Context) error {
	return h.processEdit(c, func(in *domain.Inbound) { in.Security = strings.ToLower(c.Text()) })
}
func (h *Handler) HandleLinkFormatInput(c telebot.Context) error {
	return h.processEdit(c, func(in *domain.Inbound) { in.LinkFormat = c.Text() })
}
func (h *Handler) HandleModeInput(c telebot.Context) error {
	mode := strings.ToLower(c.Text())
	return h.processEdit(c, func(in *domain.Inbound) {
		transport := in.GetTransportSettingsOrDefault()
		if transport == nil {
			transport = &domain.TransportSettings{}
		}
		transport.Mode = mode
		in.TransportSettings = transport
	})
}

// === Missing Handlers Implementation ===

func (h *Handler) HandleInboundSNIInput(c telebot.Context) error {
	return h.processEdit(c, func(in *domain.Inbound) {
		tls := in.GetTLSSettingsOrDefault()
		if tls == nil {
			tls = &domain.TLSSettings{}
		}
		tls.ServerName = c.Text()
		in.TLSSettings = tls
	})
}

func (h *Handler) HandleInboundALPNInput(c telebot.Context) error {
	return h.processEdit(c, func(in *domain.Inbound) {
		tls := in.GetTLSSettingsOrDefault()
		if tls == nil {
			tls = &domain.TLSSettings{}
		}
		// Expect comma separated values, e.g. "h2,http/1.1"
		parts := strings.Split(c.Text(), ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		tls.ALPN = parts
		in.TLSSettings = tls
	})
}

func (h *Handler) HandlePathInput(c telebot.Context) error {
	path := c.Text()
	return h.processEdit(c, func(in *domain.Inbound) {
		transport := in.GetTransportSettingsOrDefault()
		if transport == nil {
			transport = &domain.TransportSettings{}
		}
		transport.Path = path
		in.TransportSettings = transport
	})
}

func (h *Handler) HandleHostInput(c telebot.Context) error {
	host := c.Text()
	return h.processEdit(c, func(in *domain.Inbound) {
		transport := in.GetTransportSettingsOrDefault()
		if transport == nil {
			transport = &domain.TransportSettings{}
		}
		transport.Host = host
		in.TransportSettings = transport
	})
}

func (h *Handler) HandleServiceInput(c telebot.Context) error {
	service := c.Text()
	return h.processEdit(c, func(in *domain.Inbound) {
		transport := in.GetTransportSettingsOrDefault()
		if transport == nil {
			transport = &domain.TransportSettings{}
		}
		transport.ServiceName = service
		in.TransportSettings = transport
	})
}

func (h *Handler) processEdit(c telebot.Context, updater func(*domain.Inbound)) error {
	userID := c.Sender().ID
	inboundID := h.stateManager.GetUint(userID, "inbound_id")
	h.stateManager.ResetSession(userID)

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	inbound, err := h.nodeUsecase.GetInbound(ctx, inboundID)
	if err != nil {
		return c.Send("❌ Inbound not found", keyboards.AdminMenu())
	}

	updater(inbound)

	// This now triggers the API push in usecase
	if err := h.nodeUsecase.UpdateInbound(ctx, inbound); err != nil {
		return c.Send("❌ Failed to update & push: "+err.Error(), keyboards.AdminMenu())
	}

	return c.Send("✅ Inbound updated successfully!", keyboards.AdminMenu())
}

// === Advanced Settings (JSON Editing) ===

func (h *Handler) HandleAdvancedSettings(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)

	msg := "⚙️ *Advanced Settings*\n\n" +
		"Edit raw JSON configuration for this inbound.\n" +
		"⚠️ *Warning:* Invalid JSON may break the inbound."

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("🔒 TLS Settings", "admin_inbound_adv_tls", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("🌐 Transport Settings", "admin_inbound_adv_transport", fmt.Sprintf("%d", id))),
		kb.Row(kb.Data("👁 Sniffing Settings", "admin_inbound_adv_sniffing", fmt.Sprintf("%d", id))),
		keyboards.BackRowID(kb, "admin_inbound_edit", id),
	)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleAdvancedTLS(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	inbound, err := h.nodeUsecase.GetInbound(ctx, id)
	if err != nil {
		return c.Edit("❌ Inbound not found")
	}

	var currentJSON string
	if inbound.TLSSettings != nil {
		jsonBytes, _ := json.MarshalIndent(inbound.TLSSettings, "", "  ")
		currentJSON = string(jsonBytes)
	} else {
		currentJSON = `{"serverName": "", "alpn": ["h2", "http/1.1"]}`
	}

	h.stateManager.SetState(c.Sender().ID, conversation.StateEditInboundAdvancedTLS)
	h.stateManager.SetData(c.Sender().ID, "inbound_id", id)

	msg := fmt.Sprintf("🔒 *TLS Settings (JSON)*\n\n"+
		"Current:\n```json\n%s\n```\n\n"+
		"Send new JSON or `cancel` to abort:", currentJSON)

	return c.Edit(msg, telebot.ModeMarkdown)
}

func (h *Handler) HandleAdvancedTransport(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	inbound, err := h.nodeUsecase.GetInbound(ctx, id)
	if err != nil {
		return c.Edit("❌ Inbound not found")
	}

	var currentJSON string
	if inbound.TransportSettings != nil {
		jsonBytes, _ := json.MarshalIndent(inbound.TransportSettings, "", "  ")
		currentJSON = string(jsonBytes)
	} else {
		currentJSON = `{"path": "/", "host": ""}`
	}

	h.stateManager.SetState(c.Sender().ID, conversation.StateEditInboundAdvancedTransport)
	h.stateManager.SetData(c.Sender().ID, "inbound_id", id)

	msg := fmt.Sprintf("🌐 *Transport Settings (JSON)*\n\n"+
		"Current:\n```json\n%s\n```\n\n"+
		"Send new JSON or `cancel` to abort:", currentJSON)

	return c.Edit(msg, telebot.ModeMarkdown)
}

func (h *Handler) HandleAdvancedSniffing(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	inbound, err := h.nodeUsecase.GetInbound(ctx, id)
	if err != nil {
		return c.Edit("❌ Inbound not found")
	}

	var currentJSON string
	if inbound.SniffingSettings != nil {
		jsonBytes, _ := json.MarshalIndent(inbound.SniffingSettings, "", "  ")
		currentJSON = string(jsonBytes)
	} else {
		currentJSON = `{"enabled": true, "destOverride": ["http", "tls"]}`
	}

	h.stateManager.SetState(c.Sender().ID, conversation.StateEditInboundAdvancedSniffing)
	h.stateManager.SetData(c.Sender().ID, "inbound_id", id)

	msg := fmt.Sprintf("👁 *Sniffing Settings (JSON)*\n\n"+
		"Current:\n```json\n%s\n```\n\n"+
		"Send new JSON or `cancel` to abort:", currentJSON)

	return c.Edit(msg, telebot.ModeMarkdown)
}

func (h *Handler) HandleAdvancedJSONInput(c telebot.Context, settingType string) error {
	userID := c.Sender().ID
	input := strings.TrimSpace(c.Text())

	if strings.ToLower(input) == "cancel" {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Cancelled", keyboards.AdminMenu())
	}

	// Validate JSON
	if !json.Valid([]byte(input)) {
		return c.Send("❌ Invalid JSON. Please try again or send `cancel`:", telebot.ModeMarkdown)
	}

	inboundID := uint(h.stateManager.GetIntData(userID, "inbound_id"))
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	inbound, err := h.nodeUsecase.GetInbound(ctx, uint(inboundID))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Inbound not found", keyboards.AdminMenu())
	}

	switch settingType {
	case "tls":
		var tls domain.TLSSettings
		if err := json.Unmarshal([]byte(input), &tls); err != nil {
			return c.Send("❌ Invalid TLS settings JSON: "+err.Error(), telebot.ModeMarkdown)
		}
		inbound.TLSSettings = &tls
	case "transport":
		var transport domain.TransportSettings
		if err := json.Unmarshal([]byte(input), &transport); err != nil {
			return c.Send("❌ Invalid Transport settings JSON: "+err.Error(), telebot.ModeMarkdown)
		}
		inbound.TransportSettings = &transport
	case "sniffing":
		var sniffing domain.SniffingSettings
		if err := json.Unmarshal([]byte(input), &sniffing); err != nil {
			return c.Send("❌ Invalid Sniffing settings JSON: "+err.Error(), telebot.ModeMarkdown)
		}
		inbound.SniffingSettings = &sniffing
	}

	if err := h.nodeUsecase.UpdateInbound(ctx, inbound); err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Failed to save: "+err.Error(), keyboards.AdminMenu())
	}

	h.stateManager.ResetSession(userID)
	return c.Send("✅ Settings updated successfully!", keyboards.AdminMenu())
}
