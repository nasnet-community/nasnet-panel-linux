package stats

import (
	"context"
	"strings"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

// HostInfo holds static system information
type HostInfo struct {
	Hostname             string `json:"hostname"`
	OS                   string `json:"os"`
	Platform             string `json:"platform"`
	PlatformFamily       string `json:"platform_family"`
	PlatformVersion      string `json:"platform_version"`
	KernelVersion        string `json:"kernel_version"`
	Arch                 string `json:"arch"`
	VirtualizationSystem string `json:"virtualization_system"`
	VirtualizationRole   string `json:"virtualization_role"`
	CPUModelName         string `json:"cpu_model_name"`
	CPUCores             int32  `json:"cpu_cores"`
	TotalMemory          uint64 `json:"total_memory"`
	TotalSwap            uint64 `json:"total_swap"`
	BootTime             uint64 `json:"boot_time"`
}

// GetHostInfo gathers host information
func GetHostInfo(ctx context.Context) (*HostInfo, error) {
	info := &HostInfo{}

	// Host Info
	h, err := host.InfoWithContext(ctx)
	if err == nil {
		info.Hostname = h.Hostname
		info.OS = h.OS
		info.Platform = h.Platform
		info.PlatformFamily = h.PlatformFamily
		info.PlatformVersion = h.PlatformVersion
		info.KernelVersion = h.KernelVersion
		info.Arch = h.KernelArch
		info.VirtualizationSystem = h.VirtualizationSystem
		info.VirtualizationRole = h.VirtualizationRole
		info.BootTime = h.BootTime
	}

	// CPU Info
	c, err := cpu.InfoWithContext(ctx)
	if err == nil && len(c) > 0 {
		// Use the first CPU for model name
		info.CPUModelName = c[0].ModelName
		// Count logic cores
		counts, _ := cpu.CountsWithContext(ctx, true)
		info.CPUCores = int32(counts)
	}

	// Memory Info
	m, err := mem.VirtualMemoryWithContext(ctx)
	if err == nil {
		info.TotalMemory = m.Total
	}

	// Swap Info
	s, err := mem.SwapMemoryWithContext(ctx)
	if err == nil {
		info.TotalSwap = s.Total
	}

	// Clean up CPU model name (remove extra spaces)
	info.CPUModelName = strings.Join(strings.Fields(info.CPUModelName), " ")

	return info, nil
}
