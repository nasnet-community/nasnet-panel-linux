package telegram

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"gopkg.in/telebot.v3"
)

// HandleExport generates and sends a CSV export file
// Usage: /export users | /export subscriptions | /export payments
func (h *Handler) HandleExport(c telebot.Context) error {
	args := c.Args()
	if len(args) == 0 {
		return c.Send("📤 *Export Data*\n\nUsage:\n• `/export users` - Export all users\n• `/export subscriptions` - Export all subscriptions", telebot.ModeMarkdown)
	}

	target := strings.ToLower(args[0])
	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 2*time.Minute)
	defer cancel()

	var filePath string
	var err error

	switch target {
	case "users":
		filePath, err = h.exportUsers(ctx)
	case "subscriptions":
		filePath, err = h.exportSubscriptions(ctx)
	default:
		return c.Send("Unknown export type. Use: `users` or `subscriptions`.", telebot.ModeMarkdown)
	}

	if err != nil {
		return c.Send(fmt.Sprintf("Export failed: %v", err))
	}
	defer os.Remove(filePath)

	doc := &telebot.Document{
		File:     telebot.FromDisk(filePath),
		FileName: filepath.Base(filePath),
		Caption:  fmt.Sprintf("📤 Export: %s (%s)", target, time.Now().Format("2006-01-02 15:04")),
	}

	return c.Send(doc)
}

func (h *Handler) exportUsers(ctx context.Context) (string, error) {
	users, _, err := h.adminUC.ListUsers(ctx, "", "", "created_at", "desc", 0, 100000)
	if err != nil {
		return "", fmt.Errorf("failed to list users: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "users_export_*.csv")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	filePath := tmpFile.Name()
	f := tmpFile
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Header
	if err := w.Write([]string{"ID", "TelegramID", "Username", "FirstName", "LastName", "IsAdmin", "IsBanned", "ActiveSubs", "TotalSubs", "CreatedAt"}); err != nil {
		return "", err
	}

	for _, u := range users {
		if err := w.Write([]string{
			strconv.FormatUint(uint64(u.ID), 10),
			strconv.FormatInt(u.TelegramID, 10),
			u.Username,
			u.FirstName,
			u.LastName,
			strconv.FormatBool(u.IsAdmin),
			strconv.FormatBool(u.IsBanned),
			strconv.Itoa(u.ActiveSubscriptions),
			strconv.Itoa(u.TotalSubscriptions),
			u.CreatedAt,
		}); err != nil {
			return "", err
		}
	}

	return filePath, nil
}

func (h *Handler) exportSubscriptions(ctx context.Context) (string, error) {
	subs, err := h.subUC.ListAllSubscriptions(ctx, "", 0, 100000)
	if err != nil {
		return "", fmt.Errorf("failed to list subscriptions: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "subscriptions_export_*.csv")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	filePath := tmpFile.Name()
	f := tmpFile
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"ID", "UserID", "Status", "ProductType", "Label", "StartDate", "EndDate", "DataUsed", "DataLimit", "ConfigEmail"}); err != nil {
		return "", err
	}

	for _, s := range subs {
		startDate := ""
		if s.StartDate != nil {
			startDate = s.StartDate.Format(time.RFC3339)
		}
		endDate := ""
		if s.EndDate != nil {
			endDate = s.EndDate.Format(time.RFC3339)
		}

		if err := w.Write([]string{
			strconv.FormatUint(uint64(s.ID), 10),
			strconv.FormatUint(uint64(s.GetUserID()), 10),
			string(s.Status),
			string(s.ProductType),
			s.Label,
			startDate,
			endDate,
			strconv.FormatInt(s.DataUsed, 10),
			strconv.FormatInt(s.DataLimit, 10),
			s.ConfigEmail,
		}); err != nil {
			return "", err
		}
	}

	return filePath, nil
}
