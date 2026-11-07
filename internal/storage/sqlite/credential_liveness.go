package sqlite

// Queries must alias browser sessions as b, personal tokens as t, and invitations as i.

const liveBrowserSession = `b.revoked_at IS NULL AND b.idle_expires>unixepoch() AND b.absolute_expires>unixepoch()` +
	` AND EXISTS(SELECT 1 FROM users WHERE users.id=b.user_id AND users.status='active')`

const issuingProviderEnabled = `(t.source_provider='' OR EXISTS(SELECT 1 FROM auth_providers WHERE auth_providers.id=t.source_provider AND auth_providers.enabled=1))`

const livePersonalToken = `t.revoked_at=0 AND (t.expires_at=0 OR t.expires_at>unixepoch())` +
	` AND EXISTS(SELECT 1 FROM users WHERE users.id=t.user_id AND users.status='active') AND ` + issuingProviderEnabled

const pendingInvitation = `i.accepted_at IS NULL AND i.revoked_at IS NULL`

const usableInvitation = pendingInvitation + ` AND i.expires_at>unixepoch()`
