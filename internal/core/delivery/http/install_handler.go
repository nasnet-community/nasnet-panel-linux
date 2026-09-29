package http

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
)

// splitCSV splits a comma-separated query value into trimmed, non-empty items.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ProbeDeps reports, per requested core (or all), which dependencies are present
// on the node. GET /cores/nodes/:nodeID/deps?cores=mtproto
func (h *Handler) ProbeDeps(c *gin.Context) {
	nodeID, ok := atoiParam(c, "nodeID")
	if !ok {
		return
	}
	var cores []string
	if raw := c.Query("cores"); raw != "" {
		cores = splitCSV(raw)
	}
	reports, err := h.install.Probe(c.Request.Context(), nodeID, cores)
	if err != nil {
		httputil.Error(c, http.StatusBadGateway, err.Error())
		return
	}
	httputil.OK(c, gin.H{"reports": toReportJSON(reports)})
}

type startDepInstallBody struct {
	Cores     []string `json:"cores"`
	Uninstall bool     `json:"uninstall"`
}

// StartDepInstall launches an async install job on the node.
// POST /cores/nodes/:nodeID/deps/install
func (h *Handler) StartDepInstall(c *gin.Context) {
	nodeID, ok := atoiParam(c, "nodeID")
	if !ok {
		return
	}
	var body startDepInstallBody
	if err := c.ShouldBindJSON(&body); err != nil {
		httputil.Error(c, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	jobID, err := h.install.StartInstall(c.Request.Context(), nodeID, body.Cores, body.Uninstall)
	if err != nil {
		httputil.Error(c, http.StatusBadGateway, err.Error())
		return
	}
	httputil.OK(c, gin.H{"job_id": jobID})
}

// GetDepInstall returns a one-shot progress snapshot (non-streaming fallback).
// GET /cores/nodes/:nodeID/deps/install/:jobID
func (h *Handler) GetDepInstall(c *gin.Context) {
	if _, ok := atoiParam(c, "nodeID"); !ok {
		return
	}
	resp, err := h.install.Progress(c.Request.Context(), c.Param("jobID"), 0)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, err.Error())
		return
	}
	httputil.OK(c, gin.H{
		"status":    resp.GetStatus(),
		"steps":     toStepsJSON(resp.GetSteps()),
		"log_lines": resp.GetLogLines(),
		"done":      resp.GetDone(),
	})
}

// StreamDepInstall streams install progress as SSE by polling the node's job
// until done. Mirrors the node log / agent-update stream pattern.
// GET /cores/nodes/:nodeID/deps/install/:jobID/stream
func (h *Handler) StreamDepInstall(c *gin.Context) {
	if _, ok := atoiParam(c, "nodeID"); !ok {
		return
	}
	jobID := c.Param("jobID")

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	ctx := c.Request.Context()
	cursor := int32(0)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	// Emit an initial frame immediately, then poll.
	emit := func() (done bool) {
		resp, err := h.install.Progress(ctx, jobID, cursor)
		if err != nil {
			c.SSEvent("error", err.Error())
			c.Writer.Flush()
			return true
		}
		cursor = resp.GetNextCursor()
		payload, _ := json.Marshal(gin.H{
			"status":    resp.GetStatus(),
			"steps":     toStepsJSON(resp.GetSteps()),
			"log_lines": resp.GetLogLines(),
			"done":      resp.GetDone(),
		})
		fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", payload)
		c.Writer.Flush()
		return resp.GetDone()
	}

	if emit() {
		return
	}
	c.Stream(func(w io.Writer) bool {
		select {
		case <-ctx.Done():
			return false
		case <-heartbeat.C:
			fmt.Fprint(w, ": keepalive\n\n")
			c.Writer.Flush()
			return true
		case <-ticker.C:
			return !emit()
		}
	})
}

// ---- JSON shaping helpers (proto → panel-friendly) ----

func toReportJSON(reports []*pb.CoreDependencyReport) []gin.H {
	out := make([]gin.H, 0, len(reports))
	for _, r := range reports {
		deps := make([]gin.H, 0, len(r.GetDeps()))
		for _, d := range r.GetDeps() {
			deps = append(deps, gin.H{
				"name": d.GetName(), "kind": d.GetKind(), "present": d.GetPresent(),
				"version": d.GetVersion(), "path": d.GetPath(), "optional": d.GetOptional(),
			})
		}
		out = append(out, gin.H{"core_type": r.GetCoreType(), "ready": r.GetReady(), "deps": deps})
	}
	return out
}

func toStepsJSON(steps []*pb.InstallStep) []gin.H {
	out := make([]gin.H, 0, len(steps))
	for _, s := range steps {
		out = append(out, gin.H{
			"name": s.GetName(), "core_type": s.GetCoreType(), "kind": s.GetKind(),
			"state": s.GetState(), "detail": s.GetDetail(),
		})
	}
	return out
}
