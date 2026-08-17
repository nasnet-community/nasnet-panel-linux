package usecase

import (
	"os"
	"testing"
)

// An apply reaches the host through the system package. Same reason as the
// note in internal/network/system/main_test.go.
func TestMain(m *testing.M) {
	os.Setenv("PATH", "")
	os.Exit(m.Run())
}
