package system

import (
	"os"
	"testing"
	"time"
)

func TestMarker_RoundTripAndAbsence(t *testing.T) {
	p := tmpPaths(t)

	got, err := ReadMarker(p)
	if err != nil {
		t.Fatalf("ReadMarker with no marker: %v", err)
	}
	if got != nil {
		t.Fatal("ReadMarker invented a marker")
	}

	m := Marker{PlanID: 7, Snapshot: "/var/lib/nasnet/snap-7.json",
		DeadlineUnix: time.Now().Add(ConfirmWindow).Unix()}
	if err := WriteMarker(p, m); err != nil {
		t.Fatal(err)
	}
	back, err := ReadMarker(p)
	if err != nil || back == nil {
		t.Fatalf("ReadMarker after write: %v %v", back, err)
	}
	if back.PlanID != 7 || back.Snapshot != m.Snapshot || back.DeadlineUnix != m.DeadlineUnix {
		t.Errorf("marker changed across the round trip: %+v", back)
	}

	if err := DeleteMarker(p); err != nil {
		t.Fatal(err)
	}
	if err := DeleteMarker(p); err != nil {
		t.Errorf("deleting an absent marker must succeed: %v", err)
	}
	if _, err := os.Stat(MarkerPath(p)); !os.IsNotExist(err) {
		t.Error("marker survived deletion")
	}
}

// 90s survives a DHCP renewal plus a browser reconnect.
func TestConfirmWindow(t *testing.T) {
	if ConfirmWindow != 90*time.Second {
		t.Errorf("ConfirmWindow = %v, want 90s", ConfirmWindow)
	}
}

func TestMarker_Expired(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	future := Marker{DeadlineUnix: now.Add(time.Minute).Unix()}
	past := Marker{DeadlineUnix: now.Add(-time.Second).Unix()}
	if future.Expired(now) {
		t.Error("a future deadline reported expired")
	}
	if !past.Expired(now) {
		t.Error("a past deadline reported unexpired")
	}
}
