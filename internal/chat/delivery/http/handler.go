package http

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/auth/middleware"
	"github.com/nasnet-community/nasnet-panel-linux/internal/chat/domain"
	subUC "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
)

type jsonResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Meta    *pagination `json:"meta,omitempty"`
}

type pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type Handler struct {
	chatUC     domain.ChatUsecase
	subUsecase subUC.SubscriptionUsecase
}

func NewHandler(chatUC domain.ChatUsecase, subUsecase subUC.SubscriptionUsecase) *Handler {
	return &Handler{chatUC: chatUC, subUsecase: subUsecase}
}

func (h *Handler) RegisterPublicRoutes(rg *gin.RouterGroup, chatRateLimiter *ChatRateLimiter) {
	rg.GET("/chat", h.GetSubChatMessages)
	rg.POST("/chat", chatRateLimiter.Middleware(), h.SendSubChatMessage)
	rg.PATCH("/chat/:messageId", chatRateLimiter.Middleware(), h.EditSubChatMessage)
	rg.DELETE("/chat/:messageId", h.DeleteSubChatMessage)
	rg.PUT("/chat/read", h.MarkSubChatAsRead)
	rg.GET("/chat/messages/:messageId/reactions", h.ListSubMessageReactions)
	rg.POST("/chat/messages/:messageId/reactions", chatRateLimiter.Middleware(), h.AddSubMessageReaction)
	rg.PATCH("/chat/messages/:messageId/reactions/:emoji", chatRateLimiter.Middleware(), h.ReplaceSubMessageReaction)
	rg.DELETE("/chat/messages/:messageId/reactions/:emoji", h.RemoveSubMessageReaction)
}

type ChatRateLimiter struct {
	mu       sync.Mutex
	shortMap map[string]time.Time
	hourMap  map[string][]time.Time
	stopCh   chan struct{}
	doneCh   chan struct{}
	once     sync.Once
}

func NewChatRateLimiter() *ChatRateLimiter {
	rl := &ChatRateLimiter{
		shortMap: make(map[string]time.Time),
		hourMap:  make(map[string][]time.Time),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go rl.runCleanup()
	return rl
}

func (rl *ChatRateLimiter) runCleanup() {
	defer close(rl.doneCh)
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCh:
			return
		}
	}
}

// Shutdown signals the cleanup goroutine to exit and waits for it to finish.
// Safe to call concurrently or repeatedly; only the first call closes stopCh.
func (rl *ChatRateLimiter) Shutdown() {
	rl.once.Do(func() { close(rl.stopCh) })
	<-rl.doneCh
}

// checkLimit returns "" if allowed; otherwise an error message.
func (rl *ChatRateLimiter) checkLimit(key string, minGap time.Duration, hourCap int) string {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	if last, ok := rl.shortMap[key]; ok && now.Sub(last) < minGap {
		return "Please wait before sending another message"
	}
	hourAgo := now.Add(-1 * time.Hour)
	times := rl.hourMap[key]
	recent := times[:0]
	for _, t := range times {
		if t.After(hourAgo) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= hourCap {
		return "Message limit reached, please try again later"
	}
	rl.shortMap[key] = now
	rl.hourMap[key] = append(recent, now)
	return ""
}

func (rl *ChatRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		uuid := c.Param("uuid")
		if errMsg := rl.checkLimit(uuid, 3*time.Second, 30); errMsg != "" {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, jsonResponse{
				Success: false, Error: errMsg,
			})
			return
		}
		c.Next()
	}
}

func (rl *ChatRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	hourAgo := time.Now().Add(-1 * time.Hour)
	for k, times := range rl.hourMap {
		var recent []time.Time
		for _, t := range times {
			if t.After(hourAgo) {
				recent = append(recent, t)
			}
		}
		if len(recent) == 0 {
			delete(rl.hourMap, k)
			delete(rl.shortMap, k)
		} else {
			rl.hourMap[k] = recent
		}
	}
}

func (h *Handler) RegisterAdminRoutes(rg *gin.RouterGroup) {
	chat := rg.Group("/admin/chats")
	chat.GET("", h.ListConversations)
	chat.GET("/unread-count", h.GetUnreadCount)
	chat.GET("/:subscriptionId/messages", h.GetAdminChatMessages)
	chat.GET("/:subscriptionId/search", h.SearchAdminChatMessages)
	chat.POST("/:subscriptionId/messages", h.SendAdminChatMessage)
	chat.PATCH("/:subscriptionId/messages/:messageId", h.EditAdminChatMessage)
	chat.DELETE("/:subscriptionId/messages/:messageId", h.DeleteAdminChatMessage)
	chat.PUT("/:subscriptionId/read", h.MarkAsRead)
	chat.GET("/:subscriptionId/pinned", h.GetPinnedMessages)
	chat.PUT("/:subscriptionId/messages/:messageId/pin", h.PinMessage)
	chat.DELETE("/:subscriptionId/messages/:messageId/pin", h.UnpinMessage)
	chat.GET("/:subscriptionId/messages/:messageId/reactions", h.ListAdminMessageReactions)
	chat.POST("/:subscriptionId/messages/:messageId/reactions", h.AddAdminMessageReaction)
	chat.PATCH("/:subscriptionId/messages/:messageId/reactions/:emoji", h.ReplaceAdminMessageReaction)
	chat.DELETE("/:subscriptionId/messages/:messageId/reactions/:emoji", h.RemoveAdminMessageReaction)
}

func (h *Handler) GetSubChatMessages(c *gin.Context) {
	uuid := c.Param("uuid")
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Subscription not found"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	messages, total, err := h.chatUC.GetMessages(c.Request.Context(), sub.ID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Failed to fetch messages"})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data:    messages,
		Meta: &pagination{
			Page:       page,
			PerPage:    limit,
			Total:      total,
			TotalPages: int(math.Ceil(float64(total) / float64(limit))),
		},
	})
}

func (h *Handler) SendSubChatMessage(c *gin.Context) {
	uuid := c.Param("uuid")
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Subscription not found"})
		return
	}

	var req struct {
		Content          string `json:"content" binding:"required"`
		ReplyToMessageID *uint  `json:"reply_to_message_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Content is required"})
		return
	}

	msg, err := h.chatUC.SendMessage(c.Request.Context(), sub.ID, "user", nil, req.Content, req.ReplyToMessageID)
	if err != nil {
		if err.Error() == "chat is disabled" {
			c.JSON(http.StatusForbidden, jsonResponse{Success: false, Error: err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, jsonResponse{Success: true, Data: msg})
}

func (h *Handler) ListConversations(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	filters := domain.ConversationFilters{
		Page:       page,
		Limit:      limit,
		Search:     c.Query("search"),
		Status:     c.Query("status"),
		UnreadOnly: c.Query("unread") == "true",
		PinnedOnly: c.Query("pinned") == "true",
		SortBy:     c.Query("sort"),
	}
	if c.Query("mine") == "true" {
		if uid, ok := middleware.GetUserID(c); ok {
			v := uid
			filters.MineAdminID = &v
		}
	}

	conversations, total, err := h.chatUC.GetConversations(c.Request.Context(), filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Failed to fetch conversations"})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data:    conversations,
		Meta: &pagination{
			Page:       page,
			PerPage:    limit,
			Total:      total,
			TotalPages: int(math.Ceil(float64(total) / float64(limit))),
		},
	})
}

func (h *Handler) GetUnreadCount(c *gin.Context) {
	count, err := h.chatUC.GetTotalUnreadCount(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Failed to get unread count"})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true, Data: map[string]int64{"count": count}})
}

func (h *Handler) GetAdminChatMessages(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	messages, total, err := h.chatUC.GetMessages(c.Request.Context(), uint(subID), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Failed to fetch messages"})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data:    messages,
		Meta: &pagination{
			Page:       page,
			PerPage:    limit,
			Total:      total,
			TotalPages: int(math.Ceil(float64(total) / float64(limit))),
		},
	})
}

func (h *Handler) SendAdminChatMessage(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}

	var req struct {
		Content          string `json:"content" binding:"required"`
		ReplyToMessageID *uint  `json:"reply_to_message_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Content is required"})
		return
	}

	adminID, _ := middleware.GetUserID(c)
	msg, err := h.chatUC.SendMessage(c.Request.Context(), uint(subID), "admin", &adminID, req.Content, req.ReplyToMessageID)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, jsonResponse{Success: true, Data: msg})
}

func (h *Handler) MarkAsRead(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}

	if err := h.chatUC.MarkAsRead(c.Request.Context(), uint(subID), "admin"); err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Failed to mark as read"})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{Success: true})
}

func (h *Handler) PinMessage(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}

	if err := h.chatUC.PinMessage(c.Request.Context(), uint(messageID), uint(subID)); err != nil {
		if errors.Is(err, domain.ErrMessageNotFound) {
			c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Failed to pin message"})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{Success: true})
}

func (h *Handler) UnpinMessage(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}

	if err := h.chatUC.UnpinMessage(c.Request.Context(), uint(messageID), uint(subID)); err != nil {
		if errors.Is(err, domain.ErrMessageNotFound) {
			c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Failed to unpin message"})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{Success: true})
}

func (h *Handler) GetPinnedMessages(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}

	messages, err := h.chatUC.GetPinnedMessages(c.Request.Context(), uint(subID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Failed to fetch pinned messages"})
		return
	}

	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data:    messages,
		Meta:    &pagination{Page: 1, PerPage: len(messages), Total: int64(len(messages)), TotalPages: 1},
	})
}

func (h *Handler) SearchAdminChatMessages(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}
	q := c.Query("q")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "25"))
	rows, total, err := h.chatUC.SearchMessages(c.Request.Context(), uint(subID), q, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Search failed"})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{
		Success: true,
		Data:    rows,
		Meta: &pagination{
			Page:       page,
			PerPage:    limit,
			Total:      total,
			TotalPages: int(math.Ceil(float64(total) / float64(limit))),
		},
	})
}

func (h *Handler) EditAdminChatMessage(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Content is required"})
		return
	}
	adminID, _ := middleware.GetUserID(c)
	updated, err := h.chatUC.EditMessage(c.Request.Context(), uint(messageID), uint(subID), "admin", &adminID, req.Content)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrMessageNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, jsonResponse{Success: false, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true, Data: updated})
}

func (h *Handler) DeleteAdminChatMessage(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	adminID, _ := middleware.GetUserID(c)
	if err := h.chatUC.DeleteMessage(c.Request.Context(), uint(messageID), uint(subID), "admin", &adminID); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrMessageNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, jsonResponse{Success: false, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true})
}

// ─── User-side public REST ───────────────────────────────────────────────────

func (h *Handler) EditSubChatMessage(c *gin.Context) {
	uuid := c.Param("uuid")
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Subscription not found"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Content is required"})
		return
	}
	updated, err := h.chatUC.EditMessage(c.Request.Context(), uint(messageID), sub.ID, "user", nil, req.Content)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrMessageNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, jsonResponse{Success: false, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true, Data: updated})
}

func (h *Handler) DeleteSubChatMessage(c *gin.Context) {
	uuid := c.Param("uuid")
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Subscription not found"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	if err := h.chatUC.DeleteMessage(c.Request.Context(), uint(messageID), sub.ID, "user", nil); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrMessageNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, jsonResponse{Success: false, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true})
}

func (h *Handler) MarkSubChatAsRead(c *gin.Context) {
	uuid := c.Param("uuid")
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Subscription not found"})
		return
	}
	if err := h.chatUC.MarkAsRead(c.Request.Context(), sub.ID, "user"); err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Failed to mark as read"})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true})
}

// ─── Reaction REST handlers ──────────────────────────────────────────────────

func (h *Handler) ListSubMessageReactions(c *gin.Context) {
	uuid := c.Param("uuid")
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Subscription not found"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	msg, err := h.chatUC.GetMessageByID(c.Request.Context(), uint(messageID))
	if err != nil || msg.SubscriptionID != sub.ID {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Message not found"})
		return
	}
	rows, err := h.chatUC.ListReactions(c.Request.Context(), uint(messageID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Failed to fetch reactions"})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true, Data: rows})
}

func (h *Handler) AddSubMessageReaction(c *gin.Context) {
	uuid := c.Param("uuid")
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Subscription not found"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	var req struct {
		Emoji string `json:"emoji" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Emoji is required"})
		return
	}
	msg, err := h.chatUC.GetMessageByID(c.Request.Context(), uint(messageID))
	if err != nil || msg.SubscriptionID != sub.ID {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Message not found"})
		return
	}
	if err := h.chatUC.AddReaction(c.Request.Context(), uint(messageID), "user", nil, req.Emoji); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true})
}

func (h *Handler) RemoveSubMessageReaction(c *gin.Context) {
	uuid := c.Param("uuid")
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Subscription not found"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	emoji := c.Param("emoji")
	msg, err := h.chatUC.GetMessageByID(c.Request.Context(), uint(messageID))
	if err != nil || msg.SubscriptionID != sub.ID {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Message not found"})
		return
	}
	if err := h.chatUC.RemoveReaction(c.Request.Context(), uint(messageID), "user", nil, emoji); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true})
}

func (h *Handler) ListAdminMessageReactions(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	msg, err := h.chatUC.GetMessageByID(c.Request.Context(), uint(messageID))
	if err != nil || msg.SubscriptionID != uint(subID) {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Message not found"})
		return
	}
	rows, err := h.chatUC.ListReactions(c.Request.Context(), uint(messageID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, jsonResponse{Success: false, Error: "Failed to fetch reactions"})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true, Data: rows})
}

func (h *Handler) AddAdminMessageReaction(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	var req struct {
		Emoji string `json:"emoji" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Emoji is required"})
		return
	}
	msg, err := h.chatUC.GetMessageByID(c.Request.Context(), uint(messageID))
	if err != nil || msg.SubscriptionID != uint(subID) {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Message not found"})
		return
	}
	adminID, _ := middleware.GetUserID(c)
	aid := adminID
	if err := h.chatUC.AddReaction(c.Request.Context(), uint(messageID), "admin", &aid, req.Emoji); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true})
}

func (h *Handler) RemoveAdminMessageReaction(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	emoji := c.Param("emoji")
	msg, err := h.chatUC.GetMessageByID(c.Request.Context(), uint(messageID))
	if err != nil || msg.SubscriptionID != uint(subID) {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Message not found"})
		return
	}
	adminID, _ := middleware.GetUserID(c)
	aid := adminID
	if err := h.chatUC.RemoveReaction(c.Request.Context(), uint(messageID), "admin", &aid, emoji); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true})
}

// ReplaceSubMessageReaction swaps the user's existing emoji for a new one in a single atomic op.
func (h *Handler) ReplaceSubMessageReaction(c *gin.Context) {
	uuid := c.Param("uuid")
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Subscription not found"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	oldEmoji := c.Param("emoji")
	var req struct {
		NewEmoji string `json:"new_emoji" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "new_emoji is required"})
		return
	}
	msg, err := h.chatUC.GetMessageByID(c.Request.Context(), uint(messageID))
	if err != nil || msg.SubscriptionID != sub.ID {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Message not found"})
		return
	}
	if err := h.chatUC.ReplaceReaction(c.Request.Context(), uint(messageID), "user", nil, oldEmoji, req.NewEmoji); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrMessageNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, jsonResponse{Success: false, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true})
}

func (h *Handler) ReplaceAdminMessageReaction(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid subscription ID"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "Invalid message ID"})
		return
	}
	oldEmoji := c.Param("emoji")
	var req struct {
		NewEmoji string `json:"new_emoji" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonResponse{Success: false, Error: "new_emoji is required"})
		return
	}
	msg, err := h.chatUC.GetMessageByID(c.Request.Context(), uint(messageID))
	if err != nil || msg.SubscriptionID != uint(subID) {
		c.JSON(http.StatusNotFound, jsonResponse{Success: false, Error: "Message not found"})
		return
	}
	adminID, _ := middleware.GetUserID(c)
	aid := adminID
	if err := h.chatUC.ReplaceReaction(c.Request.Context(), uint(messageID), "admin", &aid, oldEmoji, req.NewEmoji); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrMessageNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, jsonResponse{Success: false, Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, jsonResponse{Success: true})
}
