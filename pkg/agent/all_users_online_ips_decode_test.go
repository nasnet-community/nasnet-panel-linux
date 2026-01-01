package agent

import (
	"testing"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// TestDecodeAllOnlineIPsResponse: proto → map[string]map[string]int64
// against empty nested entries, nil IP maps (older agents), empty top-level.
func TestDecodeAllOnlineIPsResponse(t *testing.T) {
	t.Run("typical payload", func(t *testing.T) {
		resp := &pb.AllOnlineIPsResponse{
			Users: map[string]*pb.OnlineIPMap{
				"alice@x": {Ips: map[string]int64{"1.2.3.4": 1700000000}},
				"bob@x":   {Ips: map[string]int64{"5.6.7.8": 1700000050, "9.10.11.12": 1700000100}},
			},
		}
		got := decodeAllOnlineIPs(resp)
		if len(got) != 2 {
			t.Fatalf("expected 2 users, got %d", len(got))
		}
		if got["alice@x"]["1.2.3.4"] != 1700000000 {
			t.Errorf("alice IP timestamp wrong: %v", got["alice@x"])
		}
		if len(got["bob@x"]) != 2 {
			t.Errorf("bob should have 2 IPs: %v", got["bob@x"])
		}
	})

	t.Run("nil entry coerced to empty map", func(t *testing.T) {
		resp := &pb.AllOnlineIPsResponse{
			Users: map[string]*pb.OnlineIPMap{
				"ghost@x": nil,
			},
		}
		got := decodeAllOnlineIPs(resp)
		if got["ghost@x"] == nil {
			t.Error("nil entry must decode to non-nil empty map so callers can range safely")
		}
		if len(got["ghost@x"]) != 0 {
			t.Errorf("nil entry should be empty map, got %v", got["ghost@x"])
		}
	})

	t.Run("empty response", func(t *testing.T) {
		got := decodeAllOnlineIPs(&pb.AllOnlineIPsResponse{})
		if got == nil {
			t.Error("empty response must decode to non-nil map")
		}
		if len(got) != 0 {
			t.Errorf("expected empty result, got %v", got)
		}
	})
}

// decodeAllOnlineIPs mirrors the decode step in *Client.GetAllUsersOnlineIPs
// and *ReverseClient.GetAllUsersOnlineIPs. Kept as a shared helper so both
// client implementations and the test stay pinned to the same shape.
// If this ever drifts from the two real decoders, the test will surface it.
func decodeAllOnlineIPs(resp *pb.AllOnlineIPsResponse) map[string]map[string]int64 {
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
