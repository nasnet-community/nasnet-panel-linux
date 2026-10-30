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

// SystemStatsFromPB maps the wire SystemStats to the package struct.
// Shared by the direct, reverse and embedded clients and by the bundle path.
func SystemStatsFromPB(resp *pb.SystemStats) *SystemStats {
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

// BufferedTrafficFromPB maps the wire BufferedTrafficStats (CoreUsage
// included) to the package struct. Shared by every client and the bundle.
func BufferedTrafficFromPB(resp *pb.BufferedTrafficStats) *BufferedTrafficStats {
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

// OnlineIPsFromPB maps the wire AllOnlineIPsResponse to the plain
// email → IP → first-seen map. Inner maps are non-nil even when empty.
func OnlineIPsFromPB(resp *pb.AllOnlineIPsResponse) map[string]map[string]int64 {
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

// OutboundTestRequestToPB maps a spec onto the wire message. TimeoutSeconds is
// kept populated so an agent predating the option fields still gets a sane
// timeout.
func OutboundTestRequestToPB(spec *OutboundTestSpec) *pb.OutboundTestRequest {
	maxDelay := spec.MaxDelayMs
	if maxDelay <= 0 {
		maxDelay = 5000
	}
	return &pb.OutboundTestRequest{
		ConfigLink:     spec.ConfigLink,
		TestUrl:        spec.TestURL,
		TimeoutSeconds: (maxDelay + 999) / 1000,
		MaxDelayMs:     maxDelay,
		Retries:        spec.Retries,
		InsecureTls:    spec.InsecureTLS,
		Speedtest:      spec.Speedtest,
		SpeedtestKb:    spec.SpeedtestKb,
		DirectProbe:    spec.DirectProbe,
	}
}

// OutboundTestResultFromPB maps the wire response back to the package struct.
func OutboundTestResultFromPB(resp *pb.OutboundTestResponse) *OutboundTestResult {
	return &OutboundTestResult{
		Success:      resp.Success,
		Status:       resp.Status,
		LatencyMs:    resp.LatencyMs,
		TTFBMs:       resp.TtfbMs,
		ConnectMs:    resp.ConnectTimeMs,
		StatusCode:   resp.StatusCode,
		IP:           resp.Ip,
		Country:      resp.Country,
		DownloadMbps: resp.DownloadMbps,
		UploadMbps:   resp.UploadMbps,
		Error:        resp.Error,
		Message:      resp.Message,
	}
}
