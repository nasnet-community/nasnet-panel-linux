package domain

import "testing"

func TestHost_IsInfoOnly(t *testing.T) {
	tests := []struct {
		name      string
		inboundID *uint
		planID    *uint
		want      bool
	}{
		{"plan host with no inbound", nil, uintPtr(7), true},
		{"server host bound to inbound", uintPtr(3), nil, false},
		{"inbound host also tagged to plan", uintPtr(3), uintPtr(7), false},
		{"detached host", nil, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Host{InboundID: tt.inboundID, PlanID: tt.planID}
			if got := h.IsInfoOnly(); got != tt.want {
				t.Errorf("IsInfoOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInbound_GetActiveHosts_EmptyAndAllDisabled(t *testing.T) {
	if got := (&Inbound{}).GetActiveHosts(); got != nil {
		t.Errorf("no hosts should return nil, got %v", got)
	}
	in := &Inbound{Hosts: []Host{{ID: 1, IsDisabled: true}, {ID: 2, IsDisabled: true}}}
	if got := in.GetActiveHosts(); got != nil {
		t.Errorf("all-disabled should return nil, got %v", got)
	}
}

// Active hosts drop disabled ones and sort by priority then ID.
func TestInbound_GetActiveHosts_FilterAndSort(t *testing.T) {
	in := &Inbound{Hosts: []Host{
		{ID: 3, Priority: 1},
		{ID: 1, Priority: 0, IsDisabled: true},
		{ID: 2, Priority: 1},
		{ID: 5, Priority: 0},
	}}
	got := in.GetActiveHosts()
	wantIDs := []uint{5, 2, 3}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d active hosts, want %d", len(got), len(wantIDs))
	}
	for i, id := range wantIDs {
		if got[i].ID != id {
			t.Errorf("position %d: got host %d, want %d", i, got[i].ID, id)
		}
	}
}
