package stats

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// NICCounters holds one interface's cumulative byte counters.
type NICCounters struct {
	BytesRecv uint64 `json:"bytes_recv"`
	BytesSent uint64 `json:"bytes_sent"`
}

// sumNICs totals every interface except loopback. gopsutil's pernic=false
// aggregate includes lo, which double-counts every locally-terminated byte.
func sumNICs(per map[string]NICCounters) (recv, sent uint64) {
	for name, c := range per {
		if name == "lo" {
			continue
		}
		recv += c.BytesRecv
		sent += c.BytesSent
	}
	return recv, sent
}

// collectPerNIC reads per-interface counters.
func collectPerNIC(ctx context.Context) (map[string]NICCounters, error) {
	netIO, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make(map[string]NICCounters, len(netIO))
	for _, c := range netIO {
		out[c.Name] = NICCounters{BytesRecv: c.BytesRecv, BytesSent: c.BytesSent}
	}
	return out, nil
}

// SystemStats holds system resource statistics
type SystemStats struct {
	// CPU
	CPUUsagePercent float64   `json:"cpu_usage_percent"`
	CPUPerCore      []float64 `json:"cpu_per_core"`

	// Memory
	MemoryTotalBytes     uint64  `json:"memory_total_bytes"`
	MemoryUsedBytes      uint64  `json:"memory_used_bytes"`
	MemoryAvailableBytes uint64  `json:"memory_available_bytes"`
	MemoryUsagePercent   float64 `json:"memory_usage_percent"`

	// Swap
	SwapTotalBytes uint64 `json:"swap_total_bytes"`
	SwapUsedBytes  uint64 `json:"swap_used_bytes"`

	// Disk
	DiskTotalBytes   uint64  `json:"disk_total_bytes"`
	DiskUsedBytes    uint64  `json:"disk_used_bytes"`
	DiskFreeBytes    uint64  `json:"disk_free_bytes"`
	DiskUsagePercent float64 `json:"disk_usage_percent"`

	// Network I/O (cumulative since system boot)
	NetworkRecvBytes uint64 `json:"network_recv_bytes"`
	NetworkSentBytes uint64 `json:"network_sent_bytes"`

	// Rate (bytes per second)
	NetworkRecvRate uint64 `json:"network_recv_rate"`
	NetworkSentRate uint64 `json:"network_sent_rate"`

	// Per interface cumulative counters
	PerNIC map[string]NICCounters `json:"per_nic"`

	// Load averages
	LoadAvg1  float64 `json:"load_avg_1"`
	LoadAvg5  float64 `json:"load_avg_5"`
	LoadAvg15 float64 `json:"load_avg_15"`

	// System uptime
	SystemUptimeSeconds int64 `json:"system_uptime_seconds"`

	// Network details
	TcpCount uint64 `json:"tcp_count"`
	UdpCount uint64 `json:"udp_count"`
	FdCount  uint64 `json:"fd_count"`
}

// Collector periodically gathers system statistics
type Collector struct {
	diskPath string

	// Cache for network delta calculation
	lastNetRecv uint64
	lastNetSent uint64
	lastNetTime time.Time

	mu sync.RWMutex
}

// NewCollector creates a new system stats collector
func NewCollector(diskPath string) *Collector {
	if diskPath == "" {
		diskPath = "/"
	}
	return &Collector{
		diskPath: diskPath,
	}
}

// Collect gathers all system statistics
func (c *Collector) Collect(ctx context.Context) (*SystemStats, error) {
	stats := &SystemStats{}

	// CPU usage (1 second sample)
	cpuPercent, err := cpu.PercentWithContext(ctx, time.Second, false)
	if err == nil && len(cpuPercent) > 0 {
		stats.CPUUsagePercent = cpuPercent[0]
	}

	// Per-core CPU
	cpuPerCore, err := cpu.PercentWithContext(ctx, 0, true)
	if err == nil {
		stats.CPUPerCore = cpuPerCore
	}

	// Memory
	vmem, err := mem.VirtualMemoryWithContext(ctx)
	if err == nil {
		stats.MemoryTotalBytes = vmem.Total
		stats.MemoryUsedBytes = vmem.Used
		stats.MemoryAvailableBytes = vmem.Available
		stats.MemoryUsagePercent = vmem.UsedPercent
	}

	// Swap
	swap, err := mem.SwapMemoryWithContext(ctx)
	if err == nil {
		stats.SwapTotalBytes = swap.Total
		stats.SwapUsedBytes = swap.Used
	}

	// Disk
	diskUsage, err := disk.UsageWithContext(ctx, c.diskPath)
	if err == nil {
		stats.DiskTotalBytes = diskUsage.Total
		stats.DiskUsedBytes = diskUsage.Used
		stats.DiskFreeBytes = diskUsage.Free
		stats.DiskUsagePercent = diskUsage.UsedPercent
	}

	// per NIC so router mode can attribute traffic to an uplink (Loopback is excluded)
	if per, err := collectPerNIC(ctx); err == nil {
		stats.PerNIC = per
		stats.NetworkRecvBytes, stats.NetworkSentBytes = sumNICs(per)

		recvDelta, sentDelta, duration, derr := c.GetNetworkDelta(ctx)
		if derr == nil && duration > 0 {
			stats.NetworkRecvRate = uint64(float64(recvDelta) / duration.Seconds())
			stats.NetworkSentRate = uint64(float64(sentDelta) / duration.Seconds())
		}
	}

	// Load averages
	loadAvg, err := load.AvgWithContext(ctx)
	if err == nil {
		stats.LoadAvg1 = loadAvg.Load1
		stats.LoadAvg5 = loadAvg.Load5
		stats.LoadAvg15 = loadAvg.Load15
	}

	// System uptime
	uptime, err := host.UptimeWithContext(ctx)
	if err == nil {
		stats.SystemUptimeSeconds = int64(uptime)
	}

	// Network Connections
	tcpConns, err := net.ConnectionsWithContext(ctx, "tcp")
	if err == nil {
		stats.TcpCount = uint64(len(tcpConns))
	}
	udpConns, err := net.ConnectionsWithContext(ctx, "udp")
	if err == nil {
		stats.UdpCount = uint64(len(udpConns))
	}

	// File Descriptors
	stats.FdCount = getFDCount()

	return stats, nil
}

// CollectQuick gathers stats without the 1-second CPU sample delay
func (c *Collector) CollectQuick(ctx context.Context) (*SystemStats, error) {
	stats := &SystemStats{}

	// CPU usage (instant, may be less accurate)
	cpuPercent, err := cpu.PercentWithContext(ctx, 0, false)
	if err == nil && len(cpuPercent) > 0 {
		stats.CPUUsagePercent = cpuPercent[0]
	}

	// Per-core CPU
	cpuPerCore, err := cpu.PercentWithContext(ctx, 0, true)
	if err == nil {
		stats.CPUPerCore = cpuPerCore
	}

	// Memory
	vmem, err := mem.VirtualMemoryWithContext(ctx)
	if err == nil {
		stats.MemoryTotalBytes = vmem.Total
		stats.MemoryUsedBytes = vmem.Used
		stats.MemoryAvailableBytes = vmem.Available
		stats.MemoryUsagePercent = vmem.UsedPercent
	}

	// Swap
	swap, err := mem.SwapMemoryWithContext(ctx)
	if err == nil {
		stats.SwapTotalBytes = swap.Total
		stats.SwapUsedBytes = swap.Used
	}

	// Disk
	diskUsage, err := disk.UsageWithContext(ctx, c.diskPath)
	if err == nil {
		stats.DiskTotalBytes = diskUsage.Total
		stats.DiskUsedBytes = diskUsage.Used
		stats.DiskFreeBytes = diskUsage.Free
		stats.DiskUsagePercent = diskUsage.UsedPercent
	}

	if per, err := collectPerNIC(ctx); err == nil {
		stats.PerNIC = per
		stats.NetworkRecvBytes, stats.NetworkSentBytes = sumNICs(per)
	}

	// Load averages
	loadAvg, err := load.AvgWithContext(ctx)
	if err == nil {
		stats.LoadAvg1 = loadAvg.Load1
		stats.LoadAvg5 = loadAvg.Load5
		stats.LoadAvg15 = loadAvg.Load15
	}

	// System uptime
	uptime, err := host.UptimeWithContext(ctx)
	if err == nil {
		stats.SystemUptimeSeconds = int64(uptime)
	}

	return stats, nil
}

// GetNetworkDelta returns network bytes transferred since last call
func (c *Collector) GetNetworkDelta(ctx context.Context) (recv, sent uint64, duration time.Duration, err error) {
	per, err := collectPerNIC(ctx)
	if err != nil || len(per) == 0 {
		return 0, 0, 0, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	currentRecv, currentSent := sumNICs(per)

	if c.lastNetTime.IsZero() {
		// First call, just store values
		c.lastNetRecv = currentRecv
		c.lastNetSent = currentSent
		c.lastNetTime = now
		return 0, 0, 0, nil
	}

	// Calculate delta
	recv = computeNetDelta(currentRecv, c.lastNetRecv)
	sent = computeNetDelta(currentSent, c.lastNetSent)
	duration = now.Sub(c.lastNetTime)

	// Update last values
	c.lastNetRecv = currentRecv
	c.lastNetSent = currentSent
	c.lastNetTime = now

	return recv, sent, duration, nil
}

// getFDCount tries to read system-wide file descriptor usage
func getFDCount() uint64 {
	// Linux: /proc/sys/fs/file-nr
	// Content: <allocated> <free> <max>
	content, err := os.ReadFile("/proc/sys/fs/file-nr")
	if err != nil {
		return 0
	}

	parts := strings.Fields(string(content))
	if len(parts) < 1 {
		return 0
	}

	count, _ := strconv.ParseUint(parts[0], 10, 64)
	// /proc/sys/fs/file-nr: [allocated, allocated-but-unused, max].
	// used = allocated - free (free is typically 0 on 2.6+).

	if len(parts) >= 2 {
		free, _ := strconv.ParseUint(parts[1], 10, 64)
		if count > free {
			return count - free
		}
	}

	return count
}

// computeNetDelta returns current - last, treating any decrease as a
// counter reset (NIC replug, interface reset, or rare 64-bit wrap) by
// returning the current value directly rather than an enormous
// underflowed delta.
func computeNetDelta(current, last uint64) uint64 {
	if current < last {
		return current
	}
	return current - last
}
