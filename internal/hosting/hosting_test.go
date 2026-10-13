package hosting_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/hosting"
)

func bootstrap() hosting.Bootstrap {
	return hosting.Bootstrap{OperationID: "op", TenantID: "tenant", PublicURL: "https://tenant.example.test", ProviderID: "cloud", Issuer: "https://cloud.example.test", Owner: hosting.Owner{Subject: "subject", DisplayName: "Alex"}, LibraryName: "Recipes"}
}

func TestBootstrapTrustBoundaries(t *testing.T) {
	if err := bootstrap().Validate(); err != nil {
		t.Fatal(err)
	}

	for _, origin := range []string{"http://tenant.example.test", "https://user@tenant.example.test", "https://tenant.example.test/", "https://tenant.example.test?", "https://tenant.example.test#", "https://tenant.example.test/path"} {
		t.Run(origin, func(t *testing.T) {
			b := bootstrap()

			b.PublicURL = origin
			if b.Validate() == nil {
				t.Fatal("unsafe origin accepted")
			}
		})
	}

	for _, provider := range []string{"local", "token", "system", "passkey", "recovery", "Cloud", strings.Repeat("x", 65)} {
		t.Run(provider, func(t *testing.T) {
			b := bootstrap()

			b.ProviderID = provider
			if b.Validate() == nil {
				t.Fatal("invalid provider accepted")
			}
		})
	}

	for _, text := range []string{"", " Alex", "Alex\n", strings.Repeat("界", 129)} {
		b := bootstrap()

		b.Owner.DisplayName = text
		if b.Validate() == nil {
			t.Fatalf("invalid display name %q accepted", text)
		}
	}
}

func TestBootstrapMismatchNamesExactChangedBindings(t *testing.T) {
	b := bootstrap()
	changed := b
	changed.Issuer = "https://other.example.test"

	changed.Owner.Subject = "other"
	if got := changed.Mismatches(b); !slices.Equal(got, []string{"issuer", "owner.subject"}) {
		t.Fatalf("mismatches = %v", got)
	}

	if got := b.Mismatches(b); len(got) != 0 {
		t.Fatalf("retry mismatch = %v", got)
	}
}

func TestMemberAuthorityRejectsIncompleteAndUnboundedBindings(t *testing.T) {
	m := hosting.MemberAuthority{OperationID: "op", TenantID: "tenant", BootstrapID: "boot", Subject: "account", UserID: "user", LibraryID: "default"}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	for _, mutate := range []func(*hosting.MemberAuthority){func(m *hosting.MemberAuthority) { m.Subject = "account\n" }, func(m *hosting.MemberAuthority) { m.LibraryID = "" }, func(m *hosting.MemberAuthority) { m.OperationID = strings.Repeat("x", 129) }, func(m *hosting.MemberAuthority) { m.UserID = "../other" }} {
		bad := m
		mutate(&bad)

		if bad.Validate() == nil {
			t.Fatal("invalid authority accepted")
		}
	}
}
