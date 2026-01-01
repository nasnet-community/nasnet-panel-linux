package domain

import "time"

// Scope identifies which layer produced an active maintenance status.
type Scope string

const (
	ScopeNone         Scope = ""
	ScopeGlobal       Scope = "global"
	ScopeNode         Scope = "node"
	ScopeSubscription Scope = "subscription"
)

// Status is the resolved maintenance state for a caller.
// Active=false => rest of fields are zero values / nil.
type Status struct {
	Active  bool       `json:"active"`
	Scope   Scope      `json:"scope"`
	Message string     `json:"message"`
	Since   *time.Time `json:"since,omitempty"`
}
