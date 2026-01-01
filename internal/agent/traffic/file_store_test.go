package traffic

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFileStoreLoadInitializesInboundMaps persists a bucket whose
// InboundUplink/InboundDownlink maps are nil, reloads it, then
// accumulates a delta. Before the fix this panics with
// "assignment to entry in nil map".
func TestFileStoreLoadInitializesInboundMaps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.json")

	// Truncate to the current hour bucket so it won't be evicted as stale.
	now := time.Now()
	bucketTS := now.Unix() / int64(time.Hour.Seconds()) * int64(time.Hour.Seconds())

	// Persist a file whose bucket omits inbound_uplink/inbound_downlink
	// (they will be nil after JSON unmarshal, just as if an older agent wrote them).
	persisted := fmt.Sprintf(`{
		"version": 2,
		"buckets": [{
			"timestamp": %d,
			"user_uplink": {},
			"user_downlink": {},
			"outbound_uplink": {},
			"outbound_downlink": {},
			"total_uplink": 0,
			"total_downlink": 0
		}]
	}`, bucketTS)
	if err := os.WriteFile(path, []byte(persisted), 0o644); err != nil {
		t.Fatal(err)
	}

	fs, err := NewFileStore(path, time.Hour, 7*24*time.Hour, time.Minute)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	defer fs.Close()

	fs.Accumulate(&XrayStatsSnapshot{
		InboundUplink:   map[string]int64{"in-1": 100},
		InboundDownlink: map[string]int64{"in-1": 200},
	}, now)
	// If we reach here without panic the fix works.
}
