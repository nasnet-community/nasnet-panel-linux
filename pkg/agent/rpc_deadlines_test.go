package agent

import (
	"context"
	"testing"
	"time"
)

func TestDeadlineFor_KnownMethods(t *testing.T) {
	cases := []struct {
		method string
		want   time.Duration
	}{
		{"GetStatus", 5 * time.Second},
		{"/nodeagent.NodeAgent/GetStatus", 5 * time.Second},
		{"RestartXray", 45 * time.Second},
		{"UpdateXrayBinary", 5 * time.Minute},
		{"Unknown_ZZ", DefaultRPCDeadline},
	}
	for _, c := range cases {
		if got := DeadlineFor(c.method); got != c.want {
			t.Errorf("DeadlineFor(%q) = %v, want %v", c.method, got, c.want)
		}
	}
}

func TestWithRPCDeadline_AppliesFallback(t *testing.T) {
	ctx, cancel := WithRPCDeadline(context.Background(), "GetStatus")
	defer cancel()

	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline on returned context")
	}
	remaining := time.Until(dl)
	if remaining <= 0 || remaining > 5*time.Second+50*time.Millisecond {
		t.Fatalf("deadline %v not within GetStatus budget", remaining)
	}
}

func TestWithRPCDeadline_RespectsExistingDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer parentCancel()

	ctx, cancel := WithRPCDeadline(parent, "UpdateXrayBinary")
	defer cancel()

	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected parent deadline to propagate")
	}
	remaining := time.Until(dl)
	if remaining > 110*time.Millisecond {
		t.Fatalf("expected to keep parent's 100ms deadline, got %v", remaining)
	}
}

func TestShortName(t *testing.T) {
	cases := map[string]string{
		"/nodeagent.NodeAgent/GetStatus": "GetStatus",
		"GetStatus":                      "GetStatus",
		"":                               "",
	}
	for in, want := range cases {
		if got := shortName(in); got != want {
			t.Errorf("shortName(%q) = %q, want %q", in, got, want)
		}
	}
}
