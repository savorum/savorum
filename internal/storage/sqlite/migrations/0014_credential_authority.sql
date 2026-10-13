ALTER TABLE personal_tokens ADD COLUMN source_provider TEXT NOT NULL DEFAULT '';

UPDATE personal_tokens SET revoked_at=unixepoch()
WHERE revoked_at=0 AND EXISTS (
    SELECT 1 FROM identities i JOIN auth_providers p ON p.id=i.provider
    WHERE i.user_id=personal_tokens.user_id
);

ALTER TABLE webhook_subscriptions ADD COLUMN authority_user TEXT NOT NULL DEFAULT '';
UPDATE webhook_subscriptions SET authority_user=updated_by;
UPDATE webhook_subscriptions SET enabled=0,pause_reason='authorization-required',revision=revision+1
WHERE deleted_at=0 AND authority_user='';

CREATE TABLE hosting_member_revocations (
    operation_id TEXT PRIMARY KEY,
    request TEXT NOT NULL
);
