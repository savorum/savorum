package print

import (
	"context"
	"fmt"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

// renderAuthority copies only the authority needed to authorize frozen asset
// reads. It never retains a browser request, session credential or request values.
type renderAuthority struct {
	principal identity.Principal
	present   bool
	libraryID string
}

func captureRenderAuthority(ctx context.Context) renderAuthority {
	principal, present := identity.PrincipalFromContext(ctx)
	return renderAuthority{principal: identity.Principal{UserID: principal.UserID, System: principal.System}, present: present, libraryID: access.LibraryID(ctx)}
}

func (a renderAuthority) bind(ctx context.Context) context.Context {
	if a.present {
		ctx = identity.WithPrincipal(ctx, a.principal)
	}

	if a.libraryID != "" {
		ctx = access.WithLibrary(ctx, a.libraryID)
	}

	return ctx
}

func (s *Service) requestScope(ctx context.Context) string {
	authority := captureRenderAuthority(ctx)
	if !authority.present && authority.libraryID == "" {
		return s.authorizationScope
	}

	return fmt.Sprintf("service:%q:library:%q:user:%q:system:%t", s.authorizationScope, authority.libraryID, authority.principal.UserID, authority.principal.System)
}
