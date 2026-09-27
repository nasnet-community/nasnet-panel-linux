package i18n

func init() {
	for lang, entries := range map[string]Dictionary{
		LangEN: {
			"SubCreatedIdentity": "🗓 *Created:* %s",
			"SubStableIdentity":  "🆔 *Service ID:* %s",
			"SubRenameHint":      "✏️ Open a service and use Rename to give it a recognizable name.",
			"SubNoExpiry":        "No expiry date",
		},
		LangFA: {
			"SubCreatedIdentity": "🗓 *ایجاد:* %s",
			"SubStableIdentity":  "🆔 *شناسه سرویس:* %s",
			"SubRenameHint":      "✏️ داخل هر سرویس، با «تغییر نام» نامی مثل «سرویس یک» انتخاب کنید.",
			"SubNoExpiry":        "بدون تاریخ انقضا",
		},
	} {
		for key, value := range entries {
			translations[lang][key] = value
		}
	}
}
