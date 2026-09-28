package repository

import (
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
)

const (
	// identifierMinLen is the shortest term that engages identifier matching.
	// Anything shorter would prefix-match a large share of the table and force
	// a scan, so short input keeps the plain text-search behaviour.
	identifierMinLen = 8

	// canonicalUUIDLen is the length of a hyphenated UUID ("8-4-4-4-12").
	canonicalUUIDLen = 36
)

// identifierSearchClause builds the OR-clause that matches a subscription by
// its public link key (the /sub/<uuid> key) or its config UUID.
//
// A full-length UUID matches by equality, so the unique indexes on link_key and
// config_id are hit directly; a shorter term matches as a prefix, which those
// same indexes still serve. Equality is checked against the lowercase form too,
// because a key pinned through RegenerateSubscriptionKey can carry an operator's
// own casing while generated ones are always lowercase.
//
// ok is false when the term cannot be an identifier — too short, or holding
// characters no UUID has — and the caller leaves its query untouched.
//
// The returned clause is already parenthesised: callers OR it into a larger
// condition that is itself ANDed with other filters, and losing that grouping
// would widen the query past them.
func identifierSearchClause(term string) (clause string, args []any, ok bool) {
	t := strings.TrimSpace(term)
	if len(t) < identifierMinLen || !isHexOrDash(t) {
		return "", nil, false
	}

	if len(t) == canonicalUUIDLen {
		conds := []string{"subscriptions.link_key = ?", "subscriptions.config_id = ?"}
		args = []any{t, t}
		if lower := strings.ToLower(t); lower != t {
			conds = append(conds, "subscriptions.link_key = ?", "subscriptions.config_id = ?")
			args = append(args, lower, lower)
		}
		return "(" + strings.Join(conds, " OR ") + ")", args, true
	}

	prefix := t + "%"
	clause = "(" + database.ILike("subscriptions.link_key", "?") +
		" OR " + database.ILike("subscriptions.config_id", "?") + ")"
	return clause, []any{prefix, prefix}, true
}

func isHexOrDash(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F', r == '-':
		default:
			return false
		}
	}
	return true
}
