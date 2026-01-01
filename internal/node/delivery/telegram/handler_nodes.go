package telegram

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/conversation"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"gopkg.in/telebot.v3"
)

// === Node Management ===

func (h *Handler) HandleNodes(c telebot.Context) error {
	utils.AnswerCallback(c)
	h.stateManager.ResetSession(c.Sender().ID)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	nodes, err := h.nodeUsecase.ListNodes(ctx)
	if err != nil {
		return c.Send("❌ Error fetching nodes: " + err.Error())
	}

	msg := "🌍 *Server Nodes Management*\n\nSelect a node to manage inbounds or check status."
	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	for _, node := range nodes {
		status := "🔴"
		if node.IsActive {
			status = "🟢"
		}
		// e.g. "🟢 🇩🇪 Frankfurt 1 (1.2.3.4)"
		label := fmt.Sprintf("%s %s %s (%s)", status, getFlag(node.CountryCode), node.Name, node.IP)
		rows = append(rows, kb.Row(kb.Data(label, "admin_node_view", fmt.Sprintf("%d", node.ID))))
	}

	rows = append(rows, kb.Row(kb.Data("➕ Add New Node", "admin_node_add")))
	rows = append(rows, kb.Row(kb.Data("🔙 Back to Admin", "back_admin")))

	kb.Inline(rows...)

	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleNodeView(c telebot.Context) error {
	utils.AnswerCallback(c)
	id := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	node, err := h.nodeUsecase.GetNode(ctx, id)
	if err != nil {
		return c.Edit("❌ Node not found")
	}

	health, _ := h.nodeUsecase.CheckNodeHealth(ctx, node.ID)

	healthStatus := "Unknown"
	if health != nil {
		if health.Healthy {
			healthStatus = fmt.Sprintf("✅ Online (%dms)", health.Latency)
		} else {
			healthStatus = "❌ " + health.Message
		}
	}

	// Get outbounds count
	outbounds, _ := h.nodeUsecase.ListOutbounds(ctx, node.ID)
	outboundCount := len(outbounds)

	msg := fmt.Sprintf("🖥 *Node Details: %s*\n"+
		"━━━━━━━━━━━━━━━━\n"+
		"📍 *Location:* %s %s\n"+
		"🏢 *Datacenter:* %s\n"+
		"🌐 *IP:* `%s`\n"+
		"🔌 *API Port:* %d\n"+
		"🚦 *Status:* %s\n\n"+
		"📡 *Inbounds:* %d configured\n"+
		"📤 *Outbounds:* %d configured",
		node.Name, getFlag(node.CountryCode), node.CountryCode,
		node.Datacenter,
		node.IP, node.APIPort,
		healthStatus,
		len(node.Inbounds), outboundCount)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data("📡 Inbounds", "admin_node_inbounds", fmt.Sprintf("%d", node.ID)),
			kb.Data("📤 Outbounds", "admin_node_outbounds", fmt.Sprintf("%d", node.ID)),
		),
		kb.Row(
			kb.Data("📋 Routing Rules", "admin_node_routing", fmt.Sprintf("%d", node.ID)),
		),
		kb.Row(
			kb.Data("📊 View Stats", "admin_node_stats", fmt.Sprintf("%d", node.ID)),
		),
		kb.Row(
			kb.Data("🔍 Discover (Import)", "admin_node_discover", fmt.Sprintf("%d", node.ID)),
			kb.Data("🔄 Sync (Push to Xray)", "admin_node_sync", fmt.Sprintf("%d", node.ID)),
		),
		kb.Row(
			kb.Data("📦 Export Config", "admin_node_export", fmt.Sprintf("%d", node.ID)),
		),
		kb.Row(
			kb.Data("🗑 Delete Node", "admin_node_delete_ask", fmt.Sprintf("%d", node.ID)),
			kb.Data("🔙 Back", "admin_nodes"),
		),
	)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleNodeStats displays Xray statistics for this node
func (h *Handler) HandleNodeStats(c telebot.Context) error {
	utils.AnswerCallback(c, "📊 Loading stats...")
	nodeID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	node, err := h.nodeUsecase.GetNode(ctx, nodeID)
	if err != nil {
		return c.Edit("❌ Node not found")
	}

	stats, err := h.nodeUsecase.GetNodeStats(ctx, nodeID)
	if err != nil {
		msg := fmt.Sprintf("❌ *Failed to get stats for %s*\n\n%s", node.Name, err.Error())
		kb := &telebot.ReplyMarkup{}
		kb.Inline(keyboards.BackRowID(kb, "admin_node_view", nodeID))
		return c.Edit(msg, telebot.ModeMarkdown, kb)
	}

	// Format uptime
	uptimeStr := formatUptime(uint32(stats.SystemUptime))

	// Format traffic
	uploadStr := formatBytes(stats.TotalUplink)
	downloadStr := formatBytes(stats.TotalDownlink)

	msg := fmt.Sprintf("📊 *Server Stats: %s*\n"+
		"━━━━━━━━━━━━━━━━\n\n"+
		"⏱ *Uptime:* %s\n"+
		"📡 *Online Users:* %d\n\n"+
		"📊 *Traffic (Cumulative):*\n"+
		"  ↑ Upload: %s\n"+
		"  ↓ Download: %s",
		node.Name, uptimeStr, stats.OnlineUsers, uploadStr, downloadStr)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔄 Refresh", "admin_node_stats", fmt.Sprintf("%d", nodeID)),
		kb.Data("🔙 Back", "admin_node_view", fmt.Sprintf("%d", nodeID))))

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// formatUptime converts seconds to human-readable format
func formatUptime(seconds uint32) string {
	if seconds == 0 {
		return "N/A"
	}
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// formatBytes converts bytes to human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// === Wizard: Add Node ===

func (h *Handler) HandleAddNodeStart(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID := c.Sender().ID
	h.stateManager.StartConversation(userID, conversation.StateAddNodeName)
	return c.Send("➕ *Add New Node*\n\nStep 1/5: Enter *Name* (e.g. Frankfurt 1):", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddNodeName(c telebot.Context) error {
	userID := c.Sender().ID
	name := c.Text()
	h.stateManager.SetData(userID, "name", name)
	h.stateManager.SetState(userID, conversation.StateAddNodeIP)
	return c.Send("Step 2/5: Enter *IP Address or Domain*:", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddNodeIP(c telebot.Context) error {
	userID := c.Sender().ID
	ip := c.Text()
	h.stateManager.SetData(userID, "ip", ip)
	h.stateManager.SetState(userID, conversation.StateAddNodeCountry)
	return c.Send("Step 3/5: Enter *Country Code* (2 letters, e.g. DE, US):", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddNodeCountry(c telebot.Context) error {
	userID := c.Sender().ID
	cc := strings.ToUpper(c.Text())
	if len(cc) != 2 {
		return c.Send("❌ Invalid code. Please enter 2 letters (e.g. US):")
	}
	h.stateManager.SetData(userID, "country", cc)
	h.stateManager.SetState(userID, conversation.StateAddNodeDatacenter)
	return c.Send("Step 4/5: Enter *Datacenter / Provider* (e.g. Hetzner):", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddNodeDatacenter(c telebot.Context) error {
	userID := c.Sender().ID
	dc := c.Text()
	h.stateManager.SetData(userID, "dc", dc)
	h.stateManager.SetState(userID, conversation.StateAddNodeAPIPort)
	return c.Send("Step 5/5: Enter *Xray API Port* (e.g. 10085):", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAddNodeAPIPort(c telebot.Context) error {
	userID := c.Sender().ID
	port, err := strconv.Atoi(c.Text())
	if err != nil {
		return c.Send("❌ Invalid port. Number required:")
	}
	if port < 1 || port > 65535 {
		return c.Send("❌ Invalid port. Must be between 1 and 65535:")
	}

	name := h.stateManager.GetStringData(userID, "name")
	ip := h.stateManager.GetStringData(userID, "ip")
	country := h.stateManager.GetStringData(userID, "country")
	dc := h.stateManager.GetStringData(userID, "dc")

	h.stateManager.ResetSession(userID)

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	_, err = h.nodeUsecase.CreateNode(ctx, name, ip, country, dc, port, 0, "direct", false, false)
	if err != nil {
		return c.Send("❌ Failed to create node: " + err.Error())
	}

	return c.Send("✅ Node Added Successfully!", keyboards.AdminMenu())
}

func (h *Handler) HandleDeleteNodeAsk(c telebot.Context) error {
	id, err := strconv.ParseUint(c.Data(), 10, 32)
	if err != nil || id == 0 {
		return c.Send("❌ Invalid node ID.")
	}
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	if err := h.nodeUsecase.DeleteNode(ctx, uint(id), false); err != nil {
		return c.Send("❌ Failed to delete node: " + err.Error())
	}
	utils.AnswerCallback(c, "✅ Deleted")
	return h.HandleNodes(c)
}

// HandleExportConfig exports the node's inbounds configuration as a JSON file
func (h *Handler) HandleExportConfig(c telebot.Context) error {
	utils.AnswerCallback(c, "📤 Generating config...")
	nodeID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 2*time.Minute)
	defer cancel()

	node, err := h.nodeUsecase.GetNode(ctx, nodeID)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Node not found"})
	}

	// Build config structure from inbounds
	type ExportedInbound struct {
		Tag               string          `json:"tag"`
		Protocol          string          `json:"protocol"`
		Port              int             `json:"port"`
		Listen            string          `json:"listen,omitempty"`
		Network           string          `json:"network,omitempty"`
		Security          string          `json:"security,omitempty"`
		TransportSettings json.RawMessage `json:"streamSettings,omitempty"`
		TLSSettings       json.RawMessage `json:"tlsSettings,omitempty"`
		RealitySettings   json.RawMessage `json:"realitySettings,omitempty"`
	}

	type ExportedConfig struct {
		NodeName   string             `json:"node_name"`
		NodeIP     string             `json:"node_ip"`
		ExportedAt string             `json:"exported_at"`
		Inbounds   []*ExportedInbound `json:"inbounds"`
	}

	exported := &ExportedConfig{
		NodeName:   node.Name,
		NodeIP:     node.IP,
		ExportedAt: time.Now().Format(time.RFC3339),
		Inbounds:   make([]*ExportedInbound, 0, len(node.Inbounds)),
	}

	for _, inb := range node.Inbounds {
		exp := &ExportedInbound{
			Tag:      inb.Tag,
			Protocol: inb.Protocol,
			Port:     inb.Port,
			Listen:   "0.0.0.0",
			Network:  inb.Network,
			Security: inb.Security,
		}

		if inb.TransportSettings != nil {
			if data, err := json.Marshal(inb.TransportSettings); err == nil {
				exp.TransportSettings = data
			}
		}
		if inb.TLSSettings != nil {
			if data, err := json.Marshal(inb.TLSSettings); err == nil {
				exp.TLSSettings = data
			}
		}
		if inb.RealitySettings != nil {
			if data, err := json.Marshal(inb.RealitySettings); err == nil {
				exp.RealitySettings = data
			}
		}

		exported.Inbounds = append(exported.Inbounds, exp)
	}

	// Marshal to JSON with indentation
	configJSON, err := json.MarshalIndent(exported, "", "  ")
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Failed to generate config"})
	}

	// Send as document
	doc := &telebot.Document{
		File:     telebot.FromReader(strings.NewReader(string(configJSON))),
		FileName: fmt.Sprintf("%s_config.json", strings.ReplaceAll(node.Name, " ", "_")),
		Caption:  fmt.Sprintf("📤 *Config Export: %s*\n\nContains %d inbound(s).", node.Name, len(node.Inbounds)),
	}

	return c.Send(doc, telebot.ModeMarkdown)
}
