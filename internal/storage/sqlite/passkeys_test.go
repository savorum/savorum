package sqlite

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/savorum/savorum/internal/identity"
)

func TestPasskeyPersistenceOwnershipLastMethodAndRPBinding(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	p, err := s.BootstrapAdmin(ctx, "chef", "Chef", "long password for passkey tests")
	if err != nil {
		t.Fatal(err)
	}

	credential := webauthn.Credential{ID: []byte("credential"), PublicKey: []byte("public-key")}
	if err = s.StorePasskey(ctx, p.UserID, "Security key", "example.test", credential); err != nil {
		t.Fatal(err)
	}

	keys, err := s.Passkeys(ctx, p.UserID)
	if err != nil || len(keys) != 1 || keys[0].Name != "Security key" {
		t.Fatal("stored credential", err)
	}

	id := base64.RawURLEncoding.EncodeToString(credential.ID)
	if err = s.ChangePasskey(ctx, "other-user", id, "Stolen", false); !errors.Is(err, identity.ErrConflict) {
		t.Fatal("cross-account rename")
	}

	if err = s.ChangePasskey(ctx, p.UserID, id, "Travel key", false); err != nil {
		t.Fatal(err)
	}

	next := credential

	next.Authenticator.SignCount = 1
	if err = s.UpdatePasskeyCredential(ctx, p.UserID, credential, next); err != nil {
		t.Fatal(err)
	}

	if err = s.UpdatePasskeyCredential(ctx, p.UserID, credential, next); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("stale counter update accepted")
	}

	if _, err = s.db.ExecContext(ctx, `DELETE FROM identities WHERE user_id=?`, p.UserID); err != nil {
		t.Fatal(err)
	}

	if err = s.ChangePasskey(ctx, p.UserID, id, "", true); !errors.Is(err, identity.ErrConflict) {
		t.Fatal("last method removed", err)
	}

	if _, err = s.GenerateRecoveryCodes(ctx, p.UserID); err != nil {
		t.Fatal(err)
	}

	if err = s.ChangePasskey(ctx, p.UserID, id, "", true); err != nil {
		t.Fatal("recovery fallback not counted", err)
	}
}

func TestPasskeyCeremonyBindingExpiryAndSingleUse(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	data := webauthn.SessionData{Challenge: "random-challenge", Expires: time.Now().Add(time.Minute)}
	if err := s.SaveWebAuthnCeremony(ctx, "secret", "user", "session", "register", "example.test", data); err != nil {
		t.Fatal(err)
	}

	for _, scope := range [][5]string{{"wrong", "user", "session", "register", "example.test"}, {"secret", "other", "session", "register", "example.test"}, {"secret", "user", "other", "register", "example.test"}, {"secret", "user", "session", "login", "example.test"}, {"secret", "user", "session", "register", "other.test"}} {
		if _, err := s.TakeWebAuthnCeremony(ctx, scope[0], scope[1], scope[2], scope[3], scope[4]); !errors.Is(err, identity.ErrCredentials) {
			t.Fatal("ceremony scope accepted")
		}
	}

	if got, err := s.TakeWebAuthnCeremony(ctx, "secret", "user", "session", "register", "example.test"); err != nil || got.Challenge != data.Challenge {
		t.Fatal(err)
	}

	if _, err := s.TakeWebAuthnCeremony(ctx, "secret", "user", "session", "register", "example.test"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("ceremony replay")
	}

	if err := s.SaveWebAuthnCeremony(ctx, "expired", "", "", "login", "example.test", data); err != nil {
		t.Fatal(err)
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE webauthn_ceremonies SET expires_at=unixepoch()-1`); err != nil {
		t.Fatal(err)
	}

	if _, err := s.TakeWebAuthnCeremony(ctx, "expired", "", "", "login", "example.test"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("expired ceremony")
	}
}

func TestPasskeysAtSeparateOriginsCoexist(t *testing.T) {
	s := openStore(t.Context(), t)

	user, err := s.BootstrapAdmin(t.Context(), "chef", "Chef", "long password for passkey tests")
	if err != nil {
		t.Fatal(err)
	}

	for _, rp := range []string{"recipes.example", "acorn-apple-basil-basket.connect.test"} {
		if err := s.StorePasskey(t.Context(), user.UserID, rp, rp, webauthn.Credential{ID: []byte(rp), PublicKey: []byte("key")}); err != nil {
			t.Fatal(err)
		}
	}

	keys, err := s.Passkeys(t.Context(), user.UserID)
	if err != nil || len(keys) != 2 || keys[0].RPID == keys[1].RPID {
		t.Fatalf("separate RP credentials: %v %v", keys, err)
	}
}
