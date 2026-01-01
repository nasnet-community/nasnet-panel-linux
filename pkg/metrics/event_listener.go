package metrics

import (
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// StartEventListener subscribes to the EventBus and updates per-node
// Prometheus gauges in real time from EventNodeStatsUpdated events.
func StartEventListener(bus *events.EventBus) {
	ch := bus.Subscribe("prometheus-metrics")

	go func() {
		log := logger.GetLogger()
		log.Info("Metrics: EventBus listener started")

		for event := range ch {
			if event.Type != events.EventNodeStatsUpdated {
				continue
			}

			// Keep consuming events to prevent channel backup, but skip gauge updates when disabled
			if !Enabled.Load() {
				continue
			}

			payload, ok := event.Payload.(events.NodeStatsPayload)
			if !ok {
				continue
			}

			nodeID := fmt.Sprintf("%d", payload.NodeID)
			nodeName := payload.NodeName

			NodeCPUPercent.WithLabelValues(nodeID, nodeName).Set(payload.CPUPercent)
			NodeMemoryPercent.WithLabelValues(nodeID, nodeName).Set(payload.MemoryPercent)
			NodeDiskPercent.WithLabelValues(nodeID, nodeName).Set(payload.DiskPercent)
			NodeTCPConnections.WithLabelValues(nodeID, nodeName).Set(float64(payload.TcpCount))
			NodeUDPConnections.WithLabelValues(nodeID, nodeName).Set(float64(payload.UdpCount))
			NodeTrafficBytes.WithLabelValues(nodeID, nodeName, "up").Set(float64(payload.TotalUplink))
			NodeTrafficBytes.WithLabelValues(nodeID, nodeName, "down").Set(float64(payload.TotalDownlink))
			NodeOnlineUsers.WithLabelValues(nodeID, nodeName).Set(float64(payload.OnlineUsers))
			NodeXrayUptimeSeconds.WithLabelValues(nodeID, nodeName).Set(float64(payload.Uptime))
		}

		log.Info("Metrics: EventBus listener stopped")
	}()
}
