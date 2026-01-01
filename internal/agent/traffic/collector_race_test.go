package traffic

import (
	"sync"
	"testing"
)

// TestBaselinePointerRace hammers the baseline field from two
// goroutines. Must pass under -race.
func TestBaselinePointerRace(t *testing.T) {
	c := &Collector{}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.baselineMu.Lock()
			_ = c.baseline
			c.baselineMu.Unlock()
		}()
		go func() {
			defer wg.Done()
			c.baselineMu.Lock()
			c.baseline = &XrayStatsSnapshot{}
			c.baselineMu.Unlock()
		}()
	}
	wg.Wait()
}
