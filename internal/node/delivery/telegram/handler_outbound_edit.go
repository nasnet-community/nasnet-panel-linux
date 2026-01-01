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
	"gopkg.in/telebot.v3"
)

// HandleOutboundEdit shows edit menu for an outbound
func (h *Handler) HandleOutboundEdit(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	outbound, err := h.nodeUsecase.GetOutbound(ctx, id)
	if err != nil {
		return c.Edit("❌ Outbound not found")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✏️ *Edit Outbound: %s*\n", outbound.Tag))
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("Protocol: `%s`\n", strings.ToUpper(outbound.Protocol)))

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	// --- Basic Settings Section ---
	rows = append(rows, kb.Row(
		kb.Data("🏷 Tag", "edit_outbound_tag", fmt.Sprintf("%d", id)),
		kb.Data("📝 Remark", "edit_outbound_remark", fmt.Sprintf("%d", id)),
	))
	rows = append(rows, kb.Row(kb.Data("⚙️ Protocol", "edit_outbound_protocol", fmt.Sprintf("%d", id))))

	// Branch based on protocol
	if outbound.Protocol != "freedom" && outbound.Protocol != "blackhole" {
		// --- Server Section ---
		rows = append(rows, kb.Row(
			kb.Data("🌐 Address", "edit_outbound_address", fmt.Sprintf("%d", id)),
			kb.Data("🔢 Port", "edit_outbound_port", fmt.Sprintf("%d", id)),
		))

		// --- Authentication Section ---
		switch outbound.Protocol {
		case "vmess", "vless":
			rows = append(rows, kb.Row(kb.Data("🆔 UUID", "edit_outbound_uuid", fmt.Sprintf("%d", id))))
			if outbound.Protocol == "vless" {
				rows = append(rows, kb.Row(kb.Data("🌊 Flow", "edit_outbound_flow", fmt.Sprintf("%d", id))))
			}
		case "trojan":
			rows = append(rows, kb.Row(kb.Data("🔑 Password", "edit_outbound_password", fmt.Sprintf("%d", id))))
		case "shadowsocks":
			rows = append(rows, kb.Row(
				kb.Data("🔒 Method", "edit_outbound_method", fmt.Sprintf("%d", id)),
				kb.Data("🔑 Password", "edit_outbound_password", fmt.Sprintf("%d", id)),
			))
		case "socks", "http":
			rows = append(rows, kb.Row(
				kb.Data("👤 Username", "edit_outbound_username", fmt.Sprintf("%d", id)),
				kb.Data("🔑 Password", "edit_outbound_password", fmt.Sprintf("%d", id)),
			))
		}

		// --- Transport & Security Section ---
		rows = append(rows, kb.Row(
			kb.Data("📡 Network", "edit_outbound_network", fmt.Sprintf("%d", id)),
			kb.Data("🔒 Security", "edit_outbound_security", fmt.Sprintf("%d", id)),
		))
	} else if outbound.Protocol == "freedom" {
		// Freedom-specific settings
		rows = append(rows, kb.Row(kb.Data("🧠 Domain Strategy", "edit_outbound_domain_strategy", fmt.Sprintf("%d", id))))
	}

	// --- Advanced Settings ---
	rows = append(rows, kb.Row(kb.Data("⚙️ Advanced Settings", "admin_outbound_edit_advanced", fmt.Sprintf("%d", id))))

	// --- Navigation ---
	rows = append(rows, keyboards.BackRowID(kb, "admin_outbound_view", id))
	kb.Inline(rows...)

	return c.Edit(sb.String(), telebot.ModeMarkdown, kb)
}

// HandleOutboundEditAdvanced shows advanced settings menu
func (h *Handler) HandleOutboundEditAdvanced(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	outbound, err := h.nodeUsecase.GetOutbound(ctx, id)
	if err != nil {
		return c.Edit("❌ Outbound not found")
	}

	msg := fmt.Sprintf("⚙️ *Advanced Settings: %s*\n\nSelect a setting to edit:", outbound.Tag)
	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	// Generic
	rows = append(rows, kb.Row(
		kb.Data("📊 Level", "edit_outbound_level", fmt.Sprintf("%d", id)),
		kb.Data("📧 Email", "edit_outbound_email", fmt.Sprintf("%d", id)),
	))

	// Protocol Specific
	if outbound.Protocol == "vless" {
		rows = append(rows, kb.Row(kb.Data("🔐 Encryption", "edit_outbound_encryption", fmt.Sprintf("%d", id))))
	}
	if outbound.Protocol == "shadowsocks" {
		rows = append(rows, kb.Row(
			kb.Data("🛡 IVCheck", "edit_outbound_ivcheck", fmt.Sprintf("%d", id)),
			kb.Data("🚀 UoT", "edit_outbound_uot", fmt.Sprintf("%d", id)),
		))
	}

	// Socket Options
	rows = append(rows, kb.Row(kb.Data("🔌 Sockopt", "admin_outbound_edit_sockopt", fmt.Sprintf("%d", id))))

	rows = append(rows, keyboards.BackRowID(kb, "admin_outbound_edit", id))
	kb.Inline(rows...)
	return c.Edit(msg, kb, telebot.ModeMarkdown)
}

// HandleOutboundEditSockopt shows sockopt menu
func (h *Handler) HandleOutboundEditSockopt(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	outbound, err := h.nodeUsecase.GetOutbound(ctx, id)
	if err != nil {
		return c.Edit("❌ Outbound not found")
	}

	sockopt := outbound.GetSockoptSettingsOrDefault()
	if sockopt == nil {
		sockopt = &domain.SockoptSettings{}
	}

	msg := fmt.Sprintf("🔌 *Sockopt Settings: %s*\n\n"+
		"Current Interface: `%s`\n"+
		"Mark: `%d`\n"+
		"TFO: `%v`\n"+
		"TProxy: `%s`\n"+
		"MPTCP: `%v`",
		outbound.Tag, sockopt.Interface, sockopt.Mark, sockopt.TcpFastOpen, sockopt.Tproxy, sockopt.TcpMptcp)

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	rows = append(rows, kb.Row(kb.Data("🌐 Interface", "edit_outbound_interface", fmt.Sprintf("%d", id))))
	rows = append(rows, kb.Row(kb.Data("🏷 Mark", "edit_outbound_mark", fmt.Sprintf("%d", id))))

	// Toggles
	tfoLabel := "TFO: Off 🔴"
	if sockopt.TcpFastOpen {
		tfoLabel = "TFO: On 🟢"
	}
	mptcpLabel := "MPTCP: Off 🔴"
	if sockopt.TcpMptcp {
		mptcpLabel = "MPTCP: On 🟢"
	}
	rows = append(rows, kb.Row(
		kb.Data(tfoLabel, "toggle_outbound_tfo", fmt.Sprintf("%d", id)),
		kb.Data(mptcpLabel, "toggle_outbound_mptcp", fmt.Sprintf("%d", id)),
	))

	// TProxy Select
	idStr := fmt.Sprintf("%d", id)
	rows = append(rows, kb.Row(
		kb.Data("TProxy: Off", "set_outbound_tproxy", idStr+"|off"),
		kb.Data("Redirect", "set_outbound_tproxy", idStr+"|redirect"),
		kb.Data("TProxy", "set_outbound_tproxy", idStr+"|tproxy"),
	))

	rows = append(rows, keyboards.BackRowID(kb, "admin_outbound_edit_advanced", id))
	kb.Inline(rows...)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleEditOutboundLevel handles level edit
func (h *Handler) HandleEditOutboundLevel(c telebot.Context) error {
	return h.handleEditFieldStart(c, conversation.StateEditOutboundLevel, "Enter new *Level* (0-10):")
}

// HandleEditOutboundEmail handles email edit
func (h *Handler) HandleEditOutboundEmail(c telebot.Context) error {
	return h.handleEditFieldStart(c, conversation.StateEditOutboundEmail, "Enter new *Email*:")
}

// HandleEditOutboundSockoptInterface handles interface edit
func (h *Handler) HandleEditOutboundSockoptInterface(c telebot.Context) error {
	return h.handleEditFieldStart(c, conversation.StateEditOutboundInterface, "Enter new *Interface* (e.g. `eth0`, `tun0`):")
}

// HandleEditOutboundSockoptMark handles mark edit
func (h *Handler) HandleEditOutboundSockoptMark(c telebot.Context) error {
	return h.handleEditFieldStart(c, conversation.StateEditOutboundMark, "Enter new *Mark* (integer):")
}

// HandleEditOutboundGenericInput processes simple string/int inputs
func (h *Handler) HandleEditOutboundGenericInput(c telebot.Context, state conversation.ConversationState) error {
	userID := c.Sender().ID
	input := c.Text()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	idStr := h.stateManager.GetStringData(userID, "outbound_id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	// Check if ID is valid
	if id == 0 {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Session expired or invalid outbound ID")
	}

	outbound, err := h.nodeUsecase.GetOutbound(ctx, uint(id))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Outbound not found")
	}

	// Helper to confirm update
	confirm := func(msg string, backBtn string) error {
		if err := h.nodeUsecase.UpdateOutbound(ctx, outbound); err != nil {
			return c.Send("❌ Failed to update: " + err.Error())
		}
		h.stateManager.ResetSession(userID)
		kb := &telebot.ReplyMarkup{}
		kb.Inline(keyboards.BackRow(kb, backBtn, idStr))
		return c.Send(msg, kb)
	}

	switch state {
	case conversation.StateEditOutboundLevel:
		l, err := strconv.Atoi(input)
		if err != nil {
			return c.Send("❌ Invalid Level (must be number):")
		}

		switch outbound.Protocol {
		case "freedom":
			fs := outbound.GetFreedomSettingsOrDefault()
			fs.UserLevel = l
			outbound.FreedomSettings = fs
		case "shadowsocks":
			ss := outbound.GetShadowsocksSettingsOrDefault()
			ss.Level = l
			outbound.ShadowsocksSettings = ss
		case "socks":
			s := outbound.GetSOCKSSettingsOrDefault()
			s.UserLevel = l
			outbound.SOCKSSettings = s
		case "http":
			httpSettings := outbound.GetHTTPSettingsOrDefault()
			httpSettings.UserLevel = l
			outbound.HTTPSettings = httpSettings
		}

		return confirm("✅ Level updated!", "admin_outbound_edit_advanced")

	case conversation.StateEditOutboundEmail:
		if outbound.Protocol == "shadowsocks" {
			ss := outbound.GetShadowsocksSettingsOrDefault()
			ss.Email = input
			outbound.ShadowsocksSettings = ss
		}
		return confirm("✅ Email updated!", "admin_outbound_edit_advanced")

	case conversation.StateEditOutboundInterface:
		ss := outbound.GetSockoptSettingsOrDefault()
		ss.Interface = input
		outbound.SockoptSettings = ss
		return confirm("✅ Interface updated!", "admin_outbound_edit_sockopt")

	case conversation.StateEditOutboundMark:
		m, err := strconv.ParseUint(input, 10, 32)
		if err != nil {
			return c.Send("❌ Invalid Mark (must be number):")
		}
		ss := outbound.GetSockoptSettingsOrDefault()
		ss.Mark = uint32(m)
		outbound.SockoptSettings = ss
		return confirm("✅ Mark updated!", "admin_outbound_edit_sockopt")
	}

	return nil
}

// HandleEditOutboundEncryption handles VLESS encryption edit
func (h *Handler) HandleEditOutboundEncryption(c telebot.Context) error {
	// For VLESS, encryption is usually "none", can be toggled or set manual
	return h.handleEditFieldStart(c, conversation.StateEditOutboundEncryption, "Enter *Encryption* (e.g. `none`):")
}

func (h *Handler) HandleEditOutboundEncryptionInput(c telebot.Context) error {
	val := c.Text()

	return h.updateOutboundField(c, func(outbound *domain.Outbound) {
		if outbound.Protocol == "vless" {
			s := outbound.GetVLESSSettingsOrDefault()
			s.Encryption = val
			outbound.VLESSSettings = s
		}
	}, "Encryption")
}

// HandleEditOutboundIVCheck toggles IVCheck
func (h *Handler) HandleEditOutboundIVCheck(c telebot.Context) error {
	return h.updateOutboundField(c, func(outbound *domain.Outbound) {
		if outbound.Protocol == "shadowsocks" {
			ss := outbound.GetShadowsocksSettingsOrDefault()
			ss.IVCheck = !ss.IVCheck
			outbound.ShadowsocksSettings = ss
		}
	}, "IVCheck")
}

// HandleEditOutboundUoT toggles UoT (reserved for future, currently not in standard SS struct but if needed)
func (h *Handler) HandleEditOutboundUoT(c telebot.Context) error {
	// Not implemented in standard struct yet, or mapped to something else.
	// Assuming skipped or implemented later.
	return c.Send("⚠️ Feature not available in this version.")
}

// HandleEditOutboundUUID
func (h *Handler) HandleEditOutboundUUID(c telebot.Context) error {
	return h.handleEditFieldStart(c, conversation.StateEditOutboundUUID, "Enter new *UUID*:")
}
func (h *Handler) HandleEditOutboundUUIDInput(c telebot.Context) error {
	val := c.Text()
	return h.updateOutboundField(c, func(outbound *domain.Outbound) {
		if outbound.Protocol == "vmess" {
			s := outbound.GetVMessSettingsOrDefault()
			s.UUID = val
			outbound.VMessSettings = s
		} else if outbound.Protocol == "vless" {
			s := outbound.GetVLESSSettingsOrDefault()
			s.UUID = val
			outbound.VLESSSettings = s
		}
	}, "UUID")
}

// HandleEditOutboundPassword
func (h *Handler) HandleEditOutboundPassword(c telebot.Context) error {
	return h.handleEditFieldStart(c, conversation.StateEditOutboundPassword, "Enter new *Password*:")
}
func (h *Handler) HandleEditOutboundPasswordInput(c telebot.Context) error {
	val := c.Text()
	return h.updateOutboundField(c, func(outbound *domain.Outbound) {
		switch outbound.Protocol {
		case "trojan":
			s := outbound.GetTrojanSettingsOrDefault()
			s.Password = val
			outbound.TrojanSettings = s
		case "shadowsocks":
			s := outbound.GetShadowsocksSettingsOrDefault()
			s.Password = val
			outbound.ShadowsocksSettings = s
		case "socks":
			s := outbound.GetSOCKSSettingsOrDefault()
			if len(s.Accounts) > 0 {
				s.Accounts[0].Pass = val
			} else {
				// accounts list is empty, seed with defaults
				user := "user"
				s.Accounts = []domain.SOCKSAccount{{User: user, Pass: val}}
				s.Auth = "password"
			}
			outbound.SOCKSSettings = s
		case "http":
			hSet := outbound.GetHTTPSettingsOrDefault()
			if len(hSet.Accounts) > 0 {
				hSet.Accounts[0].Pass = val
			} else {
				hSet.Accounts = []domain.HTTPAccount{{User: "user", Pass: val}}
			}
			outbound.HTTPSettings = hSet
		}
	}, "Password")
}

// HandleEditOutboundUsername
func (h *Handler) HandleEditOutboundUsername(c telebot.Context) error {
	return h.handleEditFieldStart(c, conversation.StateEditOutboundUsername, "Enter new *Username*:")
}
func (h *Handler) HandleEditOutboundUsernameInput(c telebot.Context) error {
	val := c.Text()
	return h.updateOutboundField(c, func(outbound *domain.Outbound) {
		switch outbound.Protocol {
		case "socks":
			s := outbound.GetSOCKSSettingsOrDefault()
			if len(s.Accounts) > 0 {
				s.Accounts[0].User = val
			} else {
				s.Accounts = []domain.SOCKSAccount{{User: val, Pass: "password"}}
				s.Auth = "password"
			}
			outbound.SOCKSSettings = s
		case "http":
			hSet := outbound.GetHTTPSettingsOrDefault()
			if len(hSet.Accounts) > 0 {
				hSet.Accounts[0].User = val
			} else {
				hSet.Accounts = []domain.HTTPAccount{{User: val, Pass: "password"}}
			}
			outbound.HTTPSettings = hSet
		}
	}, "Username")
}

// HandleEditOutboundFlow
func (h *Handler) HandleEditOutboundFlow(c telebot.Context) error {
	return h.handleEditFieldStart(c, conversation.StateEditOutboundFlow, "Enter new *Flow* (e.g. `xtls-rprx-vision` or empty):")
}
func (h *Handler) HandleEditOutboundFlowInput(c telebot.Context) error {
	val := c.Text()
	return h.updateOutboundField(c, func(outbound *domain.Outbound) {
		if outbound.Protocol == "vless" {
			s := outbound.GetVLESSSettingsOrDefault()
			s.Flow = val
			outbound.VLESSSettings = s
		}
	}, "Flow")
}

// HandleEditOutboundMethod
func (h *Handler) HandleEditOutboundMethod(c telebot.Context) error {
	return h.handleEditFieldStart(c, conversation.StateEditOutboundMethod, "Enter new *Method* (e.g. `aes-256-gcm`):")
}
func (h *Handler) HandleEditOutboundMethodInput(c telebot.Context) error {
	val := c.Text()
	return h.updateOutboundField(c, func(outbound *domain.Outbound) {
		if outbound.Protocol == "shadowsocks" {
			s := outbound.GetShadowsocksSettingsOrDefault()
			s.Method = val
			outbound.ShadowsocksSettings = s
		}
	}, "Method")
}

// updateOutboundField Generic Helper
func (h *Handler) updateOutboundField(c telebot.Context, updater func(*domain.Outbound), logLabel string) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	idStr := h.stateManager.GetStringData(userID, "outbound_id")
	// If idStr is empty, try to extract from callback data (for toggle buttons)
	if idStr == "" {
		// Try parsing from callback if possible, but usually state carries it
		// For toggle buttons, id is in c.Data often wrapped in h.extractOutboundID
		idStr = h.extractOutboundID(c)
	}

	id, _ := strconv.ParseUint(idStr, 10, 32)
	if id == 0 {
		return c.Send("❌ Error: Invalid ID or Session. Try again.")
	}

	outbound, err := h.nodeUsecase.GetOutbound(ctx, uint(id))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Outbound not found")
	}

	updater(outbound)

	if err := h.nodeUsecase.UpdateOutbound(ctx, outbound); err != nil {
		return c.Send("❌ Update failed: " + err.Error())
	}

	h.stateManager.ResetSession(userID)

	if c.Callback() != nil {
		// It's a button click (toggle)
		c.Callback().Data = idStr
		return h.HandleOutboundEditAdvanced(c)
	}

	// It's text input
	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Outbound", "admin_outbound_edit", idStr)))
	return c.Send(fmt.Sprintf("✅ %s updated!", logLabel), kb)
}

// HandleToggleOutboundTFO toggles sockopt TFO
func (h *Handler) HandleToggleOutboundTFO(c telebot.Context) error {
	return h.toggleSockoptBool(c, func(s *domain.SockoptSettings) { s.TcpFastOpen = !s.TcpFastOpen })
}

// HandleToggleOutboundMPTCP toggles sockopt MPTCP
func (h *Handler) HandleToggleOutboundMPTCP(c telebot.Context) error {
	return h.toggleSockoptBool(c, func(s *domain.SockoptSettings) { s.TcpMptcp = !s.TcpMptcp })
}

// HandleSetOutboundTProxy handles TProxy selection
func (h *Handler) HandleSetOutboundTProxy(c telebot.Context) error {
	utils.AnswerCallback(c, "Updating...")
	parts := strings.Split(c.Data(), "|")
	if len(parts) < 2 {
		return nil
	}
	idStr := parts[0]
	val := parts[1]

	return h.updateSockopt(c, idStr, func(s *domain.SockoptSettings) { s.Tproxy = val })
}

// Sockopt Helpers

func (h *Handler) toggleSockoptBool(c telebot.Context, toggler func(*domain.SockoptSettings)) error {
	utils.AnswerCallback(c)
	idStr := h.extractOutboundID(c)
	return h.updateSockopt(c, idStr, toggler)
}

func (h *Handler) updateSockopt(c telebot.Context, idStr string, updater func(*domain.SockoptSettings)) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	id, _ := strconv.ParseUint(idStr, 10, 32)

	outbound, err := h.nodeUsecase.GetOutbound(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Outbound not found")
	}

	ss := outbound.GetSockoptSettingsOrDefault()
	if ss == nil {
		ss = &domain.SockoptSettings{}
	}

	updater(ss)
	outbound.SockoptSettings = ss

	if err := h.nodeUsecase.UpdateOutbound(ctx, outbound); err != nil {
		return c.Edit("❌ Failed: " + err.Error())
	}

	c.Callback().Data = idStr
	return h.HandleOutboundEditSockopt(c)
}

func (h *Handler) extractOutboundID(c telebot.Context) string {
	// Usually passed directly in Data
	return c.Data()
}

// HandleEditOutboundRemark handles remark edit
func (h *Handler) HandleEditOutboundRemark(c telebot.Context) error {
	utils.AnswerCallback(c)
	id := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateEditOutboundRemark)
	h.stateManager.SetData(userID, "outbound_id", id)

	return c.Edit("Enter new *Remark/Label*:", telebot.ModeMarkdown, keyboards.CancelInline())
}

func (h *Handler) HandleEditOutboundRemarkInput(c telebot.Context) error {
	userID := c.Sender().ID
	remark := c.Text()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	idStr := h.stateManager.GetStringData(userID, "outbound_id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	outbound, err := h.nodeUsecase.GetOutbound(ctx, uint(id))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Outbound not found")
	}

	outbound.Remark = remark
	h.nodeUsecase.UpdateOutbound(ctx, outbound)
	h.stateManager.ResetSession(userID)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Outbound", "admin_outbound_view", idStr)))

	return c.Send("✅ Remark updated!", kb)
}

// HandleEditOutboundAddress handles address edit
func (h *Handler) HandleEditOutboundAddress(c telebot.Context) error {
	utils.AnswerCallback(c)
	id := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateEditOutboundAddress)
	h.stateManager.SetData(userID, "outbound_id", id)

	return c.Edit("Enter new *Server Address*:", telebot.ModeMarkdown, keyboards.CancelInline())
}

func (h *Handler) HandleEditOutboundAddressInput(c telebot.Context) error {
	userID := c.Sender().ID
	address := c.Text()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	idStr := h.stateManager.GetStringData(userID, "outbound_id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	outbound, err := h.nodeUsecase.GetOutbound(ctx, uint(id))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Outbound not found")
	}

	outbound.Address = address
	h.nodeUsecase.UpdateOutbound(ctx, outbound)
	h.stateManager.ResetSession(userID)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Outbound", "admin_outbound_view", idStr)))

	return c.Send("✅ Address updated!", kb)
}

// HandleEditOutboundPort handles port edit
func (h *Handler) HandleEditOutboundPort(c telebot.Context) error {
	utils.AnswerCallback(c)
	id := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateEditOutboundPort)
	h.stateManager.SetData(userID, "outbound_id", id)

	return c.Edit("Enter new *Port*:", telebot.ModeMarkdown, keyboards.CancelInline())
}

func (h *Handler) HandleEditOutboundPortInput(c telebot.Context) error {
	userID := c.Sender().ID
	portStr := c.Text()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return c.Send("❌ Invalid port. Please enter a number:")
	}

	idStr := h.stateManager.GetStringData(userID, "outbound_id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	outbound, err := h.nodeUsecase.GetOutbound(ctx, uint(id))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Outbound not found")
	}

	outbound.Port = port
	h.nodeUsecase.UpdateOutbound(ctx, outbound)
	h.stateManager.ResetSession(userID)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Outbound", "admin_outbound_view", idStr)))

	return c.Send("✅ Port updated!", kb)
}

// HandleEditOutboundProtocol shows protocol selection
func (h *Handler) HandleEditOutboundProtocol(c telebot.Context) error {
	utils.AnswerCallback(c)
	id := c.Data()

	msg := "⚙️ *Edit Protocol*\n\nChoose new protocol (Note: this may reset some settings):"
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("🌐 Freedom", "edit_outbound_proto_save", id, "freedom")),
		kb.Row(kb.Data("🕳 Blackhole", "edit_outbound_proto_save", id, "blackhole")),
		kb.Row(kb.Data("⚡ VLESS", "edit_outbound_proto_save", id, "vless")),
		kb.Row(kb.Data("📡 VMess", "edit_outbound_proto_save", id, "vmess")),
		kb.Row(kb.Data("🔐 Trojan", "edit_outbound_proto_save", id, "trojan")),
		kb.Row(kb.Data("🧦 SOCKS", "edit_outbound_proto_save", id, "socks")),
		kb.Row(kb.Data("👻 Shadowsocks", "edit_outbound_proto_save", id, "shadowsocks")),
		kb.Row(kb.Data("🔙 Cancel", "admin_outbound_edit", id)),
	)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleEditOutboundProtocolSave updates the protocol
func (h *Handler) HandleEditOutboundProtocolSave(c telebot.Context) error {
	data, ok := utils.SplitCallback(c, "|", 2)
	if !ok {
		return nil
	}
	idStr := data[0]
	newProto := data[1]

	id, _ := strconv.ParseUint(idStr, 10, 32)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	outbound, err := h.nodeUsecase.GetOutbound(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Outbound not found")
	}

	outbound.Protocol = newProto
	if err := h.nodeUsecase.UpdateOutbound(ctx, outbound); err != nil {
		return c.Edit("❌ Failed to update: " + err.Error())
	}

	utils.AnswerCallback(c, "Protocol updated!")
	c.Callback().Data = idStr
	return h.HandleOutboundEdit(c)
}

// Generic Edit Handler Helper
func (h *Handler) handleEditFieldStart(c telebot.Context, state conversation.ConversationState, prompt string) error {
	utils.AnswerCallback(c)
	id := c.Data()
	userID := c.Sender().ID
	h.stateManager.StartConversation(userID, state)
	h.stateManager.SetData(userID, "outbound_id", id)
	return c.Edit(prompt, telebot.ModeMarkdown, &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{{
			telebot.InlineButton{Text: "🔙 Cancel", Data: "admin_outbound_edit|" + id},
		}},
	})
}

// HandleEditOutboundNetwork shows network selection
func (h *Handler) HandleEditOutboundNetwork(c telebot.Context) error {
	utils.AnswerCallback(c)
	id := c.Data()

	msg := "📡 *Edit Network*\n\nChoose new transport network:"
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("TCP", "edit_outbound_net_save", id, "tcp")),
		kb.Row(kb.Data("WebSocket", "edit_outbound_net_save", id, "ws")),
		kb.Row(kb.Data("gRPC", "edit_outbound_net_save", id, "grpc")),
		kb.Row(kb.Data("xHTTP", "edit_outbound_net_save", id, "xhttp")),
		kb.Row(kb.Data("🔙 Cancel", "admin_outbound_edit", id)),
	)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleEditOutboundNetworkSave
func (h *Handler) HandleEditOutboundNetworkSave(c telebot.Context) error {
	data, ok := utils.SplitCallback(c, "|", 2)
	if !ok {
		return nil
	}
	idStr := data[0]
	newNet := data[1]

	id, _ := strconv.ParseUint(idStr, 10, 32)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	outbound, err := h.nodeUsecase.GetOutbound(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Outbound not found")
	}

	outbound.Network = newNet
	h.nodeUsecase.UpdateOutbound(ctx, outbound)

	utils.AnswerCallback(c, "Network updated!")
	c.Callback().Data = idStr
	return h.HandleOutboundEdit(c)
}

// HandleEditOutboundSecurity shows security selection
func (h *Handler) HandleEditOutboundSecurity(c telebot.Context) error {
	utils.AnswerCallback(c)
	id := c.Data()

	msg := "🔒 *Edit Security*\n\nChoose security type (Note: TLS/Reality require extra settings):"
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("🔓 None", "edit_outbound_sec_save", id, "none")),
		kb.Row(kb.Data("🔒 TLS", "edit_outbound_sec_save", id, "tls")),
		kb.Row(kb.Data("🛡 Reality", "edit_outbound_sec_save", id, "reality")),
		kb.Row(kb.Data("🔙 Cancel", "admin_outbound_edit", id)),
	)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleEditOutboundSecuritySave
func (h *Handler) HandleEditOutboundSecuritySave(c telebot.Context) error {
	data, ok := utils.SplitCallback(c, "|", 2)
	if !ok {
		return nil
	}
	idStr := data[0]
	newSec := data[1]

	id, _ := strconv.ParseUint(idStr, 10, 32)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	outbound, err := h.nodeUsecase.GetOutbound(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Outbound not found")
	}

	outbound.Security = newSec
	h.nodeUsecase.UpdateOutbound(ctx, outbound)

	utils.AnswerCallback(c, "Security updated!")
	c.Callback().Data = idStr
	return h.HandleOutboundEdit(c)
}
