package sqlite

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/identity"
)

func TestCredentialAuthorityUpgradeRevokesUnattributedExternalTokens(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "old.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	partial := &Store{db: &writerDB{raw: db}, dsn: path, path: path}

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}

	for i, m := range migrations[:13] {
		if err := partial.applyMigration(ctx, m, i == 0); err != nil {
			t.Fatal(err)
		}
	}

	for _, q := range []string{
		`INSERT INTO users(id,display_name,instance_admin,created_at,updated_at) VALUES('external','External',1,0,0),('local','Local',0,0,0)`,
		`INSERT INTO auth_providers(id,kind,display_name,enabled) VALUES('cloud','managed','Cloud',1)`,
		`INSERT INTO identities(user_id,provider,provider_subject) VALUES('external','cloud','subject'),('local','local','login')`,
		`INSERT INTO webhook_subscriptions(id,library_id,name,url,event_types,enabled,revision,created_at,updated_at,updated_by,rotated_at,secret_ref,created_sequence) VALUES('owned','','Owned','https://receiver.example/events','["user.updated"]',1,1,0,0,'external',0,'key',0),('unknown','','Unknown','https://other.example/events','["user.updated"]',1,1,0,0,'',0,'other-key',0)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	secrets := map[string]string{"external": "savorum_pat_" + strings.Repeat("x", 64), "local": "savorum_pat_" + strings.Repeat("y", 64)}
	for user, secret := range secrets {
		if _, err := db.ExecContext(ctx, `INSERT INTO personal_tokens(id,user_id,token_hash,name,created_at,expires_at,scopes,libraries) VALUES(?,?,?,'Integration',0,0,'["recipes:read"]','[]')`, "pat:"+user, user, tokenHash(secret)); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = s.Close() })

	if _, _, err := s.ResolvePersonalToken(ctx, secrets["external"]); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("unattributed external token survived: %v", err)
	}

	if _, _, err := s.ResolvePersonalToken(ctx, secrets["local"]); err != nil {
		t.Fatalf("local token lost: %v", err)
	}

	if hook, err := s.DeliveryWebhook(ctx, "owned"); err != nil || hook.URL != "https://receiver.example/events" || hook.SecretRef != "key" {
		t.Fatalf("owned integration changed: %+v %v", hook, err)
	}

	var (
		enabled bool
		reason  string
	)
	if err := s.db.QueryRowContext(ctx, `SELECT enabled,pause_reason FROM webhook_subscriptions WHERE id='unknown'`).Scan(&enabled, &reason); err != nil || enabled || reason != "authorization-required" {
		t.Fatalf("unattributed webhook active: %v %q %v", enabled, reason, err)
	}
}
