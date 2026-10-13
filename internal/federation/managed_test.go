package federation

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"golang.org/x/oauth2"
)

func TestManagedGrantTrustBoundariesAndRotation(t *testing.T) {
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	overlapPublic, other, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	exchangeEndpoint := "https://cloud.example/redeem"
	p := Provider{ID: "cloud", Kind: "managed", DisplayName: "Hosting", Managed: &ManagedConfig{Issuer: "https://cloud.example", Audience: "tenant", AuthorizeURL: "https://cloud.example/authorize", TokenURL: exchangeEndpoint, KeysFile: "keys.json"}}
	now := time.Now().Truncate(time.Second)
	verifier := oauth2.GenerateVerifier()

	for _, name := range []string{"valid", "issuer", "audience", "multiple-audience", "origin", "expired", "future", "long", "nonce", "subject", "local-user", "library", "signature", "type", "retired-key", "overlap-key", "replay"} {
		t.Run(name, func(t *testing.T) {
			grant := ManagedGrant{Claims: jwt.Claims{Issuer: p.Managed.Issuer, Subject: "account", Audience: jwt.Audience{"tenant"}, ID: "unique", IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Minute))}, Origin: "https://tenant.example", CodeChallenge: oauth2.S256ChallengeFromVerifier(verifier), LocalUserID: "user", LibraryID: "default"}
			signingKey := key
			kind := ManagedGrantType
			kid := "current"

			switch name {
			case "issuer":
				grant.Issuer = "https://evil.example"
			case "audience":
				grant.Audience = jwt.Audience{"other"}
			case "multiple-audience":
				grant.Audience = jwt.Audience{"tenant", "other"}
			case "origin":
				grant.Origin = "https://evil.example"
			case "expired":
				grant.Expiry = jwt.NewNumericDate(now)
			case "future":
				grant.IssuedAt = jwt.NewNumericDate(now.Add(time.Second))
			case "long":
				grant.Expiry = jwt.NewNumericDate(now.Add(time.Hour))
			case "nonce":
				grant.ID = ""
			case "subject":
				grant.Subject = ""
			case "local-user":
				grant.LocalUserID = ""
			case "library":
				grant.LibraryID = ""
			case "signature":
				signingKey = other
			case "type":
				kind = "JWT"
			case "retired-key":
				kid = "retired"
			case "overlap-key":
				kid = "overlap"
				signingKey = other
			}

			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: signingKey}, (&jose.SignerOptions{}).WithType(jose.ContentType(kind)).WithHeader("kid", kid))
			if err != nil {
				t.Fatal(err)
			}

			raw, err := jwt.Signed(signer).Claims(grant).Serialize()
			if err != nil {
				t.Fatal(err)
			}

			keys := map[string]ed25519.PublicKey{"current": public, "overlap": overlapPublic}

			_, err = VerifyManagedGrant(raw, p, keys, "https://tenant.example", now)
			if name == "valid" || name == "overlap-key" || name == "replay" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil {
					t.Fatalf("accepted %s", name)
				}

				return
			}

			h := &Handler{store: &fakeStore{}, options: Options{PublicURL: "https://tenant.example"}, managedKeys: map[string]map[string]ed25519.PublicKey{"cloud": keys}}
			if _, err = h.redeemManagedGrant(t.Context(), p, raw, verifier); err != nil {
				t.Fatal(err)
			}

			if _, err = h.redeemManagedGrant(t.Context(), p, raw, verifier); err == nil {
				t.Fatal("grant replay")
			}
		})
	}

	for _, change := range []func(*Provider){func(p *Provider) { p.AutoProvision = true }, func(p *Provider) { p.DefaultLibrary = "default" }, func(p *Provider) { p.Managed.TokenURL = "https://evil.example/redeem" }} {
		altered := p
		managed := *p.Managed
		altered.Managed = &managed
		change(&altered)

		if altered.validateManaged() == nil {
			t.Fatal("unsafe managed configuration")
		}
	}

	path := filepath.Join(t.TempDir(), "keys.json")

	data, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: public, KeyID: "current", Algorithm: "EdDSA", Use: "sig"}}})
	if err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if keys, err := LoadManagedKeys(path); err != nil || len(keys) != 1 {
		t.Fatalf("load keys %v", err)
	}
}
