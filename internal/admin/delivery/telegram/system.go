package telegram

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"gopkg.in/telebot.v3"
)

// HandleSystem shows system health and resource information
func (h *Handler) HandleSystem(c telebot.Context) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	// --- DB Connectivity ---
	dbStatus := "Connected"
	sqlDB, err := h.db.DB()
	if err != nil {
		dbStatus = "Error: database connection issue"
	} else {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := sqlDB.PingContext(pingCtx); err != nil {
			dbStatus = "Unreachable: database ping failed"
		}
	}

	// --- Node Counts ---
	totalNodes := 0
	onlineNodes := 0
	nodes, err := h.nodeUC.ListNodes(ctx)
	if err == nil {
		totalNodes = len(nodes)
		for _, n := range nodes {
			if n.IsOnline {
				onlineNodes++
			}
		}
	}

	// --- Provisioning Queue ---
	queueDepth := int64(0)
	if h.provRepo != nil {
		queueDepth, _ = h.provRepo.CountPending(ctx)
	}

	// --- Memory Usage ---
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	allocMB := float64(mem.Alloc) / 1024 / 1024
	sysMB := float64(mem.Sys) / 1024 / 1024
	numGC := mem.NumGC
	goroutines := runtime.NumGoroutine()

	msg := fmt.Sprintf(`🖥 *System Status*

📡 *Database:* %s

🌐 *Nodes:* %d total, %d online

📦 *Provisioning Queue:* %d pending

💾 *Memory*
• Alloc: %.1f MB
• Sys: %.1f MB
• GC Runs: %d
• Goroutines: %d

🔧 *Runtime*
• Go: %s
• OS/Arch: %s/%s
• CPUs: %d`,
		dbStatus,
		totalNodes, onlineNodes,
		queueDepth,
		allocMB, sysMB, numGC, goroutines,
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(),
	)

	return c.Send(msg, telebot.ModeMarkdown)
}
