package identity

import (
	"context"
	"slices"
)

// TokenPolicy controls personal automation credential lifetimes.
type TokenPolicy struct {
	DefaultDays      int  `json:"default_days"`
	MaximumDays      int  `json:"maximum_days"`
	AllowNonexpiring bool `json:"allow_nonexpiring"`
}

// PersonalToken contains only safe credential metadata.
type PersonalToken struct {
	Audience   string   `json:"-"`
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	CreatedAt  int64    `json:"created_at"`
	ExpiresAt  int64    `json:"expires_at"`
	LastUsedAt int64    `json:"last_used_at"`
	RevokedAt  int64    `json:"revoked_at"`
	Scopes     []string `json:"scopes"`
	Libraries  []string `json:"libraries"`
}

// TokenRequest specifies a capability subset, never instance administration.
type TokenRequest struct {
	Name        string   `json:"name"`
	Days        int      `json:"days"`
	Nonexpiring bool     `json:"nonexpiring"`
	Scopes      []string `json:"scopes"`
	Libraries   []string `json:"libraries"`
}

// TokenGrant is returned once; the secret is never part of list responses.
type TokenGrant struct {
	PersonalToken
	Secret string `json:"secret"`
}

// ValidTokenScope recognizes the initial coarse automation capability set.
func ValidTokenScope(scope string) bool {
	return slices.Contains([]string{"recipes:read", "recipes:write", "library:read", "library:write", "imports:write"}, scope)
}

type personalTokenKey struct{}

// WithPersonalToken records the token that authenticated a request.
func WithPersonalToken(ctx context.Context, token PersonalToken) context.Context {
	return context.WithValue(ctx, personalTokenKey{}, token)
}

// PersonalTokenFromContext returns the token that authenticated the request, if any.
func PersonalTokenFromContext(ctx context.Context) (PersonalToken, bool) {
	token, ok := ctx.Value(personalTokenKey{}).(PersonalToken)
	return token, ok
}
