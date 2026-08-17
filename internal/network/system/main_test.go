package system

import (
	"os"
	"testing"
)

// This package shells out to systemctl, networkctl and netplan. They are
// missing on a mac and real on a CI runner, which is why the same tests pass
// here and fail there. Empty PATH means neither runs.
func TestMain(m *testing.M) {
	os.Setenv("PATH", "")
	os.Exit(m.Run())
}
