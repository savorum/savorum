package identity

import "context"

type sessionIdentityKey struct{}

// WithSessionIdentity binds native session creation to the redeemed identity.
func WithSessionIdentity(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIdentityKey{}, id)
}

// SessionIdentity reports an identity that must still authorize session creation.
func SessionIdentity(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(sessionIdentityKey{}).(string)
	return id, ok
}
