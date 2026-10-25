package identity

import "context"

type browserSessionKey struct{}

// WithBrowserSession binds native session creation to its browser authentication.
func WithBrowserSession(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, browserSessionKey{}, id)
}

// BrowserSession reports a browser session that must still authorize the exchange.
func BrowserSession(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(browserSessionKey{}).(string)
	return id, ok
}

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
