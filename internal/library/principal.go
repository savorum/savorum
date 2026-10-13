package library

import (
	"context"

	"github.com/savorum/savorum/internal/identity"
)

func principalUserID(ctx context.Context) string {
	if p, ok := identity.FromContext(ctx); ok {
		return p.UserID
	}
	// Legacy offline callers can still operate on legacy personal fixtures.
	// The running application always supplies its persisted principal.
	return DefaultUserID
}
