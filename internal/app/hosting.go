package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/hosting"
)

// checkHosting keeps a hosted instance authenticated at its bootstrapped
// origin, and lets a managed provider serve only the tenant it was
// bootstrapped for.
func checkHosting(ctx context.Context, store Store, options Options, publicURL string) (bool, error) {
	tenant, err := store.HostingTenant(ctx)

	hosted := err == nil
	if err != nil && !errors.Is(err, hosting.ErrNotHosted) {
		return false, err
	}

	if hosted {
		if !options.AuthEnabled {
			return false, errors.New("this instance was bootstrapped for hosting and must run with authentication enabled; run savorum hosting release to leave hosting")
		}

		if publicURL != tenant.PublicURL {
			return false, fmt.Errorf("this instance was bootstrapped for %s; set that public URL or run savorum hosting release to leave hosting", tenant.PublicURL)
		}
	}

	for _, p := range options.Providers.Providers {
		if p.Disabled || p.Kind != "managed" {
			continue
		}

		if !hosted {
			return false, fmt.Errorf("managed provider %s requires a hosting bootstrap; remove it from the provider configuration", p.ID)
		}

		if !matchesTenant(p, tenant) {
			return false, fmt.Errorf("managed provider %s does not match this instance's hosting bootstrap", p.ID)
		}
	}

	return hosted, nil
}

func matchesTenant(p federation.Provider, tenant hosting.Tenant) bool {
	return p.Managed != nil && p.ID == tenant.ProviderID && p.Managed.Audience == tenant.TenantID && p.Managed.Issuer == tenant.Issuer
}
