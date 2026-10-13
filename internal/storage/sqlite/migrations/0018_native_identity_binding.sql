-- Pending grants cannot survive a change to the identity that authenticated them.
ALTER TABLE native_sign_in_grants ADD COLUMN identity_id TEXT NOT NULL DEFAULT '';
DELETE FROM native_sign_in_grants;
