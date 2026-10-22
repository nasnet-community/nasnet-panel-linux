// Package http serves the Settings → Updates API.
package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/nasnet-community/nasnet-panel-linux/internal/audit"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/update/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/update/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
)

type Handler struct {
	uc    *usecase.Usecase
	audit auditDomain.AuditLogUsecase
}

func NewHandler(uc *usecase.Usecase, audit auditDomain.AuditLogUsecase) *Handler {
	return &Handler{uc: uc, audit: audit}
}

// RegisterRoutes goes on the admin group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/updates")
	g.GET("", h.Status)
	g.POST("/check", h.Check)
	g.POST("/apply", h.Apply)
	g.DELETE("/schedule", h.CancelSchedule)
	g.POST("/upload", h.Upload)
	g.POST("/rollback", h.Rollback)
	g.GET("/jobs/:id/stream", h.StreamJob)
}

// RegisterHealth goes on the engine root, next to /health.
func (h *Handler) RegisterHealth(r gin.IRoutes) {
	r.GET("/health/update", h.Health)
}

// GET /updates
func (h *Handler) Status(c *gin.Context) {
	st, err := h.uc.Status(c.Request.Context())
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.OK(c, st)
}

// POST /updates/check
func (h *Handler) Check(c *gin.Context) {
	res, err := h.uc.CheckNow(c.Request.Context())
	if errors.Is(err, usecase.ErrNoVersion) {
		httputil.Error(c, http.StatusConflict, "This build has no release version to compare.")
		return
	}
	if err != nil {
		httputil.Error(c, http.StatusConflict, err.Error())
		return
	}
	httputil.OK(c, res)
}

// POST /updates/apply {version} or {upload_id}, with at to schedule it
func (h *Handler) Apply(c *gin.Context) {
	var req struct {
		usecase.Request
		At *time.Time `json:"at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, "Send a version or an upload_id.")
		return
	}
	if req.At != nil {
		s, err := h.uc.ScheduleUpdate(c.Request.Context(), req.Request, *req.At)
		if err != nil {
			httputil.Error(c, http.StatusConflict, usecase.Sentence(err.Error()))
			return
		}
		h.log(c, auditDomain.AuditPanelUpdateSchedule, map[string]string{"to": s.Version, "at": s.At.Format(time.RFC3339)})
		httputil.OK(c, gin.H{"schedule": s})
		return
	}
	job, err := h.uc.Apply(c.Request.Context(), req.Request)
	if err != nil {
		httputil.Error(c, http.StatusConflict, usecase.Sentence(err.Error()))
		return
	}
	h.log(c, auditDomain.AuditPanelUpdate, map[string]string{"from": job.From, "to": job.To, "source": job.Source})
	httputil.JSON(c, http.StatusAccepted, gin.H{"job_id": job.ID})
}

// DELETE /updates/schedule
func (h *Handler) CancelSchedule(c *gin.Context) {
	s, err := h.uc.CancelSchedule(c.Request.Context())
	if err != nil {
		httputil.Error(c, http.StatusConflict, usecase.Sentence(err.Error()))
		return
	}
	h.log(c, auditDomain.AuditPanelUpdateCancel, map[string]string{"to": s.Version, "at": s.At.Format(time.RFC3339)})
	httputil.OK(c, gin.H{"schedule": s})
}

// POST /updates/upload, multipart "file": an offline bundle, streamed to
// disk. It answers with what its signed bundle.json says.
func (h *Handler) Upload(c *gin.Context) {
	// a big bundle over a slow link outlasts the server's 5 minute read timeout
	rc := http.NewResponseController(c.Writer)
	_ = rc.SetReadDeadline(time.Now().Add(time.Hour))
	_ = rc.SetWriteDeadline(time.Now().Add(time.Hour))
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, usecase.MaxUpload+1<<20)

	mr, err := c.Request.MultipartReader()
	if err != nil {
		httputil.Error(c, http.StatusBadRequest, "Send the bundle as a file.")
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			httputil.Error(c, http.StatusBadRequest, "Send the bundle in a field named file.")
			return
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		up, err := h.uc.SaveUpload(part)
		part.Close()
		var tooBig *http.MaxBytesError
		switch {
		case errors.As(err, &tooBig):
			httputil.Error(c, http.StatusRequestEntityTooLarge, "The file is over 300 MB.")
			return
		case err != nil:
			httputil.Error(c, http.StatusUnprocessableEntity, usecase.Sentence(err.Error()))
			return
		}
		h.log(c, auditDomain.AuditPanelUpdateUpload, map[string]string{"version": up.Version, "upload_id": up.ID})
		httputil.OK(c, up)
		return
	}
}

// POST /updates/rollback
func (h *Handler) Rollback(c *gin.Context) {
	job, err := h.uc.Rollback(c.Request.Context())
	if err != nil {
		httputil.Error(c, http.StatusConflict, usecase.Sentence(err.Error()))
		return
	}
	h.log(c, auditDomain.AuditPanelRollback, map[string]string{"from": job.From, "to": job.To})
	httputil.JSON(c, http.StatusAccepted, gin.H{"job_id": job.ID})
}

// GET /updates/jobs/:id/stream: server-sent snapshots until the job ends.
// The stream breaks when the panel restarts; the page then polls
// /settings/status until the new version answers.
func (h *Handler) StreamJob(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.uc.Job(id); !ok {
		httputil.Error(c, http.StatusNotFound, "no such job")
		return
	}
	hd := c.Writer.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-cache")
	hd.Set("Connection", "keep-alive")
	hd.Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	ctx := c.Request.Context()
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	keep := time.NewTicker(15 * time.Second)
	defer keep.Stop()
	var last []byte
	emit := func() (done bool) {
		j, ok := h.uc.Job(id)
		if !ok {
			return true
		}
		b, _ := json.Marshal(j)
		if string(b) != string(last) {
			last = b
			fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", b)
			c.Writer.Flush()
		}
		return j.Phase == domain.PhaseDone
	}
	if emit() {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-keep.C:
			fmt.Fprint(c.Writer, ": keepalive\n\n")
			c.Writer.Flush()
		case <-tick.C:
			if emit() {
				return
			}
		}
	}
}

// GET /health/update, loopback only: the update runner's trial boot.
func (h *Handler) Health(c *gin.Context) {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, h.uc.Health(c.Request.Context()))
}

func (h *Handler) log(c *gin.Context, action auditDomain.AuditAction, values map[string]string) {
	if h.audit == nil {
		return
	}
	ac := audit.FromGinContext(c)
	b, _ := json.Marshal(values)
	h.audit.Log(c.Request.Context(), &auditDomain.AuditLog{
		Action: string(action), ActorID: ac.ActorID, ActorName: ac.ActorName,
		EntityType: "panel_update", NewValues: string(b),
		IPAddress: ac.IPAddress, RequestID: ac.RequestID, Source: "http",
	})
}
