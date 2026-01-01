package telegram

import (
	"fmt"
	"strconv"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"gopkg.in/telebot.v3"
)

// === Manual Add User Wizard ===

func (h *Handler) HandleManualAddUserStart(c telebot.Context) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	nodes, err := h.adminUC.ListAllNodes(ctx)
	if err != nil {
		return c.Send("❌ Error listing nodes: " + err.Error())
	}

	if len(nodes) == 0 {
		return c.Send("⚠️ No nodes available.")
	}

	msg := "➕ *Add Manual User*\n\nSelect a node:"
	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, n := range nodes {
		if !n.IsActive {
			continue
		}
		rows = append(rows, kb.Row(kb.Data("🖥 "+n.Name, "manual_node_add", fmt.Sprintf("%d", n.ID))))
	}
	kb.Inline(rows...)

	return c.Send(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleManualAddUserNodeSelect(c telebot.Context) error {
	nodeID, _ := strconv.Atoi(c.Data())
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	nodes, err := h.adminUC.ListAllNodes(ctx)
	if err != nil {
		return c.Edit("❌ Error: " + err.Error())
	}

	var selectedNodeName string
	var inbounds []struct{ Protocol, Tag string }

	for _, n := range nodes {
		if n.ID == uint(nodeID) {
			selectedNodeName = n.Name
			for _, in := range n.Inbounds {
				inbounds = append(inbounds, struct{ Protocol, Tag string }{in.Protocol, in.Tag})
			}
			break
		}
	}

	if selectedNodeName == "" {
		return c.Edit("❌ Node not found.")
	}

	msg := fmt.Sprintf("➕ *Add Manual User* (%s)\n\nSelect an Inbound:", selectedNodeName)
	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, in := range inbounds {
		label := fmt.Sprintf("[%s] %s", in.Protocol, in.Tag)
		rows = append(rows, kb.Row(kb.Data(label, "manual_inbound_add", fmt.Sprintf("%d:%s", nodeID, in.Tag))))
	}
	// rows = append(rows, kb.Row(kb.Data("🔙 Back", "manual_add_user_start")))
	kb.Inline(rows...)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleManualAddUserAskEmail just renders the message asking for email
func (h *Handler) HandleManualAddUserAskEmail(c telebot.Context, nodeID, inboundTag string) error {
	return c.Edit(fmt.Sprintf("📝 *Enter Email/Username*\n\nNode ID: %s\nInbound: `%s`\n\nType the email/username for the new user:", nodeID, inboundTag), telebot.ModeMarkdown, keyboards.CancelInline())
}

// HandleManualAddUserExecute performs the action
func (h *Handler) HandleManualAddUserExecute(c telebot.Context, nodeIDStr, inboundTag, email string) error {
	nodeID, _ := strconv.Atoi(nodeIDStr)

	c.Send("⏳ Creating user...", telebot.ModeMarkdown)

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	user, link, err := h.adminUC.AddUserToInbound(ctx, uint(nodeID), inboundTag, email)
	if err != nil {
		return c.Send("❌ Failed: " + err.Error())
	}

	msg := fmt.Sprintf("✅ *User Created Successfully*\n\n"+
		"📧 Email: `%s`\n"+
		"🆔 UUID: `%s`\n"+
		"🔗 *Link:*\n`%s`",
		user.Email, user.UUID, link)

	return c.Send(msg, telebot.ModeMarkdown, keyboards.AdminMenu())
}

// === Manual Get Link Wizard ===

func (h *Handler) HandleManualGetLinkStart(c telebot.Context) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	nodes, err := h.adminUC.ListAllNodes(ctx)
	if err != nil {
		return c.Send("❌ Error: " + err.Error())
	}

	msg := "🔗 *Generate Custom Link*\n\nSelect a node:"
	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, n := range nodes {
		if !n.IsActive {
			continue
		}
		rows = append(rows, kb.Row(kb.Data("🖥 "+n.Name, "manual_node_link", fmt.Sprintf("%d", n.ID))))
	}
	kb.Inline(rows...)

	return c.Send(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleManualGetLinkNodeSelect(c telebot.Context) error {
	nodeID, _ := strconv.Atoi(c.Data())
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	nodes, err := h.adminUC.ListAllNodes(ctx)
	if err != nil {
		return c.Edit("❌ Error: " + err.Error())
	}

	var selectedNodeName string
	var inbounds []struct{ Protocol, Tag string }

	for _, n := range nodes {
		if n.ID == uint(nodeID) {
			selectedNodeName = n.Name
			for _, in := range n.Inbounds {
				inbounds = append(inbounds, struct{ Protocol, Tag string }{in.Protocol, in.Tag})
			}
			break
		}
	}

	if selectedNodeName == "" {
		return c.Edit("❌ Node not found.")
	}

	msg := fmt.Sprintf("🔗 *Generate Link* (%s)\n\nSelect an Inbound:", selectedNodeName)
	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, in := range inbounds {
		label := fmt.Sprintf("[%s] %s", in.Protocol, in.Tag)
		rows = append(rows, kb.Row(kb.Data(label, "manual_inbound_link", fmt.Sprintf("%d:%s", nodeID, in.Tag))))
	}
	kb.Inline(rows...)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleManualGetLinkAskEmail(c telebot.Context, inboundTag string) error {
	return c.Edit(fmt.Sprintf("📝 *Enter Email*\n\nEnter the exact email used in Xray for inbound `%s`:", inboundTag), telebot.ModeMarkdown, keyboards.CancelInline())
}

func (h *Handler) HandleManualGetLinkAskUUID(c telebot.Context, email string) error {
	return c.Send(fmt.Sprintf("Step 2/2: Enter the *UUID* (Password) for user `%s`:", email), telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleManualGetLinkExecute(c telebot.Context, nodeIDStr, inboundTag, email, uuid string) error {
	nodeID, _ := strconv.Atoi(nodeIDStr)

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	link, err := h.adminUC.GenerateCustomConfigLink(ctx, uint(nodeID), inboundTag, email, uuid)
	if err != nil {
		return c.Send("❌ Error: " + err.Error())
	}

	msg := fmt.Sprintf("🔗 *Link Generated:*\n\n`%s`", link)
	return c.Send(msg, telebot.ModeMarkdown, keyboards.AdminMenu())
}
