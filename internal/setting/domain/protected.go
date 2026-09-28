package domain

import "context"

// protectedKeys may only be written by their owning feature; the generic
// settings API skips them.
var protectedKeys = map[string]bool{
	"agent_cert_strict_mode": true,
}

type protectedWriteKey struct{}

// AllowProtectedWrite marks ctx as coming from the feature that owns a
// protected key.
func AllowProtectedWrite(ctx context.Context) context.Context {
	return context.WithValue(ctx, protectedWriteKey{}, true)
}

func ProtectedWriteAllowed(ctx context.Context) bool {
	v, _ := ctx.Value(protectedWriteKey{}).(bool)
	return v
}

func IsProtectedKey(key string) bool { return protectedKeys[key] }
