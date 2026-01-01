package telegram

import (
	"fmt"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"gopkg.in/telebot.v3"
)

// HandleDiscoverInbounds fetches inbounds from Xray and adds new ones to DB
func (h *Handler) HandleDiscoverInbounds(c telebot.Context) error {
	utils.AnswerCallback(c, "🔍 Connecting...")
	nodeID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 2*time.Minute)
	defer cancel()

	c.Edit("🔍 *Discovering inbounds...*\n\nConnecting to Xray API to import unknown tags.", telebot.ModeMarkdown)

	newInbounds, err := h.nodeUsecase.DiscoverInbounds(ctx, nodeID)
	if err != nil {
		return c.Edit(fmt.Sprintf("❌ *Discovery Failed*\n\n%s", err.Error()), telebot.ModeMarkdown)
	}

	if len(newInbounds) == 0 {
		utils.AnswerCallbackWithAlert(c, "No new inbounds found.")
		return h.HandleNodeView(c)
	}

	msg := fmt.Sprintf("✅ *Discovered %d new inbound(s):*\n\n", len(newInbounds))
	for _, in := range newInbounds {
		msg += fmt.Sprintf("• `%s` (%s, port %d)\n", in.Tag, in.Protocol, in.Port)
	}
	msg += "\n⚠️ *Note:* Imported inbounds may need editing to set correct Link Formats."

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Node", "admin_node_view", fmt.Sprintf("%d", nodeID))))

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleSyncInbounds enforces DB state onto Xray (Restart Recovery)
func (h *Handler) HandleSyncInbounds(c telebot.Context) error {
	utils.AnswerCallback(c, "🔄 Syncing...")
	nodeID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 2*time.Minute)
	defer cancel()

	c.Edit("🔄 *Syncing inbounds...*\n\nEnforcing database state onto Xray server...", telebot.ModeMarkdown)

	result, err := h.nodeUsecase.SyncInbounds(ctx, nodeID)
	if err != nil {
		return c.Edit(fmt.Sprintf("❌ *Sync Failed*\n\n%s", err.Error()), telebot.ModeMarkdown)
	}

	msg := fmt.Sprintf("✅ *Sync Complete!*\n\n"+
		"📥 *Restored (Pushed):* %d\n"+
		"📦 *Imported (New):* %d\n"+
		"✨ *Kept:* %d\n"+
		"⚠️ *Errors:* %d",
		result.Restored, result.Imported, result.Kept, result.Errors)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Node", "admin_node_view", fmt.Sprintf("%d", nodeID))))

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// === Inbound Management ===

func (h *Handler) HandleInbounds(c telebot.Context) error {
	utils.AnswerCallback(c)
	nodeID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	node, err := h.nodeUsecase.GetNode(ctx, nodeID)
	if err != nil {
		return c.Edit("❌ Node not found")
	}

	msg := fmt.Sprintf("📡 *Inbounds for %s*\n\nSelect an inbound to edit or delete.", node.Name)
	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	for _, in := range node.Inbounds {
		// e.g. "vmess (443) - High Speed"
		label := fmt.Sprintf("%s (%d) - %s", in.Protocol, in.Port, in.Remark)
		rows = append(rows, kb.Row(kb.Data(label, "admin_inbound_view", fmt.Sprintf("%d", in.ID))))
	}

	rows = append(rows, kb.Row(kb.Data("➕ Add Inbound", "admin_inbound_add", fmt.Sprintf("%d", node.ID))))
	rows = append(rows, kb.Row(kb.Data("🔙 Back to Node", "admin_node_view", fmt.Sprintf("%d", node.ID))))

	kb.Inline(rows...)
	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleInboundView(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	inbound, err := h.nodeUsecase.GetInbound(ctx, id)
	if err != nil {
		return c.Edit("❌ Inbound not found")
	}

	// Get settings
	tlsSettings := inbound.GetTLSSettingsOrDefault()
	realitySettings := inbound.GetRealitySettingsOrDefault()
	transportSettings := inbound.GetTransportSettingsOrDefault()
	sniffingSettings := inbound.GetSniffingSettingsOrDefault()

	address := inbound.Address
	if address == "" && inbound.Node != nil {
		address = inbound.Node.IP + " (Node)"
	} else if address == "" {
		address = "(uses Node IP)"
	}

	var sb strings.Builder
	sb.WriteString("📥 *Inbound Details*\n━━━━━━━━━━━━━━━━━━━━━━\n")

	// Basic Info Section
	sb.WriteString("📋 *Basic Info*\n")
	sb.WriteString(fmt.Sprintf("┣ 🏷 Tag: `%s`\n", inbound.Tag))
	sb.WriteString(fmt.Sprintf("┣ ⚙️ Protocol: `%s`\n", strings.ToUpper(inbound.Protocol)))
	sb.WriteString(fmt.Sprintf("┗ 📝 Remark: %s\n", inbound.Remark))

	// Server Section
	sb.WriteString("\n🌐 *Server*\n")
	sb.WriteString(fmt.Sprintf("┣ 🔢 Port: `%d`\n", inbound.Port))
	sb.WriteString(fmt.Sprintf("┗ 📍 Address: `%s`\n", address))

	// Transport Section
	if inbound.Network != "" && inbound.Network != "tcp" {
		sb.WriteString("\n📡 *Transport*\n")
		sb.WriteString(fmt.Sprintf("┣ 🌐 Network: `%s`\n", inbound.Network))
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
	if inbound.Security != "" && inbound.Security != "none" {
		sb.WriteString("\n🔒 *Security*\n")
		sb.WriteString(fmt.Sprintf("┣ 🛡 Type: `%s`\n", strings.ToUpper(inbound.Security)))

		if inbound.Security == "tls" && tlsSettings != nil {
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

		if inbound.Security == "reality" && realitySettings != nil {
			if len(realitySettings.ServerNames) > 0 {
				sb.WriteString(fmt.Sprintf("┣ 📜 SNI: `%s`\n", realitySettings.ServerNames[0]))
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

	// Sniffing Section
	if sniffingSettings != nil && sniffingSettings.Enabled {
		sb.WriteString("\n🔎 *Sniffing*\n")
		sb.WriteString("┣ ✅ Enabled\n")
		if len(sniffingSettings.DestOverride) > 0 {
			sb.WriteString(fmt.Sprintf("┗ 🎯 Override: `%s`\n", strings.Join(sniffingSettings.DestOverride, ",")))
		} else {
			sb.WriteString("┗ ─\n")
		}
	}

	// Link Format (truncated)
	if inbound.LinkFormat != "" {
		linkPreview := inbound.LinkFormat
		if len(linkPreview) > 50 {
			linkPreview = linkPreview[:50] + "..."
		}
		sb.WriteString(fmt.Sprintf("\n🔗 *Link:* `%s`\n", linkPreview))
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("✏️ Edit", "admin_inbound_edit", fmt.Sprintf("%d", inbound.ID)),
			kb.Data("📊 Stats", "admin_inbound_stats", fmt.Sprintf("%d", inbound.ID))),
		kb.Row(kb.Data("🔄 Push Update", "admin_inbound_resync", fmt.Sprintf("%d", inbound.ID))),
		kb.Row(kb.Data("🗑 Delete", "admin_inbound_delete", fmt.Sprintf("%d", inbound.ID))),
		keyboards.BackRowID(kb, "admin_node_inbounds", inbound.NodeID),
	)

	return c.Edit(sb.String(), telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleDeleteInbound(c telebot.Context) error {
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	in, err := h.nodeUsecase.GetInbound(ctx, id)
	if err != nil {
		return utils.AnswerCallback(c, "Error: Inbound not found")
	}

	h.nodeUsecase.DeleteInbound(ctx, id)
	utils.AnswerCallback(c, "Inbound Deleted")

	c.Callback().Data = fmt.Sprintf("%d", in.NodeID)
	return h.HandleInbounds(c)
}

// HandleInboundStats displays traffic statistics for a specific inbound
func (h *Handler) HandleInboundStats(c telebot.Context) error {
	utils.AnswerCallback(c, "📊 Loading stats...")
	inboundID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	inbound, err := h.nodeUsecase.GetInbound(ctx, inboundID)
	if err != nil {
		return c.Edit("❌ Inbound not found")
	}

	stats, err := h.nodeUsecase.GetInboundStats(ctx, inboundID)
	if err != nil {
		msg := fmt.Sprintf("❌ *Failed to get stats for %s*\n\n%s", inbound.Tag, err.Error())
		kb := &telebot.ReplyMarkup{}
		kb.Inline(keyboards.BackRowID(kb, "admin_inbound_view", inboundID))
		return c.Edit(msg, telebot.ModeMarkdown, kb)
	}

	uploadStr := formatBytes(stats.TotalUplink)
	downloadStr := formatBytes(stats.TotalDownlink)

	msg := fmt.Sprintf("📊 *Inbound Stats: %s*\n"+
		"━━━━━━━━━━━━━━━━\n\n"+
		"📊 *Traffic (Cumulative):*\n"+
		"  ↑ Upload: %s\n"+
		"  ↓ Download: %s\n\n"+
		"📡 *Active Users:* %d",
		inbound.Tag, uploadStr, downloadStr, stats.ActiveUsers)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔄 Refresh", "admin_inbound_stats", fmt.Sprintf("%d", inboundID)),
		kb.Data("🔙 Back", "admin_inbound_view", fmt.Sprintf("%d", inboundID))))

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleResyncInbound triggers a forceful push of the current DB config to Xray
func (h *Handler) HandleResyncInbound(c telebot.Context) error {
	utils.AnswerCallback(c, "Pushing config...")
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	inbound, err := h.nodeUsecase.GetInbound(ctx, id)
	if err != nil {
		return c.Send("Error: Inbound not found")
	}

	// Calling UpdateInbound without changing fields forces a reconstruction
	if err := h.nodeUsecase.UpdateInbound(ctx, inbound); err != nil {
		return c.Send("❌ Push failed: " + err.Error())
	}

	c.Send("✅ Configuration pushed to Xray server.")
	c.Callback().Data = fmt.Sprintf("%d", id)
	return h.HandleInboundView(c)
}
