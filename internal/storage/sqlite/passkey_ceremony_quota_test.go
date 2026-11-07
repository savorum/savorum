package sqlite

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/savorum/savorum/internal/identity"
)

func TestPasskeyRegistrationCeremoniesAreBoundedPerUserAndOverall(t *testing.T) {
	s := openStore(t.Context(), t)

	data := webauthn.SessionData{Challenge: "challenge"}
	for i := range 5 {
		if err := s.SaveWebAuthnCeremony(t.Context(), fmt.Sprint(i), "user", fmt.Sprint(i), "register", "rp", data); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.SaveWebAuthnCeremony(t.Context(), "replacement", "user", "0", "register", "rp", data); err != nil {
		t.Fatal(err)
	}

	if _, err := s.TakeWebAuthnCeremony(t.Context(), "0", "user", "0", "register", "rp"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("old challenge survived", err)
	}

	if err := s.SaveWebAuthnCeremony(t.Context(), "six", "user", "six", "register", "rp", data); !errors.Is(err, identity.ErrConflict) {
		t.Fatal("principal quota bypass", err)
	}

	if _, err := s.db.ExecContext(t.Context(), `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<4995) INSERT INTO webauthn_ceremonies SELECT 'fill-'||x,'user-'||x,'session','register','rp','{}',unixepoch()+300 FROM n`); err != nil {
		t.Fatal(err)
	}

	if err := s.SaveWebAuthnCeremony(t.Context(), "another", "other", "session", "register", "rp", data); !errors.Is(err, identity.ErrConflict) {
		t.Fatal("registration capacity bypass", err)
	}

	if err := s.SaveWebAuthnCeremony(t.Context(), "login", "", "", "login", "rp", data); err != nil {
		t.Fatal("registration pool denied login", err)
	}
}

func TestPasskeyRegistrationQuotaHoldsUnderConcurrentRequests(t *testing.T) {
	s := openStore(t.Context(), t)
	outcomes := make(chan error, 12)

	for i := range 12 {
		go func() {
			outcomes <- s.SaveWebAuthnCeremony(t.Context(), fmt.Sprint(i), "user", fmt.Sprint(i), "register", "rp", webauthn.SessionData{Challenge: "challenge"})
		}()
	}

	accepted := 0

	for range 12 {
		err := <-outcomes
		if err == nil {
			accepted++
		} else if !errors.Is(err, identity.ErrConflict) {
			t.Fatal(err)
		}
	}

	if accepted != 5 {
		t.Fatal("concurrent principal quota", accepted)
	}
}
