package http

import (
	"encoding/csv"
	"fmt"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	userRepo "github.com/nasnet-community/nasnet-panel-linux/internal/user/repository"
)

// ExportHandler handles data export endpoints
type ExportHandler struct {
	userRepo userRepo.UserRepository
	subRepo  subRepo.SubscriptionRepository
}

func NewExportHandler(
	userRepo userRepo.UserRepository,
	subRepo subRepo.SubscriptionRepository,
) *ExportHandler {
	return &ExportHandler{
		userRepo: userRepo,
		subRepo:  subRepo,
	}
}

func (h *ExportHandler) RegisterRoutes(rg *gin.RouterGroup) {
	export := rg.Group("/admin/export")
	{
		export.GET("/users", h.ExportUsers)
		export.GET("/subscriptions", h.ExportSubscriptions)
	}
}

func (h *ExportHandler) ExportUsers(c *gin.Context) {
	format := c.DefaultQuery("format", "csv")
	q := parseUserListQuery(c, time.Now())
	q.IDs = parseIDs(c.Query("ids"))
	q.Offset, q.Limit = 0, 0

	page, err := h.userRepo.ListForAdmin(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	group := string(q.Group)
	if group == "" {
		group = "all"
	}
	filename := fmt.Sprintf("users_%s_%s", group, time.Now().Format("20060102"))

	if format == "json" {
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.json", filename))
		c.Header("Content-Type", "application/json")
		c.JSON(http.StatusOK, page.Items)
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
	c.Header("Content-Type", "text/csv")
	c.Status(http.StatusOK)

	w := csv.NewWriter(c.Writer)
	defer w.Flush()
	w.Write(userExportHeader)
	for _, u := range page.Items {
		w.Write(userExportRow(u))
	}
}

var userExportHeader = []string{
	"ID", "TelegramID", "Username", "FirstName", "LastName", "Telegram", "Group", "Subscription", "SubscriptionStatus",
	"EndDate", "DataUsedGB", "DataLimitGB", "LastSeen", "LifetimeDataGB", "IsAdmin", "IsBanned", "JoinedAt",
}

func userExportRow(u *adminDomain.UserListItem) []string {
	telegram := "none"
	if u.TelegramID > 0 {
		telegram = "linked"
	}
	var title, status, end, used, limit string
	if s := u.Subscription; s != nil {
		title = s.Label
		status = s.Status
		if s.EndDate != nil {
			end = s.EndDate.UTC().Format(time.RFC3339)
		}
		used = exportGB(s.DataUsed)
		limit = "unlimited"
		if s.DataLimit > 0 {
			limit = exportGB(s.DataLimit)
		}
	}
	lastSeen := ""
	if u.LastActiveAt != nil {
		lastSeen = *u.LastActiveAt
	}
	return []string{
		strconv.FormatUint(uint64(u.ID), 10), strconv.FormatInt(u.TelegramID, 10),
		httputil.CSVText(u.Username), httputil.CSVText(u.FirstName), httputil.CSVText(u.LastName),
		telegram, string(u.Group), httputil.CSVText(title), status, end, used, limit, lastSeen, exportGB(u.DataUsed),
		strconv.FormatBool(u.IsAdmin), strconv.FormatBool(u.IsBanned), u.CreatedAt,
	}
}

func exportGB(bytes int64) string {
	return strconv.FormatFloat(float64(bytes)/(1<<30), 'f', 2, 64)
}

func (h *ExportHandler) ExportSubscriptions(c *gin.Context) {
	format := c.DefaultQuery("format", "csv")
	status := c.Query("status")

	subs, err := h.subRepo.ListAll(c.Request.Context(), status, 0, 100000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	filename := fmt.Sprintf("subscriptions_%s", time.Now().Format("20060102"))

	if format == "json" {
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.json", filename))
		c.Header("Content-Type", "application/json")
		c.JSON(http.StatusOK, subs)
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
	c.Header("Content-Type", "text/csv")
	c.Status(http.StatusOK)

	w := csv.NewWriter(c.Writer)
	defer w.Flush()

	w.Write([]string{"ID", "UserID", "Status", "DataUsed", "DataLimit", "StartDate", "EndDate", "CreatedAt"})

	for _, s := range subs {
		endDate := ""
		if s.EndDate != nil {
			endDate = s.EndDate.Format(time.RFC3339)
		}
		w.Write([]string{
			fmt.Sprint(s.ID),
			fmt.Sprint(s.GetUserID()),
			string(s.Status),
			fmt.Sprint(s.DataUsed),
			fmt.Sprint(s.DataLimit),
			s.CreatedAt.Format(time.RFC3339),
			endDate,
			s.CreatedAt.Format(time.RFC3339),
		})
	}
}
