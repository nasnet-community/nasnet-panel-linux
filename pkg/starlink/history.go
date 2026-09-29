package starlink

import (
	"context"
	"fmt"
	"math"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/starlink/pb"
)

// Outage start times in get_history are GPS time; the event log is Unix.
// Offset = GPS epoch (1980-01-06) minus 18 leap seconds, checked against a
// live dish where the same outage appears in both lists.
const gpsEpochOffsetNs = int64(315964782) * int64(time.Second)

// History is the dish's own rolling buffer (900 × 1 s), averaged into StepS
// buckets. Point i sits at EndAt - (len-1-i)*StepS. Nil entries are buckets
// with no valid sample (NaN during an outage).
type History struct {
	EndAt    time.Time  `json:"end_at"`
	StepS    int        `json:"step_s"`
	Latency  []*float64 `json:"pop_ping_latency_ms"`
	DropRate []*float64 `json:"pop_ping_drop_rate"`
	Downlink []*float64 `json:"downlink_throughput_bps"`
	Uplink   []*float64 `json:"uplink_throughput_bps"`
	Power    []*float64 `json:"power_w"`
	Outages  []Outage   `json:"outages"`
	Events   []Event    `json:"events"`
}

type Outage struct {
	Cause      string    `json:"cause"`
	StartAt    time.Time `json:"start_at"`
	DurationMs int64     `json:"duration_ms"`
	DidSwitch  bool      `json:"did_switch"`
}

type Event struct {
	Severity   string    `json:"severity"`
	Reason     string    `json:"reason"`
	StartAt    time.Time `json:"start_at"`
	DurationMs int64     `json:"duration_ms"`
}

// GetHistory fetches the dish history, bucketed into step-second means.
func (c *Client) GetHistory(ctx context.Context, step int) (*History, error) {
	resp, err := c.handle(ctx, &pb.Request{
		Request: &pb.Request_GetHistory{GetHistory: &pb.GetHistoryRequest{}},
	})
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	h := resp.GetDishGetHistory()
	if h == nil {
		return nil, fmt.Errorf("unexpected response type for get_history")
	}
	if step < 1 {
		step = 1
	}

	out := &History{
		EndAt:    time.Now(),
		StepS:    step,
		Latency:  bucket(orderRing(h.PopPingLatencyMs, h.Current), step, clampNonNeg),
		DropRate: bucket(orderRing(h.PopPingDropRate, h.Current), step, clampUnit),
		Downlink: bucket(orderRing(h.DownlinkThroughputBps, h.Current), step, clampNonNeg),
		Uplink:   bucket(orderRing(h.UplinkThroughputBps, h.Current), step, clampNonNeg),
		Power:    bucket(orderRing(h.PowerIn, h.Current), step, clampNonNeg),
		Outages:  []Outage{},
		Events:   []Event{},
	}

	for _, o := range h.Outages {
		out.Outages = append(out.Outages, Outage{
			Cause:      o.Cause.String(),
			StartAt:    time.Unix(0, o.StartTimestampNs+gpsEpochOffsetNs).UTC(),
			DurationMs: int64(o.DurationNs / uint64(time.Millisecond)),
			DidSwitch:  o.DidSwitch,
		})
	}
	if el := h.EventLog; el != nil {
		for _, e := range el.Events {
			out.Events = append(out.Events, Event{
				Severity:   e.Severity.String(),
				Reason:     e.Reason.String(),
				StartAt:    time.Unix(0, e.StartTimestampNs).UTC(),
				DurationMs: int64(e.DurationNs / uint64(time.Millisecond)),
			})
		}
		if el.CurrentTimestampNs > 0 {
			out.EndAt = time.Unix(0, el.CurrentTimestampNs).UTC()
		}
	}
	return out, nil
}

// orderRing unrolls one of the dish's ring buffers oldest→newest. current is
// the total number of samples ever written, so current%len is the slot the
// dish writes next — the oldest one once the buffer has wrapped.
func orderRing(ring []float32, current uint64) []float32 {
	n := len(ring)
	if n == 0 {
		return nil
	}
	if current < uint64(n) {
		return append([]float32(nil), ring[:current]...)
	}
	start := int(current % uint64(n))
	out := make([]float32, 0, n)
	out = append(out, ring[start:]...)
	return append(out, ring[:start]...)
}

// bucket averages src in step-sized groups aligned to the newest sample,
// dropping a partial group at the old end. NaN/Inf samples are skipped; a
// group with none left becomes nil (a gap in the chart).
func bucket(src []float32, step int, clean func(float64) float64) []*float64 {
	out := make([]*float64, 0, len(src)/step)
	for i := len(src) % step; i+step <= len(src); i += step {
		var sum float64
		var n int
		for _, v := range src[i : i+step] {
			f := float64(v)
			if math.IsNaN(f) || math.IsInf(f, 0) {
				continue
			}
			sum += f
			n++
		}
		if n == 0 {
			out = append(out, nil)
			continue
		}
		v := clean(sum / float64(n))
		out = append(out, &v)
	}
	return out
}
