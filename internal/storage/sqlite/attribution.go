package sqlite

import (
	"context"
	"strings"

	"github.com/savorum/savorum/internal/identity"
)

// Actor names are resolved at read time so account erasure cannot leave private
// names in immutable revision snapshots. Missing historical actors stay unknown.
const actorDisplaySQL = `CASE WHEN actor_id = 'system' THEN 'System'
 WHEN actor_id = '' THEN 'Unknown actor'
 ELSE COALESCE((SELECT CASE WHEN status='deleted' THEN 'Former user' ELSE display_name END
 FROM users WHERE id=actor_id), 'Former user') END`

func actorDisplayFor(column string) string {
	return strings.ReplaceAll(actorDisplaySQL, "actor_id", column)
}

func attributionActor(ctx context.Context) string {
	principal, ok := identity.PrincipalFromContext(ctx)
	if !ok {
		return ""
	}

	if principal.System {
		return "system"
	}

	return principal.UserID
}
