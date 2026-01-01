package telegram

import (
	"fmt"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/link"
	"gopkg.in/telebot.v3"
)

// === Outbound Management ===

func (h *Handler) HandleOutbounds(c telebot.Context) error {
	utils.AnswerCallback(c)
	nodeID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	node, err := h.nodeUsecase.GetNode(ctx, nodeID)
	if err != nil {
		return c.Edit("❌ Node not found")
	}

	outbounds, err := h.nodeUsecase.ListOutbounds(ctx, nodeID)
	if err != nil {
		return c.Edit("❌ Failed to list outbounds")
	}

	msg := fmt.Sprintf("📤 *Outbounds for %s*\n\nSelect an outbound to view or delete.", node.Name)
	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	for _, out := range outbounds {
		// e.g. "freedom (direct) - Direct Out"
		label := fmt.Sprintf("%s - %s", out.Protocol, out.Tag)
		if out.Remark != "" {
			label = fmt.Sprintf("%s (%s)", out.Protocol, out.Remark)
		}
		rows = append(rows, kb.Row(kb.Data(label, "admin_outbound_view", fmt.Sprintf("%d", out.ID))))
	}

	rows = append(rows, kb.Row(
		kb.Data("➕ Add Outbound", "admin_outbound_add", fmt.Sprintf("%d", node.ID)),
		kb.Data("🔗 Import", "admin_outbound_import", fmt.Sprintf("%d", node.ID)),
	))
	rows = append(rows, kb.Row(
		kb.Data("🔍 Discover", "admin_outbound_discover", fmt.Sprintf("%d", node.ID)),
		kb.Data("🔄 Sync", "admin_outbound_sync", fmt.Sprintf("%d", node.ID)),
	))
	rows = append(rows, kb.Row(kb.Data("🔙 Back to Node", "admin_node_view", fmt.Sprintf("%d", node.ID))))

	kb.Inline(rows...)
	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleOutboundView(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	outbound, err := h.nodeUsecase.GetOutbound(ctx, id)
	if err != nil {
		return c.Edit("❌ Outbound not found")
	}

	tlsSettings := outbound.GetTLSSettingsOrDefault()
	realitySettings := outbound.GetRealitySettingsOrDefault()
	transportSettings := outbound.GetTransportSettingsOrDefault()
	sockoptSettings := outbound.GetSockoptSettingsOrDefault()

	// --- Dynamic Message Building ---
	var sb strings.Builder
	sb.WriteString("📤 *Outbound Details*\n━━━━━━━━━━━━━━━━━━━━━━\n")

	// Basic Info Section
	sb.WriteString("📋 *Basic Info*\n")
	sb.WriteString(fmt.Sprintf("┣ 🏷 Tag: `%s`\n", outbound.Tag))
	sb.WriteString(fmt.Sprintf("┣ ⚙️ Protocol: `%s`\n", strings.ToUpper(outbound.Protocol)))
	sb.WriteString(fmt.Sprintf("┗ 📝 Remark: %s\n", outbound.Remark))

	// Direct Protocols (Freedom/Blackhole)
	if outbound.Protocol == "freedom" || outbound.Protocol == "blackhole" {
		sb.WriteString("\n🎯 *Mode*\n")
		if outbound.Protocol == "freedom" {
			fs := outbound.GetFreedomSettingsOrDefault()
			strategy := fs.DomainStrategy
			if strategy == "" {
				strategy = "AsIs"
			}
			sb.WriteString(fmt.Sprintf("┗ 🧠 Strategy: `%s`\n", strategy))
			if fs.Redirect != "" {
				sb.WriteString(fmt.Sprintf("  ↪️ Redirect: `%s`\n", fs.Redirect))
			}
		} else {
			sb.WriteString("┗ 🕳 Drops all traffic\n")
		}
	} else {
		// Proxy Protocols - Server Section
		sb.WriteString("\n🌐 *Server*\n")
		sb.WriteString(fmt.Sprintf("┣ 📍 Address: `%s`\n", outbound.Address))
		sb.WriteString(fmt.Sprintf("┗ 🔢 Port: `%d`\n", outbound.Port))

		// Authentication Section
		sb.WriteString("\n🔐 *Authentication*\n")
		switch outbound.Protocol {
		case "vmess":
			v := outbound.GetVMessSettingsOrDefault()
			sb.WriteString(fmt.Sprintf("┣ 🆔 UUID: `%s`\n", v.UUID))
			sb.WriteString(fmt.Sprintf("┗ 🔒 Security: `%s`\n", v.Security))
		case "vless":
			v := outbound.GetVLESSSettingsOrDefault()
			sb.WriteString(fmt.Sprintf("┣ 🆔 UUID: `%s`\n", v.UUID))
			if v.Flow != "" {
				sb.WriteString(fmt.Sprintf("┣ 🌊 Flow: `%s`\n", v.Flow))
			}
			if v.Encryption != "" {
				sb.WriteString(fmt.Sprintf("┗ 🔒 Encryption: `%s`\n", v.Encryption))
			} else {
				sb.WriteString("┗ ─\n")
			}
		case "trojan":
			t := outbound.GetTrojanSettingsOrDefault()
			sb.WriteString(fmt.Sprintf("┗ 🔑 Password: `%s`\n", t.Password))
		case "shadowsocks":
			ss := outbound.GetShadowsocksSettingsOrDefault()
			sb.WriteString(fmt.Sprintf("┣ 🔒 Method: `%s`\n", ss.Method))
			sb.WriteString(fmt.Sprintf("┗ 🔑 Password: `%s`\n", ss.Password))
		case "socks":
			s := outbound.GetSOCKSSettingsOrDefault()
			if len(s.Accounts) > 0 {
				sb.WriteString(fmt.Sprintf("┣ 👤 User: `%s`\n", s.Accounts[0].User))
				sb.WriteString(fmt.Sprintf("┗ 🔑 Pass: `%s`\n", s.Accounts[0].Pass))
			} else {
				sb.WriteString("┗ 🔓 No auth\n")
			}
		case "http":
			httpSettings := outbound.GetHTTPSettingsOrDefault()
			if len(httpSettings.Accounts) > 0 {
				sb.WriteString(fmt.Sprintf("┣ 👤 User: `%s`\n", httpSettings.Accounts[0].User))
				sb.WriteString(fmt.Sprintf("┗ 🔑 Pass: `%s`\n", httpSettings.Accounts[0].Pass))
			} else {
				sb.WriteString("┗ 🔓 No auth\n")
			}
		}

		// Network & Transport Section
		if outbound.Network != "" && outbound.Network != "tcp" {
			sb.WriteString("\n📡 *Transport*\n")
			sb.WriteString(fmt.Sprintf("┣ 🌐 Network: `%s`\n", outbound.Network))
			if transportSettings != nil {
				if transportSettings.Path != "" {
					sb.WriteString(fmt.Sprintf("┣ 📂 Path: `%s`\n", transportSettings.Path))
				}
				if transportSettings.Host != "" {
					sb.WriteString(fmt.Sprintf("┣ 🏠 Host: `%s`\n", transportSettings.Host))
				}
				if transportSettings.ServiceName != "" {
					sb.WriteString(fmt.Sprintf("┣ 🔧 Service: `%s`\n", transportSettings.ServiceName))
				}
				if transportSettings.Mode != "" {
					sb.WriteString(fmt.Sprintf("┣ ⚙️ Mode: `%s`\n", transportSettings.Mode))
				}
			}
			sb.WriteString("┗ ─\n")
		}

		// Security Section
		if outbound.Security != "" && outbound.Security != "none" {
			sb.WriteString("\n🔒 *Security*\n")
			sb.WriteString(fmt.Sprintf("┣ 🛡 Type: `%s`\n", strings.ToUpper(outbound.Security)))

			if outbound.Security == "tls" && tlsSettings != nil {
				if tlsSettings.ServerName != "" {
					sb.WriteString(fmt.Sprintf("┣ 📜 SNI: `%s`\n", tlsSettings.ServerName))
				}
				if tlsSettings.Fingerprint != "" {
					sb.WriteString(fmt.Sprintf("┣ 🖐 FP: `%s`\n", tlsSettings.Fingerprint))
				}
				if len(tlsSettings.ALPN) > 0 {
					sb.WriteString(fmt.Sprintf("┣ 📋 ALPN: `%s`\n", strings.Join(tlsSettings.ALPN, ",")))
				}
			}

			if outbound.Security == "reality" && realitySettings != nil {
				if realitySettings.ServerName != "" {
					sb.WriteString(fmt.Sprintf("┣ 📜 SNI: `%s`\n", realitySettings.ServerName))
				}
				if realitySettings.Fingerprint != "" {
					sb.WriteString(fmt.Sprintf("┣ 🖐 FP: `%s`\n", realitySettings.Fingerprint))
				}
				if realitySettings.PublicKey != "" {
					pk := realitySettings.PublicKey
					if len(pk) > 16 {
						pk = pk[:16] + "..."
					}
					sb.WriteString(fmt.Sprintf("┣ 🔑 PubKey: `%s`\n", pk))
				}
				if realitySettings.ShortID != "" {
					sb.WriteString(fmt.Sprintf("┣ 🆔 ShortID: `%s`\n", realitySettings.ShortID))
				}
			}
			sb.WriteString("┗ ─\n")
		}

		// Sockopt Section (only if configured)
		if sockoptSettings != nil && (sockoptSettings.Mark > 0 || sockoptSettings.Interface != "" || sockoptSettings.DialerProxy != "") {
			sb.WriteString("\n⚙️ *Sockopt*\n")
			if sockoptSettings.Mark > 0 {
				sb.WriteString(fmt.Sprintf("┣ 📌 Mark: `%d`\n", sockoptSettings.Mark))
			}
			if sockoptSettings.Interface != "" {
				sb.WriteString(fmt.Sprintf("┣ 🔌 Interface: `%s`\n", sockoptSettings.Interface))
			}
			if sockoptSettings.DialerProxy != "" {
				sb.WriteString(fmt.Sprintf("┣ ↩️ Dialer: `%s`\n", sockoptSettings.DialerProxy))
			}
			sb.WriteString("┗ ─\n")
		}
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("✏️ Edit", "admin_outbound_edit", fmt.Sprintf("%d", outbound.ID))),
		kb.Row(
			kb.Data("🔌 Test", "admin_outbound_test", fmt.Sprintf("%d", outbound.ID)),
			kb.Data("📤 Export", "admin_outbound_export", fmt.Sprintf("%d", outbound.ID)),
		),
		kb.Row(kb.Data("🗑 Delete", "admin_outbound_delete", fmt.Sprintf("%d", outbound.ID))),
		keyboards.BackRowID(kb, "admin_node_outbounds", outbound.NodeID),
	)

	return c.Edit(sb.String(), telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleDeleteOutbound(c telebot.Context) error {
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	out, err := h.nodeUsecase.GetOutbound(ctx, id)
	if err != nil {
		return utils.AnswerCallback(c, "Error: Outbound not found")
	}

	h.nodeUsecase.DeleteOutbound(ctx, id)
	utils.AnswerCallback(c, "Outbound Deleted")

	c.Callback().Data = fmt.Sprintf("%d", out.NodeID)
	return h.HandleOutbounds(c)
}

// HandleOutboundExportLink generates a shareable link for the outbound
func (h *Handler) HandleOutboundExportLink(c telebot.Context) error {
	utils.AnswerCallback(c)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	outbound, err := h.nodeUsecase.GetOutbound(ctx, id)
	if err != nil {
		return c.Send("❌ Outbound not found")
	}

	shareLink, err := link.Generate(outbound)
	if err != nil {
		return c.Send(fmt.Sprintf("❌ Cannot generate link for this protocol: %v", err))
	}

	msg := fmt.Sprintf("📤 *Share Link for %s*\n\n`%s`\n\n_Tap to copy_", outbound.Remark, shareLink)
	return c.Send(msg, telebot.ModeMarkdown)
}

// HandleOutboundTestConnectivity tests the connectivity of an outbound via the agent
func (h *Handler) HandleOutboundTestConnectivity(c telebot.Context) error {
	utils.AnswerCallback(c, "🔌 Testing connectivity...")
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 30*time.Second)
	defer cancel()

	result, err := h.nodeUsecase.TestOutbound(ctx, id, "")
	if err != nil {
		msg := fmt.Sprintf("❌ *Test Failed*\n\n❗ *Error:* `%s`", err.Error())
		kb := &telebot.ReplyMarkup{}
		kb.Inline(keyboards.BackRowID(kb, "admin_outbound_view", id))
		return c.Send(msg, telebot.ModeMarkdown, kb)
	}

	var msg string
	if result.Success {
		msg = fmt.Sprintf("✅ *Connectivity Test Passed*\n\n"+
			"⏱ *Latency:* `%dms`\n"+
			"📡 *Status:* `HTTP %d`",
			result.LatencyMs, result.StatusCode)
		if result.IP != "" {
			msg += fmt.Sprintf("\n🌐 *Exit IP:* `%s`", result.IP)
		}
		if result.Message != "" {
			msg += fmt.Sprintf("\n💬 `%s`", result.Message)
		}
	} else {
		msg = fmt.Sprintf("❌ *Connectivity Test Failed*\n\n"+
			"❗ *Error:* `%s`", result.Error)
		if result.LatencyMs > 0 {
			msg += fmt.Sprintf("\n⏱ *Timeout after:* `%dms`", result.LatencyMs)
		}
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(keyboards.BackRowID(kb, "admin_outbound_view", id))
	return c.Send(msg, telebot.ModeMarkdown, kb)
}

// HandleDiscoverOutbounds fetches outbounds from Xray and adds new ones to DB
func (h *Handler) HandleDiscoverOutbounds(c telebot.Context) error {
	utils.AnswerCallback(c, "🔍 Discovering outbounds...")
	nodeID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 2*time.Minute)
	defer cancel()

	c.Edit("🔍 *Discovering outbounds...*\n\nConnecting to Xray API to import unknown outbound tags.", telebot.ModeMarkdown)

	newOutbounds, err := h.nodeUsecase.DiscoverOutbounds(ctx, nodeID)
	if err != nil {
		return c.Edit(fmt.Sprintf("❌ *Discovery Failed*\n\n%s", err.Error()), telebot.ModeMarkdown)
	}

	if len(newOutbounds) == 0 {
		utils.AnswerCallbackWithAlert(c, "No new outbounds found.")
		c.Callback().Data = fmt.Sprintf("%d", nodeID)
		return h.HandleOutbounds(c)
	}

	msg := fmt.Sprintf("✅ *Discovered %d new outbound(s):*\n\n", len(newOutbounds))
	for _, out := range newOutbounds {
		msg += fmt.Sprintf("• `%s` (%s)\n", out.Tag, out.Protocol)
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Outbounds", "admin_node_outbounds", fmt.Sprintf("%d", nodeID))))

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleSyncOutbounds enforces DB state onto Xray
func (h *Handler) HandleSyncOutbounds(c telebot.Context) error {
	utils.AnswerCallback(c, "🔄 Syncing outbounds...")
	nodeID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 2*time.Minute)
	defer cancel()

	c.Edit("🔄 *Syncing outbounds...*\n\nEnforcing database state onto Xray server...", telebot.ModeMarkdown)

	result, err := h.nodeUsecase.SyncOutbounds(ctx, nodeID)
	if err != nil {
		return c.Edit(fmt.Sprintf("❌ *Sync Failed*\n\n%s", err.Error()), telebot.ModeMarkdown)
	}

	msg := fmt.Sprintf("✅ *Outbound Sync Complete!*\n\n"+
		"📥 *Restored (Pushed):* %d\n"+
		"📦 *Imported (New):* %d\n"+
		"✨ *Kept:* %d\n"+
		"⚠️ *Errors:* %d",
		result.Restored, result.Imported, result.Kept, result.Errors)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Outbounds", "admin_node_outbounds", fmt.Sprintf("%d", nodeID))))

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}
