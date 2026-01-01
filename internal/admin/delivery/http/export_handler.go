package http

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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

	users, _, err := h.userRepo.ListAll(c.Request.Context(), "", "", "id", "asc", 0, 100000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	filename := fmt.Sprintf("users_%s", time.Now().Format("20060102"))

	if format == "json" {
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.json", filename))
		c.Header("Content-Type", "application/json")
		c.JSON(http.StatusOK, users)
		return
	}

	// CSV
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
	c.Header("Content-Type", "text/csv")
	c.Status(http.StatusOK)

	w := csv.NewWriter(c.Writer)
	defer w.Flush()

	w.Write([]string{"ID", "TelegramID", "Username", "FirstName", "LastName", "IsAdmin", "IsBanned", "CreatedAt"})

	for _, u := range users {
		w.Write([]string{
			fmt.Sprint(u.ID),
			fmt.Sprint(u.TelegramID),
			u.Username,
			u.FirstName,
			u.LastName,
			fmt.Sprint(u.IsAdmin),
			fmt.Sprint(u.IsBanned),
			u.CreatedAt.Format(time.RFC3339),
		})
	}
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
