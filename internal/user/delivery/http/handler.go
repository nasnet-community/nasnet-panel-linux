package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/user/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
	httpMiddleware "github.com/nasnet-community/nasnet-panel-linux/transport/http/middleware"
)

type Handler struct {
	userUsecase usecase.UserUsecase
}

func NewHandler(userUsecase usecase.UserUsecase) *Handler {
	return &Handler{userUsecase: userUsecase}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	users := rg.Group("/users")
	{
		// List and search
		users.GET("", h.List)

		// Single user operations
		users.GET("/:id", h.GetByID)
		users.GET("/telegram/:telegram_id", h.GetByTelegramID)

		// Registration
		users.POST("", h.Register)
		users.POST("/get-or-create", h.GetOrCreate)
	}
}

func (h *Handler) List(c *gin.Context) {
	pg := httputil.ParsePage(c, 20)

	users, err := h.userUsecase.List(c.Request.Context(), pg.Offset, pg.PerPage)
	if err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusInternalServerError, err, "")
		return
	}

	httputil.Paged(c, users, &httputil.Meta{
		Page:    pg.Page,
		PerPage: pg.PerPage,
	})
}

func (h *Handler) GetByID(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	user, err := h.userUsecase.GetByID(c.Request.Context(), id)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, err.Error())
		return
	}

	httputil.OK(c, user)
}

func (h *Handler) GetByTelegramID(c *gin.Context) {
	telegramID, err := strconv.ParseInt(c.Param("telegram_id"), 10, 64)
	if err != nil {
		httputil.Error(c, http.StatusBadRequest, "invalid telegram_id")
		return
	}

	user, err := h.userUsecase.GetByTelegramID(c.Request.Context(), telegramID)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, err.Error())
		return
	}

	httputil.OK(c, user)
}

type registerRequest struct {
	TelegramID int64  `json:"telegram_id" binding:"required"`
	Username   string `json:"username"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
}

// Register creates a new user
func (h *Handler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	user, err := h.userUsecase.Register(c.Request.Context(), req.TelegramID, req.Username, req.FirstName, req.LastName)
	if err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	httputil.Created(c, user)
}

// GetOrCreate retrieves existing user or creates a new one
func (h *Handler) GetOrCreate(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	user, err := h.userUsecase.GetOrCreate(c.Request.Context(), req.TelegramID, req.Username, req.FirstName, req.LastName)
	if err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	httputil.OK(c, user)
}
