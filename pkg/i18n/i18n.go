package i18n

import (
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
)

// Define supported languages
const (
	LangEN = "en"
	LangFA = "fa"
)

// Dictionary maps a language key to its translations
type Dictionary map[string]string

// translations stores all supported languages
var translations = map[string]Dictionary{
	LangEN: {
		// Welcome message
		"WelcomeMsg": "👋 *Welcome, %s!*\n\n" +
			"🔐 *Premium V2Ray — Done Right*\n\n" +
			"We do ONE thing and do it best: blazing-fast, rock-solid *V2Ray / Xray* configs.\n\n" +
			"• ⚡ Lightning speed, ultra-low ping\n" +
			"• 💎 Top-tier quality & uptime\n" +
			"• 🌐 Great for streaming & gaming\n\n" +
			"🚀 Tap a button below to get started!",

		"TgLinkSuccess": "✅ *Connected!* You'll get subscription notifications right here.",
		"TgLinkExpired": "⚠️ This link expired. Open your subscription page and tap *Connect Telegram* again.",

		// Main Menu Buttons
		"BtnBuyVPN":    "⚡ Buy VPN",
		"BtnMySubs":    "🛍️ My Subs",
		"BtnProfile":   "👤 Profile",
		"BtnPayments":  "📜 Payments",
		"BtnSupport":   "💬 Support",
		"BtnHelp":      "❓ Help",
		"BtnGuide":     "📖 Guide",
		"BtnSettings":  "⚙️ Settings",
		"BtnFreeTrial": "🎁 Get Free Trial",

		// Settings Menu
		"SettingsTitle": "⚙️ *User Settings*\n━━━━━━━━━━━━━━━━━━━━",
		"SettingsDesc":  "Manage your preferences below:\n\n🌍 *Language:* %s",

		// Profile
		"ProfileMsg": "👤 *Your Profile*\n\n" +
			"🆔 *ID:* `%d`\n" +
			"👤 *Username:* @%s\n" +
			"📛 *Name:* %s %s\n" +
			"📊 *Status:* %s\n" +
			"🌐 *Language:* %s\n\n" +
			"📅 *Member since:* %s",
		"StatusActive": "✅ Active",
		"StatusBanned": "🚫 Banned",
		"StatusAdmin":  "👑 Admin",
		"TrialNotUsed": "Not Used",
		"TrialClaimed": "Claimed",

		// Balance
		"BalanceMsg": "💰 *Your Balance*\n\n" +
			"*Current:* $%.2f\n\n" +
			"To add funds, please contact support or use a coupon code.\n" +
			"Use ⚡ *Buy VPN* to purchase a subscription.",

		// Help
		"HelpMsg": "❓ *Help & Commands*\n\n" +
			"*Navigation:*\n" +
			"Use the buttons below to navigate.\n\n" +
			"*Main Features:*\n" +
			"⚡ *Buy VPN* - Purchase a subscription\n" +
			"📊 *My Subs* - View your subscriptions\n" +
			"👤 *Profile* - View your profile\n\n" +
			"*Commands:*\n" +
			"/start - Restart the bot\n" +
			"/help - Show this help\n" +
			"/redeem <code> - Redeem a coupon\n\n" +
			"*Support:*\n" +
			"Contact @admin for assistance.",

		// Support
		"SupportMsg": "💬 *Support Center*\n" +
			"━━━━━━━━━━━━━━━━\n\n" +
			"📞 *Contact Admin:* %s\n" +
			"⏰ *Response Time:* Usually within 24h\n\n" +
			"━━━━━━━━━━━━━━━━\n\n" +
			"❓ *Common Issues:*\n\n" +
			"• Subscription not working?\n" +
			"  → Try /start then check 📊 My Subs\n\n" +
			"• Payment stuck on pending?\n" +
			"  → Wait up to 24h for admin approval\n\n" +
			"• Need a refund?\n" +
			"  → Contact us with your payment ID\n\n" +
			"━━━━━━━━━━━━━━━━\n" +
			"💡 Use /help for commands",

		// General Errors and Success
		"ErrGeneral":      "❌ An error occurred. Please try again later.",
		"ErrUserNotFound": "❌ User not found.",
		"ErrRedeemFailed": "❌ Redemption failed: %s",
		"SuccessRedeem":   "🎉 *Success!*\n\n$%.2f has been added to your balance.\nNew Balance: $%.2f",
		"UsageRedeem":     "Usage: `/redeem <code>`\n\nExample: `/redeem FREE10`",

		// Language select
		"ChooseLanguage": "Welcome! Please select your language 👇",
		"ChangeLanguage": "Select your preferred language:",
		"LangChanged":    "✅ Language has been successfully changed.",

		// Inline buttons
		"BtnChangeLang": "🌐 Change Language",
		"BtnCancel":     "❌ Cancel",
		"BtnBackMenu":   "🔙 Back to Menu",
		"BtnConfirm":    "✅ Confirm",

		// === Plan Handler ===
		"ErrFetchPlans":    "❌ *Error fetching plans.* Please try again later.",
		"NoPlansAvailable": "📭 *No plans available right now.*",
		"PlansTitle": "💎 *Premium VPN Plans*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🚀 *High Speed • 🛡 Secure • ♾️ Unlimited Devices*\n\n",
		"PlanServers":    "\n🌍 *Servers:* %d Locations",
		"PlanDevices":    " • 📱 %d Devices",
		"PlanInfo":       "📝 *Info:* %s",
		"PlanDevicesCnt": "📱 *Devices:* %d",
		"BtnBuyPlan":     "💳 Buy %s ($%.2f)",
		"BtnCloseList":   "❌ Close List",
		"DataUnlimited":  "♾️ Unlimited",

		// === Payment Handler ===
		"ErrFetchPayments": "❌ Error fetching payments.",
		"PaymentHistoryEmpty": "📭 *Payment History*\n\nNo transactions yet.\n" +
			"Use ⚡ *Buy VPN* to make a purchase.",
		"PaymentHistoryTitle":  "📜 *Payment History* (Page %d)\n━━━━━━━━━━━━━━━━\n\n",
		"BtnPrev":              "⬅️ Prev",
		"BtnNext":              "➡️ Next",
		"PlanNotAvailable":     "❌ *Plan no longer available.*",
		"PlanAlreadyPurchased": "❌ *You have already purchased this plan.*\n\nThis plan can only be purchased once.",
		"PurchaseSummary": "💳 *Purchase Summary*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *Plan:* %s\n" +
			"💰 *Price:* $%.2f\n" +
			"⏱ *Duration:* %d Days\n" +
			"📊 *Data:* %s\n" +
			"%s" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"👇 *Select Payment Method:*",
		"BtnCrypto":         "🪙 Crypto",
		"BtnCardPayment":    "💳 Card Payment",
		"BtnAccountBalance": "💰 Account Balance",
		"BtnBackToPlans":    "🔙 Back to Plans",
		"CryptoPayment": "🪙 *Crypto Payment*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *Plan:* %s\n" +
			"💰 *Total:* `$%.2f`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔸 *USDT (BSC - BEP20):*\n`%s`\n\n" +
			"🔸 *Bitcoin (BTC):*\n`%s`\n\n" +
			"🔸 *Monero (XMR):*\n`%s`\n\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"⚠️ *Important:*\n" +
			"• Send the **exact amount**.\n" +
			"• Users are responsible for transaction fees.\n\n" +
			"1️⃣ Copy address & send payment\n" +
			"2️⃣ Click button below to verify",
		"ChooseCryptoAsset": "🪙 *Which cryptocurrency would you like to pay with?*",
		"CryptoUnavailable": "⚠️ Crypto payment is unavailable right now. Please pick another method or contact support.",
		"CryptoPaymentSingle": "🪙 *Crypto Payment*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *Plan:* %s\n" +
			"💰 *Total:* `$%.2f`\n" +
			"🪙 *Pay with:* %s\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔑 *Address:*\n`%s`\n\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"⚠️ Send the **exact amount**; you cover network fees.\n\n" +
			"1️⃣ Copy address & send payment\n" +
			"2️⃣ Click button below to verify",
		"BtnIHavePaid": "✅ I have Paid",
		"BtnBack":      "🔙 Back",
		"CardPayment": "💳 *Card Payment*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *Selected Plan:* %s\n" +
			"💵 *Amount:* `%s Toman`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🏦 *Card Number:*\n`%s`\n" +
			"👤 *Name:* %s\n\n" +
			"⚠️ *Note:*\n" +
			"1. Please transfer the exact amount.\n" +
			"2. After transfer, click «✅ I Paid» for admin to approve.\n\n" +
			"__________________________\n",
		"BtnIPaid":   "✅ I Paid",
		"BtnBackPay": "🔙 Back",
		"InsufficientBalance": "❌ *Insufficient Balance*\n\n" +
			"💰 *Price:* $%.2f\n" +
			"💳 *Your Balance:* $%.2f\n" +
			"📉 *Shortfall:* $%.2f\n\n" +
			"Please top up your account or pay directly via crypto.",
		"BtnTopUpCrypto": "💳 Top Up via Crypto",
		"PurchaseFailed": "❌ *Purchase Failed*\n\n" +
			"An error occurred. Please try again or contact support.",
		"PurchaseSuccessful": "✅ *Purchase Successful!*\n\n" +
			"📦 *Plan:* %s\n" +
			"🆔 *Sub ID:* #%d\n" +
			"📅 *Expires:* %s\n\n" +
			"Your subscription is active. Click below to get your config.",
		"BtnGetConfig": "📄 Get Config",
		"BtnQRCode":    "📱 QR Code",
		"BtnMainMenu":  "🔙 Main Menu",
		"PaymentVerification": "📝 *Payment Verification*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"Please send the **Transaction Hash (TXID)** or upload a **Screenshot** of the payment now.\n\n" +
			"_Click Cancel to stop._",
		"CardPaymentReceipt": "📸 *Send Payment Receipt*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"Please send the receipt image or transaction tracking number.\n\n" +
			"_Click Cancel to stop._",
		"SessionExpired":      "❌ Session expired. Please start over.",
		"InvalidProof":        "⚠️ Invalid proof. Please send a valid TXID or screenshot.",
		"PaymentError":        "❌ Error processing payment record. Please contact support.",
		"ProofSubmittedShort": "✅ Proof submitted! Reference: #%d\nAdmins have been notified.",
		"ProofSubmitted": "✅ *Proof Submitted!*\n\n" +
			"🆔 *Reference:* #%d\n" +
			"📦 *Plan:* %s\n" +
			"💰 *Amount:* $%.2f\n" +
			"📝 *Proof:* `%s`\n\n" +
			"Admins have been notified. Your subscription will be activated automatically upon approval.",
		"InvalidPlanSelection": "❌ Invalid plan selection.",

		// === Subscription Handler ===
		"TrialProcessing":  "⏳ Processing trial request...",
		"TrialSettingUp":   "⏳ *Setting up your free trial...*\nPlease wait a moment.",
		"TrialAlreadyUsed": "❌ *Trial Not Available*\n\nYou have already used your free trial.",
		"TrialNoPlans":     "⚠️ *Trial Not Available*\n\nThere are no trial plans active right now.",
		"TrialNoServers":   "⚠️ *Trial Not Available*\n\nThe trial plan has no assigned servers. Please contact support.",
		"TrialFailed":      "❌ Activation failed: %s",
		"TrialNextStep":    "🎉 You're all set! Tap 🛍️ *My Subs* anytime to view your config.",
		"BuyNoServers":     "⚠️ This plan has no available servers right now. Try another plan or contact support.",
		"TrialActivated": "🚀 *Trial Activated!*\n\n" +
			"📦 *Plan:* %s\n" +
			"📊 *Limit:* %s\n" +
			"⏱ *Expires:* %s\n\n" +
			"Here is your connection key:",
		"ConfigFile":     "📄 *Configuration File*\nImport this file into your %s client.",
		"BtnBuyPremium":  "💳 Buy Premium",
		"SubKeyText":     "🔐 *Subscription Key*\n\nClick to copy:\n`%s`",
		"SubKeyQR":       "🔐 *Key / QR*\n\nTap to copy:\n`%s`",
		"ErrFetchSubs":   "❌ Error fetching subscriptions.",
		"NoSubsYet":      "📭 You have no subscriptions yet.\n\nUse 💳 *Buy* to purchase a plan!",
		"MySubsTitle":    "📂 *My Subscriptions* (Page %d)\n━━━━━━━━━━━━━━━━━━━━\n\n",
		"BtnRefreshList": "🔄 Refresh List",
		"TimeLeft":       "⏳ %s left",
		"SubNotFound":    "❌ Subscription not found",
		"ErrOccurred":    "❌ An error occurred.",
		"AccessDenied":   "❌ Access denied.",
		"SubDetails": "📊 *%s*\n" +
			"%s *%s*\n\n" +
			"📦 *Plan:* %s\n" +
			"%s\n\n" +
			"━━━━━━━ *DATA* ━━━━━━━\n" +
			"📈 *Used:* %s / %s\n" +
			"%s %.0f%%\n" +
			"📉 *Left:* %s\n\n" +
			"━━━━━━━ *VALIDITY* ━━━━━━━\n" +
			"📅 *Expires:* %s\n" +
			"⏳ *%s*\n\n" +
			"🌐 *Servers:* %d    🕒 *Since:* %s",
		"StatusOnline":       "🟢 Online",
		"LastActive":         "🟢 Last active: %s",
		"WgNotConnected":     "⚪ Not connected yet",
		"BtnRegenLink":       "🔗 New Link",
		"SubLinkRegenerated": "🔗 *New Subscription Link*\n\nOld link disabled. Installed apps keep working — re-share this link:\n\n`%s`",
		"UnlimitedData":      "♾️ Unlimited",
		"BtnSubLink":         "📲 Sub Link",
		"ServersTitle":       "🌐 *Servers* (%d)\n\nPick a server for its QR + link:",
		"BtnAllConfigs":      "📋 All configs",
		"BtnRename":          "✏️ Rename",
		"BtnRegenKey":        "🔄 Regenerate Key",
		"BtnBackToList":      "🔙 Back to List",
		"BtnRenewNow":        "⚠️ Renew Now",
		"BtnRenewSoon":       "🔔 Renew Soon",
		"BtnRenew":           "🔄 Renew",
		"BtnRenewSub":        "🔄 Renew Subscription",
		"BtnRenewMetered":    "🔄 New cycle (resets usage)",
		"BtnRenewMeteredNow": "⚠️ New cycle now (resets usage)",
		"GeneratingConfig":   "📄 Generating config...",
		"ErrFetchSub":        "❌ Error fetching subscription",
		"SubNotActive":       "❌ Subscription is not active.",
		"ErrGenerateConfig":  "❌ Failed to generate config. Please try again or contact support.",
		"ConfigTitle":        "📄 *Configuration*\n━━━━━━━━━━━━━━━━━━━━\n\n",
		"ConfigN":            "*Config %d:*\n",
		"ConfigTapCopy":      "💡 _Tap on a config to copy it._",
		"BtnShowQR":          "📱 Show QR",
		"GeneratingQR":       "📱 Generating QR code...",
		"NoConfigAvailable":  "❌ No config available",
		"QRTooLong":          "📱 *Subscription Key*\n\n⚠️ Link too long for QR code.\n\nClick to copy:\n`%s`",
		"QRCode":             "📱 *QR Code*\n\nScan with your VPN client or tap to copy:\n`%s`",
		"QRCaptionN":         "📱 *Config %d*\n\n`%s`",
		"QRAlbumNav":         "📱 *QR codes ready* — scan each above with your VPN client, or tap a code to copy.",
		"SubLinkTitle": "📲 *Subscription Link*\n\n" +
			"Add this URL to your V2Ray client for auto-updates:\n\n" +
			"`%s`\n\n" +
			"💡 *How to use:*\n" +
			"• V2RayNG: Subscription → + → Paste URL\n" +
			"• Streisand: Add Subscription → Paste URL\n" +
			"• Shadowrocket: Add Subscription → Paste URL",
		"SubLinkNotConfigured": "❌ Subscription links not configured. Contact admin.",
		"RenameTitle":          "✏️ *Rename Subscription*\n\nPlease enter a new name for Subscription #%d:",
		"NameTooLong":          "❌ Name too long (max 20 chars). Try again:",
		"NameEmpty":            "❌ Name cannot be empty. Try again:",
		"SessionError":         "❌ Session error. Please try again.",
		"FailedUpdateName":     "❌ Failed to update name.",
		"RenamedTo":            "✅ Renamed to *%s*",
		"RegenWarning": "⚠️ *Are you sure?*\n\n" +
			"Regenerating your key will **invalidate your current connection**.\n" +
			"You will need to update the configuration on all your devices.",
		"BtnYesRegenerate": "✅ Yes, Regenerate",
		"Regenerating":     "🔄 Regenerating...",
		"Processing":       "🔄 *Processing...* Please wait.",
		"RegenFailed":      "❌ Failed to regenerate: %s",
		"KeyRegenerated":   "✅ *Key Regenerated Successfully!*",
		"PlanNotFoundSub":  "❌ Plan not found for this subscription",
		"RenewTitle": "🔄 *Renew Subscription*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"📦 *Plan:* %s\n" +
			"💰 *Price:* $%.2f\n\n" +
			"📅 *Current Expiry:* %s\n" +
			"📅 *New Expiry:* %s\n\n" +
			"%s",
		"RenewCrypto":  "💳 Renew with Crypto",
		"RenewCard":    "💳 Renew with Card",
		"RenewBalance": "💰 Renew from Balance",
		"RenewCryptoInstructions": "🪙 *Renew with Crypto*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *Plan:* %s\n" +
			"💰 *Total:* `$%.2f`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔸 *USDT (BSC - BEP20):*\n`%s`\n\n" +
			"🔸 *Bitcoin (BTC):*\n`%s`\n\n" +
			"🔸 *Monero (XMR):*\n`%s`\n\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"Please send the exact amount.",
		"RenewCryptoSingle": "🪙 *Renew with Crypto*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *Plan:* %s\n" +
			"💰 *Total:* `$%.2f`\n" +
			"🪙 *Pay with:* %s\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔑 *Address:*\n`%s`\n\n" +
			"Send the exact amount, then send the transaction hash or a screenshot.",
		"RenewCardInstructions": "💳 *Renew with Card*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *Plan:* %s\n" +
			"💵 *Amount:* `%s Toman`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🏦 *Card Number:*\n`%s`\n" +
			"👤 *Name:* %s\n\n" +
			"Please transfer the exact amount.",
		"RenewProofReceived": "✅ *Renewal Proof Received!*\n\n" +
			"🆔 *Reference:* #%d\n" +
			"📦 *Plan:* %s\n\n" +
			"Admins have been notified.",
		"RenewInsufficientBalance": "❌ *Insufficient Balance*\n\n" +
			"💰 *Renewal Price:* $%.2f\n" +
			"💳 *Your Balance:* $%.2f\n" +
			"📉 *Shortfall:* $%.2f\n\n" +
			"Please top up your account or use crypto/card payment to renew.",
		"RenewError":         "❌ *Renewal Failed*\n\nAn error occurred. Please try again or contact support.",
		"RenewSuccess":       "✅ *Subscription Renewed!*\n\n📦 *Plan:* %s\n\nYour subscription has been extended.",
		"BtnAddData":         "➕ Add Data",
		"BtnAddDataPriced":   "➕ Add Data ($%.2f/GB)",
		"BtnContinue":        "Continue ➡️",
		"TopUpNotAllowed":    "Top-up is not available for this plan.",
		"TopUpNeedsActive":   "Top-up is only available on an active subscription. Please renew instead.",
		"RenewNotAllowed":    "Renewal is not available for this plan.",
		"PlanInactive":       "❌ This plan has been disabled. Renewal and adding data are no longer available for it.",
		"TopUpError":         "❌ Top-up failed. Please try again.",
		"TopUpKeepsExpiry":   "Expiry is unchanged.",
		"TopUpExtendsExpiry": "Expiry is extended.",
		"TopUpStepper": "➕ *Add Data — %s*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"*+%d GB* — $%.2f\n%s",
		"TopUpMethod": "➕ *Add Data — %s*\n\n*+%d GB* — $%.2f\n\nChoose a payment method:",
		"TopUpConfirm": "➕ *Confirm Top-up*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"*+%d GB* — $%.2f\n\n" +
			"💳 *Balance:* $%.2f\n💳 *After:* $%.2f",
		"TopUpSuccess":       "✅ *Added %d GB to your subscription.*",
		"TopUpProofReceived": "✅ *Top-up request #%d received* (+%d GB)\n\nIt will be applied after the admin confirms your payment.",
		"TopUpCryptoInstructions": "🪙 *Add Data — +%d GB*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *Total:* `$%.2f`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔸 *USDT (BEP20):*\n`%s`\n\n🔸 *BTC:*\n`%s`\n\n🔸 *XMR:*\n`%s`\n\n" +
			"Send the exact amount, then send the transaction hash or a screenshot.",
		"TopUpCryptoSingle": "🪙 *Add Data — +%d GB*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *Total:* `$%.2f`\n" +
			"🪙 *Pay with:* %s\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔑 *Address:*\n`%s`\n\n" +
			"Send the exact amount, then send the transaction hash or a screenshot.",
		"TopUpCardInstructions": "💳 *Add Data — +%d GB*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *Total:* %s Toman\n\n" +
			"🔸 *Card:* `%s`\n🔸 *Holder:* %s\n\n" +
			"Pay, then send a screenshot of the receipt.",
		"RenewMeteredStepper": "🔄 *Renew — %s*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"*%d GB* — $%.2f\n+%d days, fresh data pool",
		"RenewMeteredMethod": "🔄 *Renew — %s*\n\n*%d GB* — $%.2f\n+%d days\n\nChoose a payment method:",
		"RenewMeteredConfirm": "🔄 *Confirm Renewal*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"*%d GB* — $%.2f\n\n" +
			"💳 *Balance:* $%.2f\n💳 *After:* $%.2f",
		"RenewMeteredDiscardWarn": "⚠️ You still have %.1f GB unused. Renewing starts a fresh pool and discards it.\n\n",
		"RenewMeteredCryptoInstructions": "🪙 *Renew — %s (%d GB)*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *Total:* `$%.2f`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔸 *USDT (BEP20):*\n`%s`\n\n🔸 *BTC:*\n`%s`\n\n🔸 *XMR:*\n`%s`\n\n" +
			"Send the exact amount, then send the transaction hash or a screenshot.",
		"RenewMeteredCryptoSingle": "🪙 *Renew — %s (%d GB)*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *Total:* `$%.2f`\n" +
			"🪙 *Pay with:* %s\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔑 *Address:*\n`%s`\n\n" +
			"Send the exact amount, then send the transaction hash or a screenshot.",
		"RenewMeteredCardInstructions": "💳 *Renew — %s (%d GB)*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *Total:* %s Toman\n\n" +
			"🔸 *Card:* `%s`\n🔸 *Holder:* %s\n\n" +
			"Pay, then send a screenshot of the receipt.",
		"BalanceSufficient":        "✅ Your Balance: $%.2f",
		"BalanceInsufficient":      "❌ Your Balance: $%.2f (need $%.2f more)",
		"BtnConfirmRenewal":        "✅ Confirm Renewal",
		"BtnTopUpBalance":          "💳 Top Up Balance",
		"ProcessingRenewal":        "🔄 Processing renewal...",
		"RenewalProcessing":        "🔄 *Processing your renewal...* Please wait.",
		"InsufficientBalanceRenew": "❌ *Insufficient Balance*\n\nPlease top up your account to renew.",
		"RenewalFailed":            "❌ Renewal failed: %s",
		"SubRenewed": "✅ *Subscription Renewed!*\n\n" +
			"📦 *Plan:* %s\n" +
			"📅 *New Expiry:* %s\n\n" +
			"Your subscription is now active.",
		"BtnMySubscriptions": "📂 My Subscriptions",

		// === Approval Notifications ===
		"NotifyRenewalApproved": "🎉 *Renewal Approved!*\n\n" +
			"Your payment has been approved and your subscription has been renewed.\n\n" +
			"📋 *Sub ID:* #%d\n" +
			"📅 *New Expiry:* %s\n\n" +
			"Use /subscriptions to view your config.",
		"NotifyRenewalFailed": "✅ *Payment Approved!*\n\n⚠️ Could not renew subscription: %s\nPlease contact admin.",
		"NotifyPaymentApproved": "🎉 *Payment Approved!*\n\n" +
			"Your payment has been approved and your subscription is now active.\n\n" +
			"📋 *Sub ID:* #%d\n" +
			"📅 *Expires:* %s\n\n" +
			"Use /subscriptions to view your config.",
		"NotifyPlanActivated":         "✅ *Payment Approved!*\n\nPaid: `$%.2f`\nSubscription activated successfully!",
		"NotifyBalanceAddFailed":      "✅ *Payment Approved!*\n\nPaid: `$%.2f %s`\n\n⚠️ Could not auto-activate subscription: %s\nPlease activate it manually.",
		"NotifyTopUp":                 "✅ *Payment Approved!*\n\nYour balance has been topped up by `%.2f %s`.",
		"NotifyPaymentRejected":       "❌ *Payment Rejected*\n\nYour payment could not be approved. Please contact support if you believe this is a mistake.",
		"NotifyPaymentRejectedReason": "❌ *Payment Rejected*\n\nReason: %s",
		"NotifyUsdtVerifying":         "⏳ *Payment received*\n\nWe found your transaction and are waiting for network confirmations. Your payment will be approved automatically once confirmed.",
		"PaymentVerifyUsdtHash":       "💡 Paste your *BSC transaction hash* (`0x…`) and your payment will be verified and approved automatically.",

		// === Payment History Labels ===
		"PayDescPurchase":    "Plan purchase: %s",
		"PayDescRenewal":     "Subscription renewal: %s",
		"PayStatusCompleted": "Completed",
		"PayStatusPending":   "Pending",
		"PayStatusFailed":    "Failed",
		"PayStatusRefunded":  "Refunded",
		"PayMethodCrypto":    "CRYPTO",
		"PayMethodCard":      "CARD",
		"PayMethodBalance":   "BALANCE",

		// === Plan Detail Labels ===
		"PlanPrice":    "Price:",
		"PlanDuration": "Duration:",
		"PlanDays":     "%d Days",
		"PlanData":     "Data:",

		// === Time Formatting ===
		"TimeDaysHours":   "%d days %d hours",
		"TimeDays":        "%d days",
		"TimeHours":       "%d hours",
		"TimeMinutes":     "%d min",
		"TimeLessThanMin": "Less than 1 min",
		"TimeUnlimited":   "Unlimited",
		"TimeExpired":     "Expired",

		// === Status Text ===
		"StatusTextActive":           "ACTIVE",
		"StatusTextExpired":          "EXPIRED",
		"StatusTextPending":          "PENDING",
		"StatusTextCancelled":        "CANCELLED",
		"StatusTextPaused":           "PAUSED",
		"StatusTextTrafficExhausted": "DATA EXHAUSTED",

		// === Data Limit ===
		"DataLimitUnlimited": "♾️ Unlimited",

		// === Expiration Notifications ===
		"NotifExpired":        "🚫 *Subscription Expired*\n\nYour **%s** subscription has expired.\n\nRenew now to restore access.",
		"NotifExpiry1Day":     "⚠️ *Final Reminder*\n\nYour **%s** subscription expires **tomorrow**.\n\n📅 Expiry: %s",
		"NotifExpiry3Days":    "🔔 *Expiry Warning*\n\nYour **%s** subscription expires in **%d days**.\n\n📅 Expiry: %s",
		"NotifExpiry7Days":    "📅 *Subscription Reminder*\n\nYour **%s** subscription expires in **%d days**.\n\n📅 Expiry: %s",
		"BtnRenewNowNotif":    "🔄 Renew Now",
		"BtnViewDetailsNotif": "📊 View Details",
		"BtnViewUsageNotif":   "📊 View Usage",

		// === Data Usage Notifications ===
		"NotifDataExhausted": "🚫 *Data Limit Reached*\n\nYour **%s** subscription has reached 100%% of its data limit.\n\nAccess may be restricted until you add more data or renew.",
		"NotifData90":        "⚠️ *Data Usage Warning*\n\nYour **%s** subscription has used **90%%** of its data limit.",
		"NotifData75":        "🔔 *Data Usage Alert*\n\nYour **%s** subscription has used **75%%** of its data limit.",
		"NotifData50":        "ℹ️ *Data Usage Notice*\n\nYour **%s** subscription has used **50%%** of its data limit.",

		// === Partial Provisioning ===
		"PartialProvisioning": "⚠️ Some servers are temporarily unavailable. Your subscription is active and the remaining servers will be added automatically shortly.",

		// === Balance Confirmation ===
		"ConfirmBalanceBuy": "💰 *Confirm Purchase*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"📦 *Plan:* %s\n" +
			"💰 *Price:* $%.2f\n\n" +
			"💳 *Current Balance:* $%.2f\n" +
			"💳 *Remaining Balance:* $%.2f\n\n" +
			"Are you sure you want to proceed?",
		"ConfirmBalanceRenew": "💰 *Confirm Renewal*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"📦 *Plan:* %s\n" +
			"💰 *Price:* $%.2f\n\n" +
			"💳 *Current Balance:* $%.2f\n" +
			"💳 *Remaining Balance:* $%.2f\n\n" +
			"Are you sure you want to renew?",
		"BtnConfirmPurchase": "✅ Confirm Purchase",

		// === Bot / Transport ===
		"SessionExpiredStart": "⏰ Your session expired. Please start again.",
		"Cancelled":           "❌ Cancelled.",
		"MainMenuText":        "🏠 Main Menu",
		"AdminMenuText":       "🛠 Admin Menu",
		"AdminPanelText":      "🛠 Admin Panel",

		// Maintenance mode notice shown to users during service maintenance
		"MaintenanceNotice": "🛠 Service maintenance in progress.\nPurchases, renewals, and config regeneration are temporarily paused. Your existing configs still work.\nPlease try again shortly.",
	},
	LangFA: {
		// Welcome message
		"WelcomeMsg": "👋 *خوش آمدید، %s!*\n\n" +
			"🔐 *V2Ray پرمیوم — بی‌نقص*\n\n" +
			"ما فقط یک کار می‌کنیم و بهترینش هستیم: کانفیگ‌های *V2Ray / Xray* فوق‌سریع و پایدار.\n\n" +
			"• ⚡ سرعت بالا و پینگ بسیار پایین\n" +
			"• 💎 کیفیت درجه‌یک و آپ‌تایم عالی\n" +
			"• 🌐 عالی برای استریم و گیمینگ\n\n" +
			"🚀 برای شروع، یکی از دکمه‌های زیر را بزنید!",

		"TgLinkSuccess": "✅ *متصل شد!* از این پس اعلان‌های اشتراک همینجا ارسال می‌شود.",
		"TgLinkExpired": "⚠️ این لینک منقضی شده است. صفحه اشتراک خود را باز کنید و دوباره روی *اتصال تلگرام* بزنید.",

		// Main Menu Buttons
		"BtnBuyVPN":    "⚡ خرید اشتراک",
		"BtnMySubs":    "🛍️ سرویس‌های من",
		"BtnProfile":   "👤 حساب کاربری",
		"BtnPayments":  "📜 پرداخت‌ها",
		"BtnSupport":   "💬 پشتیبانی",
		"BtnHelp":      "❓ راهنما",
		"BtnGuide":     "📖 آموزش",
		"BtnSettings":  "⚙️ تنظیمات",
		"BtnFreeTrial": "🎁 دریافت تست رایگان",

		// Settings Menu
		"SettingsTitle": "⚙️ *تنظیمات کاربر*\n━━━━━━━━━━━━━━━━━━━━",
		"SettingsDesc":  "تغییر تنظیمات و ترجیحات:\n\n🌍 *زبان فعلی:* %s",

		// Profile
		"ProfileMsg": "👤 *حساب کاربری شما*\n\n" +
			"🆔 *شناسه:* `%d`\n" +
			"👤 *نام کاربری:* @%s\n" +
			"📛 *نام:* %s %s\n" +
			"📊 *وضعیت:* %s\n" +
			"🌐 *زبان:* %s\n\n" +
			"📅 *عضویت از:* %s",
		"StatusActive": "✅ فعال",
		"StatusBanned": "🚫 مسدود",
		"StatusAdmin":  "👑 مدیر",
		"TrialNotUsed": "استفاده نشده",
		"TrialClaimed": "دریافت شده",

		// Balance
		"BalanceMsg": "💰 *موجودی شما*\n\n" +
			"*فعلی:* $%.2f\n\n" +
			"برای افزایش موجودی لطفاً با پشتیبانی تماس بگیرید یا از کد تخفیف استفاده کنید.\n" +
			"از ⚡ *خرید اشتراک* برای تهیه سرویس استفاده کنید.",

		// Help
		"HelpMsg": "❓ *راهنما و دستورات*\n\n" +
			"*منوها:*\n" +
			"از دکمه‌های زیر برای جابجایی استفاده کنید.\n\n" +
			"*امکانات اصلی:*\n" +
			"⚡ *خرید اشتراک* - تهیه سرویس جدید\n" +
			"📊 *سرویس‌های من* - مشاهده سرویس‌های شما\n" +
			"👤 *حساب کاربری* - مشاهده اطلاعات حساب\n\n" +
			"*دستورات:*\n" +
			"/start - راه‌اندازی مجدد ربات\n" +
			"/help - نمایش این راهنما\n" +
			"/redeem <code> - استفاده از کد هدیه\n\n" +
			"*پشتیبانی:*\n" +
			"برای کمک با @admin تماس بگیرید.",

		// Support
		"SupportMsg": "💬 *مرکز پشتیبانی*\n" +
			"━━━━━━━━━━━━━━━━\n\n" +
			"📞 *ارتباط با پشتیبانی:* %s\n" +
			"⏰ *زمان پاسخگویی:* معمولا کمتر از ۲۴ ساعت\n\n" +
			"━━━━━━━━━━━━━━━━\n\n" +
			"❓ *مشکلات رایج:*\n\n" +
			"• سرویس شما کار نمی‌کند؟\n" +
			"  → ربات را /start کرده و در 📊 منو بررسی کنید\n\n" +
			"• پرداخت شما تایید نشده؟\n" +
			"  → تا ۲۴ ساعت منتظر تایید بمانید\n\n" +
			"━━━━━━━━━━━━━━━━\n" +
			"💡 برای راهنما /help را ارسال کنید",

		// General Errors and Success
		"ErrGeneral":      "❌ خطایی رخ داد. لطفا بعدا تلاش کنید.",
		"ErrUserNotFound": "❌ کاربر پیدا نشد.",
		"ErrRedeemFailed": "❌ استفاده از کد ناموفق بود: %s",
		"SuccessRedeem":   "🎉 *موفقیت!*\n\nمبلغ $%.2f به موجودی شما افزوده شد.\nموجودی جدید: $%.2f",
		"UsageRedeem":     "نحوه استفاده: `/redeem <code>`\n\nمثال: `/redeem FREE10`",

		// Language select
		"ChooseLanguage": "به ربات ما خوش آمدید! لطفا زبان خود را انتخاب کنید 👇",
		"ChangeLanguage": "زبان مورد نظر خود را انتخاب کنید:",
		"LangChanged":    "✅ زبان با موفقیت تغییر یافت.",

		// Inline buttons
		"BtnChangeLang": "🌐 تغییر زبان",
		"BtnCancel":     "❌ لغو",
		"BtnBackMenu":   "🔙 بازگشت به منو",
		"BtnConfirm":    "✅ تایید",

		// === Plan Handler ===
		"ErrFetchPlans":    "❌ *خطا در دریافت پلن‌ها.* لطفا بعدا تلاش کنید.",
		"NoPlansAvailable": "📭 *هیچ پلنی در حال حاضر موجود نیست.*",
		"PlansTitle": "💎 *پلن‌های اشتراک VPN*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🚀 *سرعت بالا • 🛡 امن • ♾️ دستگاه نامحدود*\n\n",
		"PlanServers":    "\n🌍 *سرورها:* %d لوکیشن",
		"PlanDevices":    " • 📱 %d دستگاه",
		"PlanInfo":       "📝 *توضیحات:* %s",
		"PlanDevicesCnt": "📱 *دستگاه:* %d",
		"BtnBuyPlan":     "💳 خرید %s ($%.2f)",
		"BtnCloseList":   "❌ بستن لیست",
		"DataUnlimited":  "♾️ نامحدود",

		// === Payment Handler ===
		"ErrFetchPayments": "❌ خطا در دریافت پرداخت‌ها.",
		"PaymentHistoryEmpty": "📭 *تاریخچه پرداخت‌ها*\n\nهنوز تراکنشی ندارید.\n" +
			"از ⚡ *خرید اشتراک* برای خرید استفاده کنید.",
		"PaymentHistoryTitle":  "📜 *تاریخچه پرداخت‌ها* (صفحه %d)\n━━━━━━━━━━━━━━━━\n\n",
		"BtnPrev":              "⬅️ قبلی",
		"BtnNext":              "➡️ بعدی",
		"PlanNotAvailable":     "❌ *پلن دیگر موجود نیست.*",
		"PlanAlreadyPurchased": "❌ *شما قبلا این پلن را خریداری کرده‌اید.*\n\nاین پلن فقط یک بار قابل خرید است.",
		"PurchaseSummary": "💳 *خلاصه خرید*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *پلن:* %s\n" +
			"💰 *قیمت:* $%.2f\n" +
			"⏱ *مدت:* %d روز\n" +
			"📊 *حجم:* %s\n" +
			"%s" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"👇 *روش پرداخت را انتخاب کنید:*",
		"BtnCrypto":         "🪙 رمزارز",
		"BtnCardPayment":    "💳 کارت به کارت",
		"BtnAccountBalance": "💰 موجودی حساب",
		"BtnBackToPlans":    "🔙 بازگشت به پلن‌ها",
		"CryptoPayment": "🪙 *پرداخت رمزارز*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *پلن:* %s\n" +
			"💰 *مبلغ:* `$%.2f`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔸 *USDT (BSC - BEP20):*\n`%s`\n\n" +
			"🔸 *Bitcoin (BTC):*\n`%s`\n\n" +
			"🔸 *Monero (XMR):*\n`%s`\n\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"⚠️ *توجه:*\n" +
			"• مبلغ **دقیق** را واریز کنید.\n" +
			"• کارمزد تراکنش بر عهده کاربر است.\n\n" +
			"1️⃣ آدرس را کپی کرده و پرداخت کنید\n" +
			"2️⃣ روی دکمه زیر کلیک کنید",
		"ChooseCryptoAsset": "🪙 *با کدام رمزارز پرداخت می‌کنید؟*",
		"CryptoUnavailable": "⚠️ پرداخت رمزارزی در حال حاضر در دسترس نیست. لطفاً روش دیگری انتخاب کنید یا با پشتیبانی تماس بگیرید.",
		"CryptoPaymentSingle": "🪙 *پرداخت رمزارز*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *پلن:* %s\n" +
			"💰 *مبلغ:* `$%.2f`\n" +
			"🪙 *پرداخت با:* %s\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔑 *آدرس:*\n`%s`\n\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"⚠️ مبلغ **دقیق** را واریز کنید؛ کارمزد شبکه بر عهده شماست.\n\n" +
			"1️⃣ آدرس را کپی کرده و پرداخت کنید\n" +
			"2️⃣ روی دکمه زیر کلیک کنید",
		"BtnIHavePaid": "✅ پرداخت کردم",
		"BtnBack":      "🔙 بازگشت",
		"CardPayment": "💳 *پرداخت کارت به کارت*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *پلن انتخابی:* %s\n" +
			"💵 *مبلغ قابل پرداخت:* `%s تومان`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🏦 *شماره کارت:*\n`%s`\n" +
			"👤 *به نام:* %s\n\n" +
			"⚠️ *توجه:*\n" +
			"۱. لطفا مبلغ دقیق را واریز کنید.\n" +
			"۲. پس از واریز، روی دکمه «✅ پرداخت کردم» کلیک کنید تا ادمین تایید کند.\n\n" +
			"__________________________\n",
		"BtnIPaid":   "✅ پرداخت کردم",
		"BtnBackPay": "🔙 بازگشت",
		"InsufficientBalance": "❌ *موجودی ناکافی*\n\n" +
			"💰 *قیمت:* $%.2f\n" +
			"💳 *موجودی شما:* $%.2f\n" +
			"📉 *کسری:* $%.2f\n\n" +
			"لطفا حساب خود را شارژ کنید یا از طریق رمزارز پرداخت کنید.",
		"BtnTopUpCrypto": "💳 شارژ با رمزارز",
		"PurchaseFailed": "❌ *خرید ناموفق*\n\n" +
			"خطایی رخ داد. لطفا دوباره تلاش کنید یا با پشتیبانی تماس بگیرید.",
		"PurchaseSuccessful": "✅ *خرید موفق!*\n\n" +
			"📦 *پلن:* %s\n" +
			"🆔 *شناسه اشتراک:* #%d\n" +
			"📅 *انقضا:* %s\n\n" +
			"اشتراک شما فعال است. برای دریافت کانفیگ روی دکمه زیر کلیک کنید.",
		"BtnGetConfig": "📄 دریافت کانفیگ",
		"BtnQRCode":    "📱 کد QR",
		"BtnMainMenu":  "🔙 منوی اصلی",
		"PaymentVerification": "📝 *تایید پرداخت*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"لطفاً **هش تراکنش (TXID)** یا **اسکرین‌شات** پرداخت را ارسال کنید.\n\n" +
			"_برای لغو روی دکمه لغو کلیک کنید._",
		"CardPaymentReceipt": "📸 *ارسال رسید پرداخت*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"لطفاً تصویر رسید یا شماره پیگیری تراکنش خود را ارسال کنید.\n\n" +
			"_برای لغو روی دکمه زیر کلیک کنید._",
		"SessionExpired":      "❌ نشست منقضی شد. لطفا دوباره شروع کنید.",
		"InvalidProof":        "⚠️ رسید نامعتبر. لطفا یک TXID معتبر یا اسکرین‌شات ارسال کنید.",
		"PaymentError":        "❌ خطا در پردازش پرداخت. لطفا با پشتیبانی تماس بگیرید.",
		"ProofSubmittedShort": "✅ رسید ارسال شد! شماره پیگیری: #%d\nادمین‌ها مطلع شدند.",
		"ProofSubmitted": "✅ *رسید ارسال شد!*\n\n" +
			"🆔 *شماره پیگیری:* #%d\n" +
			"📦 *پلن:* %s\n" +
			"💰 *مبلغ:* $%.2f\n" +
			"📝 *رسید:* `%s`\n\n" +
			"ادمین‌ها مطلع شدند. اشتراک شما پس از تایید به صورت خودکار فعال خواهد شد.",
		"InvalidPlanSelection": "❌ انتخاب پلن نامعتبر.",

		// === Subscription Handler ===
		"TrialProcessing":  "⏳ در حال پردازش درخواست تست...",
		"TrialSettingUp":   "⏳ *در حال راه‌اندازی تست رایگان...*\nلطفا صبر کنید.",
		"TrialAlreadyUsed": "❌ *تست رایگان موجود نیست*\n\nشما قبلا از تست رایگان خود استفاده کرده‌اید.",
		"TrialNoPlans":     "⚠️ *تست رایگان موجود نیست*\n\nدر حال حاضر پلن تست رایگانی فعال نیست.",
		"TrialNoServers":   "⚠️ *تست رایگان موجود نیست*\n\nپلن تست رایگان سروری تخصیص داده نشده است. لطفاً با پشتیبانی تماس بگیرید.",
		"TrialFailed":      "❌ فعال‌سازی ناموفق: %s",
		"TrialNextStep":    "🎉 آماده‌اید! هر زمان روی 🛍️ *سرویس‌های من* بزنید تا کانفیگ خود را ببینید.",
		"BuyNoServers":     "⚠️ این پلن در حال حاضر سرور در دسترس ندارد. پلن دیگری را امتحان کنید یا با پشتیبانی تماس بگیرید.",
		"TrialActivated": "🚀 *تست رایگان فعال شد!*\n\n" +
			"📦 *پلن:* %s\n" +
			"📊 *محدودیت:* %s\n" +
			"⏱ *انقضا:* %s\n\n" +
			"کلید اتصال شما:",
		"ConfigFile":     "📄 *فایل کانفیگ*\nاین فایل را در کلاینت %s وارد کنید.",
		"BtnBuyPremium":  "💳 خرید اشتراک پرمیوم",
		"SubKeyText":     "🔐 *کلید اشتراک*\n\nبرای کپی کلیک کنید:\n`%s`",
		"SubKeyQR":       "🔐 *کلید / QR*\n\nبرای کپی تپ کنید:\n`%s`",
		"ErrFetchSubs":   "❌ خطا در دریافت سرویس‌ها.",
		"NoSubsYet":      "📭 هنوز سرویسی ندارید.",
		"MySubsTitle":    "📂 *سرویس‌های من* (صفحه %d)\n━━━━━━━━━━━━━━━━━━━━\n\n",
		"BtnRefreshList": "🔄 بروزرسانی لیست",
		"TimeLeft":       "⏳ %s مانده",
		"SubNotFound":    "❌ سرویس پیدا نشد",
		"ErrOccurred":    "❌ خطایی رخ داد.",
		"AccessDenied":   "❌ دسترسی غیرمجاز.",
		"SubDetails": "📊 *%s*\n" +
			"%s *%s*\n\n" +
			"📦 *پلن:* %s\n" +
			"%s\n\n" +
			"━━━━━━━ *مصرف داده* ━━━━━━━\n" +
			"📈 *مصرف شده:* %s / %s\n" +
			"%s %.0f%%\n" +
			"📉 *باقیمانده:* %s\n\n" +
			"━━━━━━━ *اعتبار* ━━━━━━━\n" +
			"📅 *انقضا:* %s\n" +
			"⏳ *%s*\n\n" +
			"🌐 *سرورها:* %d    🕒 *از:* %s",
		"StatusOnline":       "🟢 آنلاین",
		"LastActive":         "🟢 آخرین فعالیت: %s",
		"WgNotConnected":     "⚪ هنوز متصل نشده",
		"BtnRegenLink":       "🔗 لینک جدید",
		"SubLinkRegenerated": "🔗 *لینک اشتراک جدید*\n\nلینک قبلی غیرفعال شد. اپ‌های نصب‌شده کار می‌کنند — این لینک را به اشتراک بگذارید:\n\n`%s`",
		"UnlimitedData":      "♾️ نامحدود",
		"BtnSubLink":         "📲 لینک اشتراک",
		"ServersTitle":       "🌐 *سرورها* (%d)\n\nبرای QR و لینک، یک سرور را انتخاب کنید:",
		"BtnAllConfigs":      "📋 همه کانفیگ‌ها",
		"BtnRename":          "✏️ تغییر نام",
		"BtnRegenKey":        "🔄 ساخت کلید جدید",
		"BtnBackToList":      "🔙 بازگشت به لیست",
		"BtnRenewNow":        "⚠️ تمدید فوری",
		"BtnRenewSoon":       "🔔 تمدید نزدیک",
		"BtnRenew":           "🔄 تمدید",
		"BtnRenewSub":        "🔄 تمدید اشتراک",
		"BtnRenewMetered":    "🔄 دوره جدید (صفر شدن مصرف)",
		"BtnRenewMeteredNow": "⚠️ دوره جدید فوری (صفر شدن مصرف)",
		"GeneratingConfig":   "📄 در حال ساخت کانفیگ...",
		"ErrFetchSub":        "❌ خطا در دریافت سرویس",
		"SubNotActive":       "❌ سرویس فعال نیست.",
		"ErrGenerateConfig":  "❌ خطا در ساخت کانفیگ. لطفا دوباره تلاش کنید یا با پشتیبانی تماس بگیرید.",
		"ConfigTitle":        "📄 *کانفیگ*\n━━━━━━━━━━━━━━━━━━━━\n\n",
		"ConfigN":            "*کانفیگ %d:*\n",
		"ConfigTapCopy":      "💡 _برای کپی روی کانفیگ تپ کنید._",
		"BtnShowQR":          "📱 نمایش QR",
		"GeneratingQR":       "📱 در حال ساخت کد QR...",
		"NoConfigAvailable":  "❌ کانفیگ موجود نیست",
		"QRTooLong":          "📱 *کلید اشتراک*\n\n⚠️ لینک برای کد QR خیلی طولانی است.\n\nبرای کپی کلیک کنید:\n`%s`",
		"QRCode":             "📱 *کد QR*\n\nبا کلاینت VPN اسکن کنید یا برای کپی تپ کنید:\n`%s`",
		"QRCaptionN":         "📱 *کانفیگ %d*\n\n`%s`",
		"QRAlbumNav":         "📱 *کدهای QR آماده شد* — هر کدام را با کلاینت VPN اسکن کنید یا برای کپی روی آن تپ کنید.",
		"SubLinkTitle": "📲 *لینک اشتراک*\n\n" +
			"این آدرس را در کلاینت V2Ray اضافه کنید:\n\n" +
			"`%s`\n\n" +
			"💡 *نحوه استفاده:*\n" +
			"• V2RayNG: اشتراک → + → آدرس را پیست کنید\n" +
			"• Streisand: افزودن اشتراک → آدرس را پیست کنید\n" +
			"• Shadowrocket: افزودن اشتراک → آدرس را پیست کنید",
		"SubLinkNotConfigured": "❌ لینک اشتراک تنظیم نشده. با ادمین تماس بگیرید.",
		"RenameTitle":          "✏️ *تغییر نام سرویس*\n\nنام جدید برای سرویس #%d وارد کنید:",
		"NameTooLong":          "❌ نام خیلی طولانی است (حداکثر ۲۰ کاراکتر). دوباره امتحان کنید:",
		"NameEmpty":            "❌ نام نمی‌تواند خالی باشد. دوباره امتحان کنید:",
		"SessionError":         "❌ خطای نشست. لطفا دوباره تلاش کنید.",
		"FailedUpdateName":     "❌ خطا در بروزرسانی نام.",
		"RenamedTo":            "✅ تغییر نام به *%s*",
		"RegenWarning": "⚠️ *آیا مطمئن هستید؟*\n\n" +
			"ساخت کلید جدید **اتصال فعلی شما را غیرفعال** خواهد کرد.\n" +
			"باید کانفیگ را روی تمام دستگاه‌هایتان بروز کنید.",
		"BtnYesRegenerate": "✅ بله، کلید جدید بساز",
		"Regenerating":     "🔄 در حال ساخت کلید جدید...",
		"Processing":       "🔄 *در حال پردازش...* لطفا صبر کنید.",
		"RegenFailed":      "❌ خطا در ساخت کلید جدید: %s",
		"KeyRegenerated":   "✅ *کلید جدید با موفقیت ساخته شد!*",
		"PlanNotFoundSub":  "❌ پلن این سرویس پیدا نشد",
		"RenewTitle": "🔄 *تمدید اشتراک*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"📦 *پلن:* %s\n" +
			"💰 *قیمت:* $%.2f\n\n" +
			"📅 *انقضای فعلی:* %s\n" +
			"📅 *انقضای جدید:* %s\n\n" +
			"%s",
		"RenewCrypto":  "💳 تمدید با رمزارز",
		"RenewCard":    "💳 تمدید کارت به کارت",
		"RenewBalance": "💰 تمدید از موجودی",
		"RenewCryptoInstructions": "🪙 *تمدید با رمزارز*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *پلن:* %s\n" +
			"💰 *مبلغ:* `$%.2f`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔸 *USDT (BSC - BEP20):*\n`%s`\n\n" +
			"🔸 *Bitcoin (BTC):*\n`%s`\n\n" +
			"🔸 *Monero (XMR):*\n`%s`\n\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"لطفاً مبلغ دقیق را واریز کنید.",
		"RenewCryptoSingle": "🪙 *تمدید با رمزارز*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *پلن:* %s\n" +
			"💰 *مبلغ:* `$%.2f`\n" +
			"🪙 *پرداخت با:* %s\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔑 *آدرس:*\n`%s`\n\n" +
			"مبلغ دقیق را واریز کنید، سپس هش تراکنش یا اسکرین‌شات را ارسال کنید.",
		"RenewCardInstructions": "💳 *تمدید کارت به کارت*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"📦 *پلن:* %s\n" +
			"💵 *مبلغ:* `%s تومان`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🏦 *شماره کارت:*\n`%s`\n" +
			"👤 *به نام:* %s\n\n" +
			"لطفاً مبلغ دقیق را واریز کنید.",
		"RenewProofReceived": "✅ *رسید تمدید دریافت شد!*\n\n" +
			"🆔 *شماره پیگیری:* #%d\n" +
			"📦 *پلن:* %s\n\n" +
			"ادمین‌ها مطلع شدند.",
		"RenewInsufficientBalance": "❌ *موجودی ناکافی*\n\n" +
			"💰 *قیمت تمدید:* $%.2f\n" +
			"💳 *موجودی شما:* $%.2f\n" +
			"📉 *کسری:* $%.2f\n\n" +
			"لطفا حساب خود را شارژ کنید یا از طریق رمزارز/کارت تمدید کنید.",
		"RenewError":         "❌ *تمدید ناموفق*\n\nخطایی رخ داد. لطفا دوباره تلاش کنید یا با پشتیبانی تماس بگیرید.",
		"RenewSuccess":       "✅ *اشتراک تمدید شد!*\n\n📦 *پلن:* %s\n\nاشتراک شما تمدید شد.",
		"BtnAddData":         "➕ افزودن حجم",
		"BtnAddDataPriced":   "➕ افزودن حجم ($%.2f/GB)",
		"BtnContinue":        "ادامه ➡️",
		"TopUpNotAllowed":    "افزودن حجم برای این پلن فعال نیست.",
		"TopUpNeedsActive":   "افزودن حجم فقط روی اشتراک فعال ممکن است. لطفاً تمدید کنید.",
		"RenewNotAllowed":    "تمدید برای این پلن فعال نیست.",
		"PlanInactive":       "❌ این پلن غیرفعال شده است. تمدید و افزودن حجم برای آن دیگر در دسترس نیست.",
		"TopUpError":         "❌ افزودن حجم ناموفق بود. دوباره تلاش کنید.",
		"TopUpKeepsExpiry":   "تاریخ انقضا تغییر نمی‌کند.",
		"TopUpExtendsExpiry": "تاریخ انقضا تمدید می‌شود.",
		"TopUpStepper": "➕ *افزودن حجم — %s*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"*+%d گیگ* — $%.2f\n%s",
		"TopUpMethod": "➕ *افزودن حجم — %s*\n\n*+%d گیگ* — $%.2f\n\nروش پرداخت را انتخاب کنید:",
		"TopUpConfirm": "➕ *تأیید افزودن حجم*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"*+%d گیگ* — $%.2f\n\n" +
			"💳 *موجودی:* $%.2f\n💳 *پس از پرداخت:* $%.2f",
		"TopUpSuccess":       "✅ *%d گیگ به اشتراک شما اضافه شد.*",
		"TopUpProofReceived": "✅ *درخواست افزودن حجم #%d ثبت شد* (+%d گیگ)\n\nپس از تأیید پرداخت توسط ادمین اعمال می‌شود.",
		"TopUpCryptoInstructions": "🪙 *افزودن حجم — +%d گیگ*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *مبلغ:* `$%.2f`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔸 *USDT (BEP20):*\n`%s`\n\n🔸 *BTC:*\n`%s`\n\n🔸 *XMR:*\n`%s`\n\n" +
			"مبلغ دقیق را ارسال و سپس هش تراکنش یا تصویر رسید را بفرستید.",
		"TopUpCryptoSingle": "🪙 *افزودن حجم — +%d گیگ*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *مبلغ:* `$%.2f`\n" +
			"🪙 *پرداخت با:* %s\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔑 *آدرس:*\n`%s`\n\n" +
			"مبلغ دقیق را ارسال و سپس هش تراکنش یا تصویر رسید را بفرستید.",
		"TopUpCardInstructions": "💳 *افزودن حجم — +%d گیگ*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *مبلغ:* %s تومان\n\n" +
			"🔸 *کارت:* `%s`\n🔸 *به نام:* %s\n\n" +
			"پرداخت کنید و سپس تصویر رسید را بفرستید.",
		"RenewMeteredStepper": "🔄 *تمدید — %s*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"*%d گیگ* — $%.2f\n+%d روز، حجم تازه",
		"RenewMeteredMethod": "🔄 *تمدید — %s*\n\n*%d گیگ* — $%.2f\n+%d روز\n\nروش پرداخت را انتخاب کنید:",
		"RenewMeteredConfirm": "🔄 *تأیید تمدید*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"*%d گیگ* — $%.2f\n\n" +
			"💳 *موجودی:* $%.2f\n💳 *پس از پرداخت:* $%.2f",
		"RenewMeteredDiscardWarn": "⚠️ هنوز %.1f گیگ استفاده‌نشده دارید. تمدید حجم تازه می‌سازد و آن را حذف می‌کند.\n\n",
		"RenewMeteredCryptoInstructions": "🪙 *تمدید — %s (%d گیگ)*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *مبلغ:* `$%.2f`\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔸 *USDT (BEP20):*\n`%s`\n\n🔸 *BTC:*\n`%s`\n\n🔸 *XMR:*\n`%s`\n\n" +
			"مبلغ دقیق را ارسال و سپس هش تراکنش یا تصویر رسید را بفرستید.",
		"RenewMeteredCryptoSingle": "🪙 *تمدید — %s (%d گیگ)*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *مبلغ:* `$%.2f`\n" +
			"🪙 *پرداخت با:* %s\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"🔑 *آدرس:*\n`%s`\n\n" +
			"مبلغ دقیق را ارسال و سپس هش تراکنش یا تصویر رسید را بفرستید.",
		"RenewMeteredCardInstructions": "💳 *تمدید — %s (%d گیگ)*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n" +
			"💰 *مبلغ:* %s تومان\n\n" +
			"🔸 *کارت:* `%s`\n🔸 *به نام:* %s\n\n" +
			"پرداخت کنید و سپس تصویر رسید را بفرستید.",
		"BalanceSufficient":        "✅ موجودی شما: $%.2f",
		"BalanceInsufficient":      "❌ موجودی شما: $%.2f (نیاز به $%.2f بیشتر)",
		"BtnConfirmRenewal":        "✅ تایید تمدید",
		"BtnTopUpBalance":          "💳 شارژ موجودی",
		"ProcessingRenewal":        "🔄 در حال پردازش تمدید...",
		"RenewalProcessing":        "🔄 *در حال پردازش تمدید...* لطفا صبر کنید.",
		"InsufficientBalanceRenew": "❌ *موجودی ناکافی*\n\nلطفا حساب خود را شارژ کنید.",
		"RenewalFailed":            "❌ تمدید ناموفق: %s",
		"SubRenewed": "✅ *اشتراک تمدید شد!*\n\n" +
			"📦 *پلن:* %s\n" +
			"📅 *انقضای جدید:* %s\n\n" +
			"اشتراک شما اکنون فعال است.",
		"BtnMySubscriptions": "📂 سرویس‌های من",

		// === Approval Notifications ===
		"NotifyRenewalApproved": "🎉 *تمدید تأیید شد!*\n\n" +
			"پرداخت شما تأیید شد و اشتراکتان تمدید شد.\n\n" +
			"📋 *شناسه سرویس:* #%d\n" +
			"📅 *انقضای جدید:* %s\n\n" +
			"از /subscriptions برای مشاهده کانفیگ استفاده کنید.",
		"NotifyRenewalFailed": "✅ *پرداخت تأیید شد!*\n\n⚠️ تمدید اشتراک ناموفق بود: %s\nلطفاً با ادمین تماس بگیرید.",
		"NotifyPaymentApproved": "🎉 *پرداخت تأیید شد!*\n\n" +
			"پرداخت شما تأیید شد و اشتراکتان فعال است.\n\n" +
			"📋 *شناسه سرویس:* #%d\n" +
			"📅 *انقضا:* %s\n\n" +
			"از /subscriptions برای مشاهده کانفیگ استفاده کنید.",
		"NotifyPlanActivated":         "✅ *پرداخت تأیید شد!*\n\nپرداخت‌شده: `$%.2f`\nاشتراک با موفقیت فعال شد!",
		"NotifyBalanceAddFailed":      "✅ *پرداخت تأیید شد!*\n\nپرداخت‌شده: `$%.2f %s`\n\n⚠️ فعال‌سازی خودکار اشتراک ناموفق بود: %s\nلطفاً دستی فعال کنید.",
		"NotifyTopUp":                 "✅ *پرداخت تأیید شد!*\n\nموجودی شما به مبلغ `%.2f %s` شارژ شد.",
		"NotifyPaymentRejected":       "❌ *پرداخت رد شد*\n\nپرداخت شما تأیید نشد. اگر فکر می‌کنید اشتباهی رخ داده، با پشتیبانی تماس بگیرید.",
		"NotifyPaymentRejectedReason": "❌ *پرداخت رد شد*\n\nدلیل: %s",
		"NotifyUsdtVerifying":         "⏳ *پرداخت دریافت شد*\n\nتراکنش شما پیدا شد و منتظر تأیید شبکه هستیم. پس از تأیید، پرداخت به‌صورت خودکار تأیید می‌شود.",
		"PaymentVerifyUsdtHash":       "💡 *هش تراکنش BSC* خود (`0x…`) را ارسال کنید تا پرداخت‌تان به‌صورت خودکار بررسی و تأیید شود.",

		// === Payment History Labels ===
		"PayDescPurchase":    "خرید پلن: %s",
		"PayDescRenewal":     "تمدید اشتراک: %s",
		"PayStatusCompleted": "تکمیل شده",
		"PayStatusPending":   "در انتظار",
		"PayStatusFailed":    "ناموفق",
		"PayStatusRefunded":  "بازگشت وجه",
		"PayMethodCrypto":    "رمزارز",
		"PayMethodCard":      "کارت",
		"PayMethodBalance":   "موجودی",

		// === Plan Detail Labels ===
		"PlanPrice":    "قیمت:",
		"PlanDuration": "مدت زمان:",
		"PlanDays":     "%d روز",
		"PlanData":     "حجم:",

		// === Time Formatting ===
		"TimeDaysHours":   "%d روز %d ساعت",
		"TimeDays":        "%d روز",
		"TimeHours":       "%d ساعت",
		"TimeMinutes":     "%d دقیقه",
		"TimeLessThanMin": "کمتر از ۱ دقیقه",
		"TimeUnlimited":   "نامحدود",
		"TimeExpired":     "منقضی شده",

		// === Status Text ===
		"StatusTextActive":           "فعال",
		"StatusTextExpired":          "منقضی",
		"StatusTextPending":          "در انتظار",
		"StatusTextCancelled":        "لغو شده",
		"StatusTextPaused":           "متوقف",
		"StatusTextTrafficExhausted": "حجم تمام شده",

		// === Data Limit ===
		"DataLimitUnlimited": "♾️ نامحدود",

		// === Expiration Notifications ===
		"NotifExpired":        "🚫 *اشتراک منقضی شد*\n\nاشتراک **%s** شما منقضی شده است.\n\nبرای بازگشت دسترسی همین الان تمدید کنید.",
		"NotifExpiry1Day":     "⚠️ *یادآوری نهایی*\n\nاشتراک **%s** شما **فردا** منقضی می‌شود.\n\n📅 انقضا: %s",
		"NotifExpiry3Days":    "🔔 *هشدار انقضا*\n\nاشتراک **%s** شما تا **%d روز** دیگر منقضی می‌شود.\n\n📅 انقضا: %s",
		"NotifExpiry7Days":    "📅 *یادآوری اشتراک*\n\nاشتراک **%s** شما تا **%d روز** دیگر منقضی می‌شود.\n\n📅 انقضا: %s",
		"BtnRenewNowNotif":    "🔄 تمدید فوری",
		"BtnViewDetailsNotif": "📊 مشاهده جزئیات",
		"BtnViewUsageNotif":   "📊 مشاهده مصرف",

		// === Data Usage Notifications ===
		"NotifDataExhausted": "🚫 *حجم تمام شد*\n\nاشتراک **%s** شما به ۱۰۰٪ حجم مجاز رسیده است.\n\nدسترسی ممکن است محدود شود تا زمانی که حجم اضافه کنید یا تمدید کنید.",
		"NotifData90":        "⚠️ *هشدار مصرف داده*\n\nاشتراک **%s** شما **۹۰٪** از حجم مجاز را مصرف کرده است.",
		"NotifData75":        "🔔 *اخطار مصرف داده*\n\nاشتراک **%s** شما **۷۵٪** از حجم مجاز را مصرف کرده است.",
		"NotifData50":        "ℹ️ *اعلان مصرف داده*\n\nاشتراک **%s** شما **۵۰٪** از حجم مجاز را مصرف کرده است.",

		// === Partial Provisioning ===
		"PartialProvisioning": "⚠️ برخی سرورها موقتا در دسترس نیستند. اشتراک شما فعال است و سرورهای باقیمانده به‌زودی به صورت خودکار اضافه خواهند شد.",

		// === Balance Confirmation ===
		"ConfirmBalanceBuy": "💰 *تایید خرید*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"📦 *پلن:* %s\n" +
			"💰 *قیمت:* $%.2f\n\n" +
			"💳 *موجودی فعلی:* $%.2f\n" +
			"💳 *موجودی باقیمانده:* $%.2f\n\n" +
			"آیا مطمئن هستید که می‌خواهید ادامه دهید؟",
		"ConfirmBalanceRenew": "💰 *تایید تمدید*\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"📦 *پلن:* %s\n" +
			"💰 *قیمت:* $%.2f\n\n" +
			"💳 *موجودی فعلی:* $%.2f\n" +
			"💳 *موجودی باقیمانده:* $%.2f\n\n" +
			"آیا مطمئن هستید که می‌خواهید تمدید کنید؟",
		"BtnConfirmPurchase": "✅ تایید خرید",

		// === Bot / Transport ===
		"SessionExpiredStart": "⏰ نشست شما منقضی شد. لطفا دوباره شروع کنید.",
		"Cancelled":           "❌ لغو شد.",
		"MainMenuText":        "🏠 منوی اصلی",
		"AdminMenuText":       "🛠 منوی مدیریت",
		"AdminPanelText":      "🛠 پنل مدیریت",

		// Maintenance mode notice (Farsi)
		"MaintenanceNotice": "🛠 در حال انجام نگهداری سرویس.\nخرید، تمدید و تولید مجدد کانفیگ موقتاً متوقف شده است. کانفیگ‌های موجود همچنان کار می‌کنند.\nلطفاً دقایقی دیگر دوباره امتحان کنید.",
	},
}

// Get translates a key for a specific language
func Get(lang string, key string, args ...interface{}) string {
	// Fallback to English if language is not supported
	if _, ok := translations[lang]; !ok {
		lang = LangEN
	}

	dict := translations[lang]
	if str, ok := dict[key]; ok {
		if len(args) > 0 {
			return fmt.Sprintf(str, args...)
		}
		return str
	}

	// Fallback to English if key is missing in the target language
	if lang != LangEN {
		if str, ok := translations[LangEN][key]; ok {
			if len(args) > 0 {
				return fmt.Sprintf(str, args...)
			}
			return str
		}
	}

	return key // Returns the key if completely missing
}

// GetMD is Get for Markdown messages: it escapes string args so user values
// can't break rendering. Numbers pass through untouched.
func GetMD(lang string, key string, args ...interface{}) string {
	escaped := make([]interface{}, len(args))
	for i, a := range args {
		if s, ok := a.(string); ok {
			escaped[i] = utils.EscapeMarkdown(s)
		} else {
			escaped[i] = a
		}
	}
	return Get(lang, key, escaped...)
}

// GetLangName returns the human-readable language name
func GetLangName(lang string) string {
	switch lang {
	case LangFA:
		return "فارسی"
	case LangEN:
		return "English"
	default:
		return "English"
	}
}
