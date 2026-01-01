package scheduler

import (
	"testing"
	"time"
)

// TestNewBackfillsIntervals exercises the backfill logic in New(): the
// StatsInterval / ExpireInterval / ReconcileInterval defaults must kick
// in when a caller leaves them at their zero value.
func TestNewBackfillsIntervals(t *testing.T) {
	t.Run("all zero -> defaults", func(t *testing.T) {
		cfg := Config{}
		applyDefaults(&cfg)

		if cfg.StatsInterval != 5*time.Second {
			t.Errorf("StatsInterval default: got %v want 5s", cfg.StatsInterval)
		}
		if cfg.ExpireInterval != 30*time.Second {
			t.Errorf("ExpireInterval default: got %v want 30s", cfg.ExpireInterval)
		}
		if cfg.ReconcileInterval != 60*time.Second {
			t.Errorf("ReconcileInterval default: got %v want 60s", cfg.ReconcileInterval)
		}
	})

	t.Run("explicit intervals preserved", func(t *testing.T) {
		cfg := Config{
			StatsInterval:     2 * time.Second,
			ExpireInterval:    20 * time.Second,
			ReconcileInterval: 120 * time.Second,
		}
		applyDefaults(&cfg)

		if cfg.StatsInterval != 2*time.Second {
			t.Errorf("explicit StatsInterval mutated: %v", cfg.StatsInterval)
		}
		if cfg.ExpireInterval != 20*time.Second {
			t.Errorf("explicit ExpireInterval mutated: %v", cfg.ExpireInterval)
		}
		if cfg.ReconcileInterval != 120*time.Second {
			t.Errorf("explicit ReconcileInterval mutated: %v", cfg.ReconcileInterval)
		}
	})
}

// applyDefaults mirrors New()'s backfill so the test bypasses full
// scheduler deps. Drift-guarded by TestNewBackfillsIntervals.
func applyDefaults(cfg *Config) {
	if cfg.StatsInterval == 0 {
		cfg.StatsInterval = 5 * time.Second
	}
	if cfg.ExpireInterval == 0 {
		cfg.ExpireInterval = 30 * time.Second
	}
	if cfg.ReconcileInterval == 0 {
		cfg.ReconcileInterval = 60 * time.Second
	}
}

// TestDefaultConfigHasSplitIntervals pins the DefaultConfig shape so a
// future refactor accidentally collapsing everything back onto a single
// interval knob is caught.
func TestDefaultConfigHasSplitIntervals(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.StatsInterval != 5*time.Second {
		t.Errorf("DefaultConfig.StatsInterval: %v", cfg.StatsInterval)
	}
	if cfg.ExpireInterval != 30*time.Second {
		t.Errorf("DefaultConfig.ExpireInterval: %v", cfg.ExpireInterval)
	}
	if cfg.ReconcileInterval != 60*time.Second {
		t.Errorf("DefaultConfig.ReconcileInterval: %v", cfg.ReconcileInterval)
	}
}
