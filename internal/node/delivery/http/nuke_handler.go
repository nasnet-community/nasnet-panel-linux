package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/audit"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

type wipeRequestBody struct {
	DryRun              bool `json:"dry_run"`
	AlsoRemoveHubRecord bool `json:"also_remove_hub_record"`
}

// Wipe handles POST /nodes/:id/wipe (unary).
func (h *Handler) Wipe(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}
	var body wipeRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	ac := audit.FromGinContext(c)
	opts := usecase.NukeOptions{
		Mode:          pb.NukeMode_NUKE_MODE_WIPE,
		DryRun:        body.DryRun,
		KeepHubRecord: !body.AlsoRemoveHubRecord,
		ActorID:       ac.ActorID,
		ActorName:     ac.ActorName,
		IPAddress:     ac.IPAddress,
	}

	report, err := h.nodeUsecase.Nuke(c.Request.Context(), uint(nodeID), opts, nil)
	if err != nil {
		if errors.Is(err, usecase.ErrNukeInFlight) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "error": err.Error()})
			return
		}
		if errors.Is(err, usecase.ErrNukeFailed) {
			// Agent ran but everything failed — return 200 with the report so the
			// UI can show what happened. The hub record was NOT mutated.
			c.JSON(http.StatusOK, gin.H{"success": false, "report": report, "error": err.Error()})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "report": report})
}

type nukeRequestBody struct {
	DryRun        bool `json:"dry_run"`
	ShredRoot     bool `json:"shred_root"`
	KeepHubRecord bool `json:"keep_hub_record"`
}

// Nuke handles POST /nodes/:id/nuke (server-sent events).
func (h *Handler) Nuke(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node id"})
		return
	}
	var body nukeRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	ac := audit.FromGinContext(c)
	opts := usecase.NukeOptions{
		Mode:          pb.NukeMode_NUKE_MODE_NUKE,
		ShredRoot:     body.ShredRoot,
		DryRun:        body.DryRun,
		KeepHubRecord: body.KeepHubRecord,
		ActorID:       ac.ActorID,
		ActorName:     ac.ActorName,
		IPAddress:     ac.IPAddress,
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	sendEvent := func(eventName string, payload interface{}) {
		data, _ := json.Marshal(payload)
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", eventName, data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	emit := func(r *pb.NukePhaseResult) { sendEvent("phase", r) }

	report, err := h.nodeUsecase.Nuke(c.Request.Context(), uint(nodeID), opts, emit)
	if err != nil {
		if errors.Is(err, usecase.ErrNukeInFlight) {
			sendEvent("error", gin.H{"error": err.Error(), "code": "in_flight"})
			return
		}
		if errors.Is(err, usecase.ErrNukeFailed) {
			// Agent ran, all phases failed. Emit the report as `done` so the UI
			// renders it, then return.
			sendEvent("done", report)
			return
		}
		sendEvent("error", gin.H{"error": err.Error()})
		return
	}
	sendEvent("done", report)
}
