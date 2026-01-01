package tool

import (
	"os"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

var sensitiveKeys = map[string]bool{
	"DB_PASSWORD":         true,
	"JWT_SECRET_KEY":      true,
	"ADMIN_PASSWORD_HASH": true,
	"TELEGRAM_BOT_TOKEN":  true,
	"GITHUB_TOKEN":        true,
}

// WizardViewConfig reads the .env file and displays all key/value pairs in a
// table, masking the values of known sensitive keys.
func WizardViewConfig(cfg *Config) {
	ui.ClearScreen()
	ui.DrawBox("Current Configuration")

	data, err := os.ReadFile(cfg.EnvFile)
	if err != nil {
		ui.StepFail("Could not read .env: " + err.Error())
		ui.PressAnyKey()
		return
	}

	headers := []string{"Key", "Value"}
	var rows [][]string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := strings.Trim(parts[1], "'\"")
		if sensitiveKeys[key] {
			val = MaskSecret(val, 4)
		}
		rows = append(rows, []string{key, val})
	}

	ui.Table(headers, rows)
	ui.StepInfo("File: " + cfg.EnvFile)
	ui.PressAnyKey()
}
