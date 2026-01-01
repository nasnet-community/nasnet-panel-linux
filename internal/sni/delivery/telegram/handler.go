package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	sniDomain "github.com/nasnet-community/nasnet-panel-linux/internal/sni/domain"
	sniUC "github.com/nasnet-community/nasnet-panel-linux/internal/sni/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/conversation"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"gopkg.in/telebot.v3"
)

// Handler handles SNI-related Telegram commands
type Handler struct {
	sniUsecase   sniUC.SNIUsecase
	nodeUsecase  usecase.NodeUsecase
	stateManager *conversation.StateManager
}

// NewHandler creates a new SNI handler
func NewHandler(sniUC sniUC.SNIUsecase, nodeUC usecase.NodeUsecase, stateManager *conversation.StateManager) *Handler {
	return &Handler{
		sniUsecase:   sniUC,
		nodeUsecase:  nodeUC,
		stateManager: stateManager,
	}
}

// HandleSNIList shows all saved SNIs
func (h *Handler) HandleSNIList(c telebot.Context) error {
	utils.AnswerCallback(c)

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	snis, err := h.sniUsecase.List(ctx)
	if err != nil {
		return c.Send("❌ Failed to list SNIs: " + err.Error())
	}

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	// Add "Add New" button and "Sync" button
	rows = append(rows, kb.Row(
		kb.Data("➕ Add New Certificate", "sni_add"),
		kb.Data("🔄 Sync from Nodes", "sni_sync"),
	))

	msg := "🔐 *TLS Certificates Management*\n\nManage your SNI certificates for TLS inbounds.\n\n"

	if len(snis) == 0 {
		msg += "_No certificates found. Add one or sync from nodes._"
	}

	for _, sni := range snis {
		var expiryStr string
		if sni.UsePathMode {
			expiryStr = "Path Mode (Skipped)"
		} else {
			expiry, err := h.sniUsecase.ValidateCertificate(sni.Certificate)
			if err == nil {
				expiryStr = expiry.Format("2006-01-02")
			} else if err.Error() == "certificate has expired" {
				expiryStr = "⚠️ EXPIRED"
			} else {
				expiryStr = "Invalid"
			}
		}

		rows = append(rows, kb.Row(kb.Data(fmt.Sprintf("� %s (%s)", sni.Name, expiryStr), "sni_view", fmt.Sprintf("%d", sni.ID))))
	}

	rows = append(rows, kb.Row(kb.Data("🔙 Back to Admin", "admin_menu")))

	kb.Inline(rows...)

	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, kb)
}

// HandleSNISync triggers certificate synchronization from nodes
func (h *Handler) HandleSNISync(c telebot.Context) error {
	utils.AnswerCallback(c)
	c.Edit("⏳ Syncing certificates from all active nodes... Please wait.", telebot.ModeMarkdown)

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	count, err := h.nodeUsecase.SyncCertificatesFromNodes(ctx)
	if err != nil {
		return c.Edit("❌ Sync failed: " + err.Error())
	}

	c.Send(fmt.Sprintf("✅ **Sync Completed**\n\nImported %d new certificate(s) from nodes.", count), telebot.ModeMarkdown)

	// Refresh list
	return h.HandleSNIList(c)
}

// HandleSNIView shows details of a specific SNI
func (h *Handler) HandleSNIView(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id, err := strconv.ParseUint(c.Data(), 10, 32)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Invalid ID"})
	}

	sni, err := h.sniUsecase.GetByID(ctx, uint(id))
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "SNI not found"})
	}

	expiry, err := h.sniUsecase.ValidateCertificate(sni.Certificate)
	expiryStr := "Unknown"
	status := "✅ Valid"
	if err == nil {
		expiryStr = expiry.Format("2006-01-02 15:04")
	} else if err.Error() == "certificate has expired" {
		expiryStr = expiry.Format("2006-01-02 15:04")
		status = "⚠️ EXPIRED"
	}

	// Determine certificate display
	var certDisplay string
	if sni.UsePathMode {
		certDisplay = fmt.Sprintf("Path: %s", sni.CertPath)
	} else {
		if len(sni.Certificate) > 70 {
			certDisplay = sni.Certificate[:70] + "..."
		} else {
			certDisplay = sni.Certificate
		}
	}

	msg := fmt.Sprintf("🔐 *SNI Details*\n\n"+
		"📛 *Name:* %s\n"+
		"🌐 *Domain:* `%s`\n"+
		"📋 *ALPN:* %s\n"+
		"📅 *Expires:* %s\n"+
		"🔒 *Status:* %s\n\n"+
		"📜 *Certificate:*\n```\n%s\n```\n\n"+
		"🔑 *Private Key:* `[HIDDEN]`",
		sni.Name, sni.Domain, sni.ALPN, expiryStr, status,
		certDisplay)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("📝 Edit Name", "sni_edit_name", fmt.Sprintf("%d", sni.ID))),
		kb.Row(kb.Data("🌐 Edit Domain", "sni_edit_domain", fmt.Sprintf("%d", sni.ID))),
		kb.Row(kb.Data("📜 Replace Certificate", "sni_edit_cert", fmt.Sprintf("%d", sni.ID))),
		kb.Row(kb.Data("🗑 Delete", "sni_delete_ask", fmt.Sprintf("%d", sni.ID))),
		keyboards.BackRow(kb, "sni_list"),
	)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleSNIAdd shows options for adding a certificate
func (h *Handler) HandleSNIAdd(c telebot.Context) error {
	utils.AnswerCallback(c)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("📋 Paste Certificate", "sni_add_manual")),
		kb.Row(kb.Data("🔒 Issue via Let's Encrypt", "sni_issue_start")),
		keyboards.BackRow(kb, "sni_list"),
	)

	return c.Edit("🔐 *Add TLS Certificate*\n\nChoose method:\n\n• *Paste*: Upload existing cert/key\n• *Let's Encrypt*: Auto-issue free certificate", telebot.ModeMarkdown, kb)
}

// HandleSNIAddManual starts the manual certificate add flow
func (h *Handler) HandleSNIAddManual(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID := c.Sender().ID

	h.stateManager.SetState(userID, conversation.StateAddSNIName)
	return c.Edit("🔐 *Add New TLS Certificate*\n\nStep 1/5: Enter a *display name* for this certificate:\n\n(e.g., 'Main Domain', 'CDN Cloudflare')", telebot.ModeMarkdown)
}

// HandleAddSNIName handles name input
func (h *Handler) HandleAddSNIName(c telebot.Context) error {
	userID := c.Sender().ID
	name := c.Text()

	h.stateManager.SetData(userID, "sni_name", name)
	h.stateManager.SetState(userID, conversation.StateAddSNIDomain)

	return c.Send("Step 2/5: Enter the *domain/SNI*:\n\n(e.g., `example.com`)", telebot.ModeMarkdown, keyboards.Cancel())
}

// HandleAddSNIDomain handles domain input
func (h *Handler) HandleAddSNIDomain(c telebot.Context) error {
	userID := c.Sender().ID
	domain := c.Text()

	h.stateManager.SetData(userID, "sni_domain", domain)

	// Show choice: Content or File Path
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("📋 Paste Certificate Content", "sni_mode_content")),
		kb.Row(kb.Data("📂 Use File Paths (on server)", "sni_mode_path")),
		kb.Row(kb.Data("❌ Cancel", "sni_list")),
	)

	return c.Send("Step 3/5: *Choose certificate input method:*\n\n• **Content**: Paste the certificate and key directly\n• **Path**: Specify file paths on the server", telebot.ModeMarkdown, kb)
}

// HandleAddSNICert handles certificate input
func (h *Handler) HandleAddSNICert(c telebot.Context) error {
	userID := c.Sender().ID
	cert := c.Text()

	// Quick validate
	_, err := h.sniUsecase.ValidateCertificate(cert)
	if err != nil {
		return c.Send("❌ Invalid certificate: "+err.Error()+"\n\nPlease paste a valid PEM certificate:", keyboards.Cancel())
	}

	h.stateManager.SetData(userID, "sni_cert", cert)
	h.stateManager.SetState(userID, conversation.StateAddSNIKey)

	return c.Send("✅ Certificate valid!\n\nStep 4/5: Paste the *Private Key* (PEM format):\n\n_Start with `-----BEGIN PRIVATE KEY-----` or `-----BEGIN RSA PRIVATE KEY-----`_", telebot.ModeMarkdown, keyboards.Cancel())
}

// HandleAddSNIKey handles private key input
func (h *Handler) HandleAddSNIKey(c telebot.Context) error {
	userID := c.Sender().ID
	key := c.Text()

	h.stateManager.SetData(userID, "sni_key", key)
	h.stateManager.SetData(userID, "sni_use_path", "false")
	h.stateManager.SetState(userID, conversation.StateAddSNIALPN)

	return c.Send("Step 5/5: Enter *ALPN* (comma-separated) or `-` for default:\n\nDefault: `h2,http/1.1`", telebot.ModeMarkdown, keyboards.Cancel())
}

// HandleSNIModeContent starts content input mode
func (h *Handler) HandleSNIModeContent(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID := c.Sender().ID

	h.stateManager.SetData(userID, "sni_use_path", "false")
	h.stateManager.SetState(userID, conversation.StateAddSNICert)

	return c.Edit("📋 Paste the *Certificate content* (PEM format):\n\n_Start with `-----BEGIN CERTIFICATE-----`_", telebot.ModeMarkdown)
}

// HandleSNIModePath starts file path input mode
func (h *Handler) HandleSNIModePath(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID := c.Sender().ID

	h.stateManager.SetData(userID, "sni_use_path", "true")
	h.stateManager.SetState(userID, conversation.StateAddSNICertPath)

	return c.Edit("📂 Enter *Certificate file path* on the server:\n\n_Example: `/root/cert/fullchain.pem`_", telebot.ModeMarkdown)
}

// HandleAddSNICertPath handles certificate path input
func (h *Handler) HandleAddSNICertPath(c telebot.Context) error {
	userID := c.Sender().ID
	certPath := c.Text()

	h.stateManager.SetData(userID, "sni_cert_path", certPath)
	h.stateManager.SetState(userID, conversation.StateAddSNIKeyPath)

	return c.Send("📂 Enter *Private Key file path* on the server:\n\n_Example: `/root/cert/privkey.pem`_", telebot.ModeMarkdown, keyboards.Cancel())
}

// HandleAddSNIKeyPath handles key path input
func (h *Handler) HandleAddSNIKeyPath(c telebot.Context) error {
	userID := c.Sender().ID
	keyPath := c.Text()

	h.stateManager.SetData(userID, "sni_key_path", keyPath)
	h.stateManager.SetState(userID, conversation.StateAddSNIALPN)

	return c.Send("Step 5/5: Enter *ALPN* (comma-separated) or `-` for default:\n\nDefault: `h2,http/1.1`", telebot.ModeMarkdown, keyboards.Cancel())
}

// HandleAddSNIALPN handles ALPN input and saves the SNI
func (h *Handler) HandleAddSNIALPN(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	alpn := c.Text()

	name := h.stateManager.GetStringData(userID, "sni_name")
	domain := h.stateManager.GetStringData(userID, "sni_domain")
	usePathMode := h.stateManager.GetStringData(userID, "sni_use_path") == "true"

	var sni *sniDomain.SNI
	var err error

	if usePathMode {
		certPath := h.stateManager.GetStringData(userID, "sni_cert_path")
		keyPath := h.stateManager.GetStringData(userID, "sni_key_path")
		h.stateManager.ResetSession(userID)
		sni, err = h.sniUsecase.CreateWithPaths(ctx, name, domain, certPath, keyPath, alpn)
	} else {
		cert := h.stateManager.GetStringData(userID, "sni_cert")
		key := h.stateManager.GetStringData(userID, "sni_key")
		h.stateManager.ResetSession(userID)
		sni, err = h.sniUsecase.Create(ctx, name, domain, cert, key, alpn)
	}

	if err != nil {
		return c.Send("❌ Failed to save SNI: "+err.Error(), keyboards.AdminMenu())
	}

	modeStr := "Content"
	validStr := "N/A (using file paths)"
	if !sni.UsePathMode {
		expiry, _ := h.sniUsecase.ValidateCertificate(sni.Certificate)
		validStr = expiry.Format("2006-01-02")
		modeStr = "Content"
	} else {
		modeStr = "File Paths"
	}

	return c.Send(fmt.Sprintf("✅ *SNI Saved Successfully!*\n\n"+
		"📛 Name: %s\n"+
		"🌐 Domain: `%s`\n"+
		"📦 Mode: %s\n"+
		"📅 Valid until: %s",
		sni.Name, sni.Domain, modeStr, validStr), telebot.ModeMarkdown, keyboards.AdminMenu())
}

// HandleSNIDeleteAsk asks for delete confirmation
func (h *Handler) HandleSNIDeleteAsk(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id := utils.CallbackID(c)
	sni, err := h.sniUsecase.GetByID(ctx, id)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "SNI not found"})
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("✅ Yes, Delete", "sni_delete", fmt.Sprintf("%d", sni.ID))),
		kb.Row(kb.Data("❌ Cancel", "sni_view", fmt.Sprintf("%d", sni.ID))),
	)

	return c.Edit(fmt.Sprintf("⚠️ *Delete Certificate?*\n\nAre you sure you want to delete:\n\n📛 %s\n🌐 `%s`\n\n_This cannot be undone._", sni.Name, sni.Domain), telebot.ModeMarkdown, kb)
}

// HandleSNIDelete deletes an SNI
func (h *Handler) HandleSNIDelete(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id := utils.CallbackID(c)
	if err := h.sniUsecase.Delete(ctx, id); err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Failed to delete"})
	}

	c.Respond(&telebot.CallbackResponse{Text: "Deleted!"})
	return h.HandleSNIList(c)
}

// HandleSNIEditName starts edit name flow
func (h *Handler) HandleSNIEditName(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID := c.Sender().ID
	id := c.Data()

	h.stateManager.SetState(userID, conversation.StateEditSNIName)
	h.stateManager.SetData(userID, "sni_edit_id", id)

	return c.Edit("📝 Enter new *display name*:", telebot.ModeMarkdown)
}

// HandleEditSNIName handles name edit
func (h *Handler) HandleEditSNIName(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	name := c.Text()

	idStr := h.stateManager.GetStringData(userID, "sni_edit_id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	h.stateManager.ResetSession(userID)

	if err := h.sniUsecase.Update(ctx, uint(id), name, "", "", "", ""); err != nil {
		return c.Send("❌ Failed to update: "+err.Error(), keyboards.AdminMenu())
	}

	return c.Send("✅ Name updated!", keyboards.AdminMenu())
}

// HandleSNIEditDomain starts edit domain flow
func (h *Handler) HandleSNIEditDomain(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID := c.Sender().ID
	id := c.Data()

	h.stateManager.SetState(userID, conversation.StateEditSNIDomain)
	h.stateManager.SetData(userID, "sni_edit_id", id)

	return c.Edit("🌐 Enter new *domain/SNI*:", telebot.ModeMarkdown)
}

// HandleEditSNIDomain handles domain edit
func (h *Handler) HandleEditSNIDomain(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	domain := c.Text()

	idStr := h.stateManager.GetStringData(userID, "sni_edit_id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	h.stateManager.ResetSession(userID)

	if err := h.sniUsecase.Update(ctx, uint(id), "", domain, "", "", ""); err != nil {
		return c.Send("❌ Failed to update: "+err.Error(), keyboards.AdminMenu())
	}

	return c.Send("✅ Domain updated!", keyboards.AdminMenu())
}

// HandleSNIEditCert starts edit certificate flow
func (h *Handler) HandleSNIEditCert(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID := c.Sender().ID
	id := c.Data()

	h.stateManager.SetState(userID, conversation.StateEditSNICert)
	h.stateManager.SetData(userID, "sni_edit_id", id)

	return c.Edit("📜 Paste new *Certificate* (PEM format):", telebot.ModeMarkdown)
}

// HandleEditSNICert handles certificate edit
func (h *Handler) HandleEditSNICert(c telebot.Context) error {
	userID := c.Sender().ID
	cert := c.Text()

	// Validate first
	_, err := h.sniUsecase.ValidateCertificate(cert)
	if err != nil {
		return c.Send("❌ Invalid certificate: "+err.Error()+"\n\nPlease paste a valid PEM certificate:", keyboards.Cancel())
	}

	h.stateManager.SetData(userID, "sni_new_cert", cert)
	h.stateManager.SetState(userID, conversation.StateEditSNIKey)

	return c.Send("📜 Certificate valid!\n\nNow paste the matching *Private Key*:", telebot.ModeMarkdown, keyboards.Cancel())
}

// HandleEditSNIKey handles private key edit after certificate
func (h *Handler) HandleEditSNIKey(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	key := c.Text()

	cert := h.stateManager.GetStringData(userID, "sni_new_cert")
	idStr := h.stateManager.GetStringData(userID, "sni_edit_id")
	id, _ := strconv.ParseUint(idStr, 10, 32)

	h.stateManager.ResetSession(userID)

	if err := h.sniUsecase.Update(ctx, uint(id), "", "", cert, key, ""); err != nil {
		return c.Send("❌ Failed to update: "+err.Error(), keyboards.AdminMenu())
	}

	return c.Send("✅ Certificate and Key updated!", keyboards.AdminMenu())
}

// === ACME Certificate Issuance Handlers ===

// HandleIssueStart shows challenge type options
func (h *Handler) HandleIssueStart(c telebot.Context) error {
	utils.AnswerCallback(c)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("⚡ HTTP-01 (Automatic)", "sni_issue_http01")),
		kb.Row(kb.Data("🌐 DNS-01 (Manual TXT record)", "sni_issue_dns01")),
		keyboards.BackRow(kb, "sni_add"),
	)

	msg := "🔒 *Issue Let's Encrypt Certificate*\n\n" +
		"Choose challenge type:\n\n" +
		"• *HTTP-01*: Automatic, requires port 80 access\n" +
		"• *DNS-01*: Add TXT record manually\n\n" +
		"_⚠️ Using staging server for testing_"

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleIssueHTTP01Start starts HTTP-01 issuance wizard
func (h *Handler) HandleIssueHTTP01Start(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID := c.Sender().ID

	h.stateManager.SetState(userID, conversation.StateIssueCertDomain)
	h.stateManager.SetData(userID, "issue_type", "http01")

	return c.Edit("🔒 *HTTP-01 Certificate Issuance*\n\nStep 1/2: Enter the *domain* to issue certificate for:\n\n(e.g., `proxy.example.com`)\n\n_The domain must point to a server with port 80 accessible._", telebot.ModeMarkdown)
}

// HandleIssueDNS01Start starts DNS-01 issuance wizard
func (h *Handler) HandleIssueDNS01Start(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID := c.Sender().ID

	h.stateManager.SetState(userID, conversation.StateIssueCertDomain)
	h.stateManager.SetData(userID, "issue_type", "dns01")

	return c.Edit("🔒 *DNS-01 Certificate Issuance*\n\nStep 1/3: Enter the *domain* to issue certificate for:\n\n(e.g., `proxy.example.com`)", telebot.ModeMarkdown)
}

// HandleIssueCertDomain handles domain input for ACME issuance
func (h *Handler) HandleIssueCertDomain(c telebot.Context) error {
	userID := c.Sender().ID
	domain := strings.TrimSpace(strings.ToLower(c.Text()))

	if domain == "" || !strings.Contains(domain, ".") {
		return c.Send("❌ Invalid domain. Enter a valid domain (e.g., example.com):")
	}

	h.stateManager.SetData(userID, "issue_domain", domain)
	h.stateManager.SetState(userID, conversation.StateIssueCertName)

	issueType := h.stateManager.GetStringData(userID, "issue_type")
	step := "2/2"
	if issueType == "dns01" {
		step = "2/3"
	}

	return c.Send(fmt.Sprintf("Step %s: Enter a *display name* for this certificate:", step), telebot.ModeMarkdown, keyboards.Cancel())
}

// HandleIssueCertName handles name input and initiates issuance
func (h *Handler) HandleIssueCertName(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	name := strings.TrimSpace(c.Text())

	domain := h.stateManager.GetStringData(userID, "issue_domain")
	issueType := h.stateManager.GetStringData(userID, "issue_type")

	if issueType == "http01" {
		// HTTP-01: Direct issuance
		h.stateManager.ResetSession(userID)
		c.Send("⏳ Issuing certificate for `"+domain+"`... Please wait, this may take up to 60 seconds.", telebot.ModeMarkdown)

		sni, err := h.sniUsecase.IssueCertHTTP01(ctx, name, domain)
		if err != nil {
			return c.Send("❌ Certificate issuance failed: "+err.Error(), keyboards.AdminMenu())
		}

		return c.Send(fmt.Sprintf("✅ *Certificate Issued Successfully!*\n\n"+
			"📛 Name: %s\n"+
			"🌐 Domain: `%s`\n"+
			"📅 Expires: %s\n"+
			"🔒 Issued via: Let's Encrypt (Staging)",
			sni.Name, sni.Domain, sni.ExpiresAt.Format("2006-01-02")), telebot.ModeMarkdown, keyboards.AdminMenu())
	}

	// DNS-01: Show TXT record to add
	h.stateManager.SetData(userID, "issue_name", name)

	challenge, err := h.sniUsecase.StartDNS01Challenge(ctx, domain)
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Failed to start DNS-01 challenge: "+err.Error(), keyboards.AdminMenu())
	}

	h.stateManager.SetState(userID, conversation.StateIssueDNS01Verify)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("✅ I've added the record - Verify", "sni_dns01_verify")),
		kb.Row(kb.Data("❌ Cancel", "sni_list")),
	)

	msg := fmt.Sprintf("🌐 *DNS-01 Challenge*\n\n"+
		"Add this TXT record to your DNS:\n\n"+
		"*Record:* `%s`\n"+
		"*Value:* `%s`\n\n"+
		"_Wait for DNS propagation (1-5 min), then click Verify._",
		challenge.TXTRecord, challenge.TXTValue)

	return c.Send(msg, telebot.ModeMarkdown, kb)
}

// HandleDNS01Verify completes DNS-01 verification
func (h *Handler) HandleDNS01Verify(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	domain := h.stateManager.GetStringData(userID, "issue_domain")
	name := h.stateManager.GetStringData(userID, "issue_name")

	h.stateManager.ResetSession(userID)

	c.Edit("⏳ Verifying DNS record and issuing certificate...")

	sni, err := h.sniUsecase.CompleteDNS01Challenge(ctx, name, domain)
	if err != nil {
		return c.Send("❌ Verification failed: "+err.Error()+"\n\n_Make sure the TXT record is correct and DNS has propagated._", telebot.ModeMarkdown, keyboards.AdminMenu())
	}

	return c.Send(fmt.Sprintf("✅ *Certificate Issued Successfully!*\n\n"+
		"📛 Name: %s\n"+
		"🌐 Domain: `%s`\n"+
		"📅 Expires: %s\n"+
		"🔒 Issued via: Let's Encrypt (Staging)",
		sni.Name, sni.Domain, sni.ExpiresAt.Format("2006-01-02")), telebot.ModeMarkdown, keyboards.AdminMenu())
}
