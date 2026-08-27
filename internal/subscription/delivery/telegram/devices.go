package telegram

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	wgDomain "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	wireguardUC "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"gopkg.in/telebot.v3"
)

// HandleDevices shows the WireGuard device list with add/rotate/remove actions.
func (h *Handler) HandleDevices(c telebot.Context, subID uint) error {
	utils.AnswerCallback(c)
	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	return h.sendDeviceList(c, v.Sub)
}

func (h *Handler) sendDeviceList(c telebot.Context, sub *domain.Subscription) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	devices, err := h.deviceUC.ListDevices(ctx, sub.ID)
	if err != nil {
		return c.Send("Could not load devices.")
	}
	servers, _ := h.deviceUC.ListServers(ctx, sub.ID)
	serverName := make(map[wgEndpointKey]string, len(servers))
	for _, s := range servers {
		serverName[wgEndpointKey{s.InboundID, s.HostID}] = wgServerLabel(s)
	}
	multi := len(servers) > 1

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	var b strings.Builder
	b.WriteString("⚡ *WireGuard Devices*\n\n")
	if len(devices) == 0 {
		b.WriteString("No devices yet. Add one to get a config.")
	} else {
		for _, d := range devices {
			fmt.Fprintf(&b, "• %s — `%s` (%s)\n", utils.EscapeMarkdown(d.Label), d.AssignedIP, d.Status)
			if multi {
				if name := serverName[wgEndpointKey{d.InboundID, wgPeerHostID(d)}]; name != "" {
					fmt.Fprintf(&b, "   🌍 %s\n", utils.EscapeMarkdown(name))
				}
			}
			fmt.Fprintf(&b, "   ↑↓ %s\n", domain.FormatBytes(d.UpBytes+d.DownBytes))
			rows = append(rows, kb.Row(
				kb.Data("🔄 "+d.Label, "wg_dev_rotate", fmt.Sprintf("%d", sub.ID), fmt.Sprintf("%d", d.ID)),
				kb.Data("🗑", "wg_dev_remove", fmt.Sprintf("%d", sub.ID), fmt.Sprintf("%d", d.ID)),
			))
		}
	}
	// Add device: with >1 server, pick the server first; otherwise create directly.
	if multi {
		rows = append(rows, kb.Row(kb.Data("➕ Add device", "wg_dev_addpick", fmt.Sprintf("%d", sub.ID))))
	} else {
		var inboundID, hostID uint
		if len(servers) == 1 {
			inboundID, hostID = servers[0].InboundID, servers[0].HostID
		}
		rows = append(rows, kb.Row(kb.Data("➕ Add device", "wg_dev_add",
			fmt.Sprintf("%d", sub.ID), fmt.Sprintf("%d", inboundID), fmt.Sprintf("%d", hostID))))
	}
	rows = append(rows, kb.Row(kb.Data("« Back", "sub_select", fmt.Sprintf("%d", sub.ID))))
	kb.Inline(rows...)
	// Edit the current message when invoked from a button so we don't spam new
	// messages (Manage devices / back / remove all reuse the same message).
	return utils.EditOrSend(c, b.String(), telebot.ModeMarkdown, kb)
}

// wgEndpointKey identifies one pickable endpoint — an inbound seen through one
// of its hosts (host 0 = the inbound's own address).
type wgEndpointKey struct {
	InboundID uint
	HostID    uint
}

func wgPeerHostID(p *wgDomain.WGPeer) uint {
	if p.HostID == nil {
		return 0
	}
	return *p.HostID
}

func wgServerLabel(s wireguardUC.WGServerOption) string {
	name := s.NodeName
	if s.Country != "" {
		name = fmt.Sprintf("%s (%s)", s.NodeName, s.Country)
	}
	// Hosts are separate endpoints on the same node — show what tells them apart.
	if s.Label != "" {
		return fmt.Sprintf("%s — %s", name, s.Label)
	}
	if s.HostID != 0 && s.Endpoint != "" {
		return fmt.Sprintf("%s — %s", name, s.Endpoint)
	}
	return name
}

// HandleAddDevicePicker lists the sub's WG servers so the user picks where to
// create the device. Shown only when the sub has more than one WG server.
func (h *Handler) HandleAddDevicePicker(c telebot.Context, subID uint) error {
	utils.AnswerCallback(c)
	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	servers, err := h.deviceUC.ListServers(ctx, v.Sub.ID)
	if err != nil || len(servers) == 0 {
		return c.Send("No WireGuard servers available.")
	}
	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, s := range servers {
		rows = append(rows, kb.Row(kb.Data("➕ "+wgServerLabel(s), "wg_dev_add",
			fmt.Sprintf("%d", v.Sub.ID), fmt.Sprintf("%d", s.InboundID), fmt.Sprintf("%d", s.HostID))))
	}
	rows = append(rows, kb.Row(kb.Data("« Back", "sub_devices", fmt.Sprintf("%d", v.Sub.ID))))
	kb.Inline(rows...)
	return utils.EditOrSend(c, "Choose a server for the new device:", kb)
}

// HandleAddDevice provisions a new peer on the chosen WG inbound and delivers
// its .conf once. inboundID 0 means "first available server".
func (h *Handler) HandleAddDevice(c telebot.Context, subID, inboundID, hostID uint) error {
	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	utils.AnswerCallback(c, "Creating device…")
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	dc, err := h.deviceUC.CreateDevice(ctx, v.Sub.ID, wireguardUC.CreateDeviceInput{
		InboundID: inboundID,
		HostID:    hostID,
	})
	if err != nil {
		if errors.Is(err, wireguardUC.ErrDeviceCapReached) {
			return c.Send("⚠️ Device limit reached for this subscription.")
		}
		return c.Send("Could not create device. Try again.")
	}
	if err := h.sendDeviceConfig(c, v.Sub, dc); err != nil {
		return err
	}
	// Refresh the device list in place so the new device shows up.
	return h.sendDeviceList(c, v.Sub)
}

// HandleRotateDevice regenerates a device's keys and delivers the new .conf.
func (h *Handler) HandleRotateDevice(c telebot.Context, subID, deviceID uint) error {
	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	utils.AnswerCallback(c, "Regenerating…")
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	dc, err := h.deviceUC.RotateDevice(ctx, v.Sub.ID, deviceID)
	if err != nil {
		return c.Send("Could not regenerate device.")
	}
	return h.sendDeviceConfig(c, v.Sub, dc)
}

func (h *Handler) HandleRemoveDevice(c telebot.Context, subID, deviceID uint) error {
	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	utils.AnswerCallback(c, "Removing…")
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if err := h.deviceUC.RemoveDevice(ctx, v.Sub.ID, deviceID); err != nil {
		return c.Send("Could not remove device.")
	}
	return h.sendDeviceList(c, v.Sub)
}

func (h *Handler) sendDeviceConfig(c telebot.Context, sub *domain.Subscription, dc *wireguardUC.DeviceConfig) error {
	doc := &telebot.Document{
		File:     telebot.FromReader(strings.NewReader(dc.Conf)),
		FileName: fmt.Sprintf("wg_sub%d_dev%d.conf", sub.ID, dc.Peer.ID),
		Caption:  "⚡ WireGuard config — import into the WireGuard app.\nShown once; regenerate to get a new one.",
	}
	if err := c.Send(doc); err != nil {
		return err
	}
	return h.sendQRPhoto(c, dc.Conf, "Scan in the WireGuard app", nil)
}
