package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnlimitedRecoveryAttemptsSurviveSerialization(t *testing.T) {
	data, err := json.Marshal(CrashRecoverySettings{Enabled: true, Command: "true", MaxAttempts: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"max_attempts":0`) {
		t.Fatalf("unlimited attempts omitted from settings response: %s", data)
	}
}
