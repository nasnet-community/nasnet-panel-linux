package agent

import (
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// coreUsageEntries maps the proto per-(protocol,email) core-usage rows into the
// client struct form. Nil when the agent sent no core usage.
func coreUsageEntries(recs []*pb.CoreUsageRecord) []CoreUsageEntry {
	if len(recs) == 0 {
		return nil
	}
	out := make([]CoreUsageEntry, 0, len(recs))
	for _, r := range recs {
		if r == nil {
			continue
		}
		out = append(out, CoreUsageEntry{
			Protocol: r.Protocol,
			Email:    r.Email,
			Uplink:   r.Uplink,
			Downlink: r.Downlink,
		})
	}
	return out
}

// systemStatsFromPB maps the wire SystemStats to the package struct.
// Shared by the direct and reverse clients and by the bundle path.
func systemStatsFromPB(resp *pb.SystemStats) *SystemStats {
	if resp == nil {
		return nil
	}
	return &SystemStats{
		CollectedAtUnixMs:   resp.CollectedAtUnixMs,
		CPUUsagePercent:     resp.CpuUsagePercent,
		MemoryTotalBytes:    resp.MemoryTotalBytes,
		MemoryUsedBytes:     resp.MemoryUsedBytes,
		MemoryUsagePercent:  resp.MemoryUsagePercent,
		DiskTotalBytes:      resp.DiskTotalBytes,
		DiskUsedBytes:       resp.DiskUsedBytes,
		DiskUsagePercent:    resp.DiskUsagePercent,
		NetworkRecvRate:     resp.NetworkRecvRate,
		NetworkSentRate:     resp.NetworkSentRate,
		LoadAvg1:            resp.LoadAvg_1,
		LoadAvg5:            resp.LoadAvg_5,
		LoadAvg15:           resp.LoadAvg_15,
		SystemUptimeSeconds: resp.SystemUptimeSeconds,
		TcpCount:            resp.TcpConns,
		UdpCount:            resp.UdpConns,
		FdCount:             resp.FdCount,
	}
}

// bufferedTrafficFromPB maps the wire BufferedTrafficStats (CoreUsage
// included) to the package struct. Shared by both clients and the bundle.
func bufferedTrafficFromPB(resp *pb.BufferedTrafficStats) *BufferedTrafficStats {
	if resp == nil {
		return nil
	}
	records := make([]*TrafficRecord, 0, len(resp.Records))
	for _, r := range resp.Records {
		records = append(records, &TrafficRecord{
			Timestamp:        r.Timestamp,
			UserUplink:       r.UserUplink,
			UserDownlink:     r.UserDownlink,
			OutboundUplink:   r.OutboundUplink,
			OutboundDownlink: r.OutboundDownlink,
			InboundUplink:    r.InboundUplink,
			InboundDownlink:  r.InboundDownlink,
			TotalUplink:      r.TotalUplink,
			TotalDownlink:    r.TotalDownlink,
			CoreUsage:        coreUsageEntries(r.CoreUsage),
		})
	}
	return &BufferedTrafficStats{
		Records:         records,
		BufferStartTime: resp.BufferStartTime,
		BufferEndTime:   resp.BufferEndTime,
	}
}

// onlineIPsFromPB maps the wire AllOnlineIPsResponse to the plain
// email → IP → first-seen map. Inner maps are non-nil even when empty.
func onlineIPsFromPB(resp *pb.AllOnlineIPsResponse) map[string]map[string]int64 {
	if resp == nil {
		return nil
	}
	out := make(map[string]map[string]int64, len(resp.Users))
	for email, entry := range resp.Users {
		if entry == nil || entry.Ips == nil {
			out[email] = map[string]int64{}
			continue
		}
		out[email] = entry.Ips
	}
	return out
}
