package units_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/srefcheckout"
	"github.com/savorum/savorum/internal/units"
)

const registryPath = "../../../sref/registry/units.json"

// srefRegistryPath is where the pinned registry is read from.
// SREF_REGISTRY_PATH names a registry file directly; otherwise SREF_CHECKOUT,
// then the sibling checkout, is used.
func srefRegistryPath() string {
	if override := os.Getenv("SREF_REGISTRY_PATH"); override != "" {
		return override
	}

	if os.Getenv("SREF_CHECKOUT") != "" {
		if root, err := srefcheckout.Locate(); err == nil {
			return filepath.Join(root, "registry", "units.json")
		}
	}

	return registryPath
}

type srefRegistry struct {
	Version string `json:"version"`
	Units   []struct {
		ID         string `json:"id"`
		Kind       string `json:"kind"`
		Dimension  string `json:"dimension"`
		Status     string `json:"status"`
		Definition *struct {
			Kind       string `json:"kind"`
			BaseUnit   string `json:"base_unit"`
			Multiplier string `json:"multiplier"`
			Offset     string `json:"offset"`
			Exact      bool   `json:"exact"`
		} `json:"definition"`
		Aliases []struct {
			Language      string   `json:"language"`
			Forms         []string `json:"forms"`
			CaseSensitive bool     `json:"case_sensitive"`
		} `json:"aliases"`
	} `json:"units"`
}

func aliasKey(unitID, locale string, caseSensitive bool, forms []string) string {
	return fmt.Sprintf("%s\x00%s\x00%t\x00%s", unitID, locale, caseSensitive, strings.Join(forms, "\x00"))
}

// TestCatalogMatchesTheSREFRegistry checks that this catalog holds exactly the
// identities and physical definitions SREF's published registry names. The
// registry's `regions` member is not compared; it is not a Convention.
func TestCatalogMatchesTheSREFRegistry(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(srefRegistryPath())
	if err != nil {
		// SREF_REGISTRY_REQUIRED, set in CI, turns the local skip into a
		// failure.
		if os.Getenv("SREF_REGISTRY_REQUIRED") != "" || srefcheckout.Required() {
			t.Fatalf("the SREF registry was required but could not be read: %v", err)
		}

		t.Skipf("the SREF registry is not beside this repository: %v", err)
	}

	var registry srefRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatalf("read the SREF registry: %v", err)
	}

	if registry.Version != sref.UnitRegistryVersion {
		t.Fatalf("the registry beside this repository is %s, but this build implements %s", registry.Version, sref.UnitRegistryVersion)
	}

	published := make(map[string]bool, len(registry.Units))
	publishedAliases := map[string]int{}

	entries := make(map[string]units.Entry)
	for _, entry := range units.DefaultCatalog().Entries() {
		entries[entry.Unit.ID] = entry
	}

	for _, registered := range registry.Units {
		if registered.Status != "active" {
			continue
		}

		published[registered.ID] = true
		for _, aliases := range registered.Aliases {
			publishedAliases[aliasKey(registered.ID, aliases.Language, aliases.CaseSensitive, aliases.Forms)]++
		}

		entry, held := entries[registered.ID]
		if !held {
			t.Errorf("SREF %s names %s, which this catalog does not hold", registry.Version, registered.ID)
			continue
		}

		if string(entry.Unit.Kind) != registered.Kind || string(entry.Unit.Dimension) != registered.Dimension {
			t.Errorf("%s is %s/%s here and %s/%s in SREF", registered.ID, entry.Unit.Kind, entry.Unit.Dimension, registered.Kind, registered.Dimension)
		}

		if entry.Unit.Kind != recipe.UnitPhysical {
			continue
		}

		if registered.Definition == nil {
			t.Errorf("SREF gives %s no physical definition, but this catalog converts it", registered.ID)
			continue
		}

		base, multiplier, offset := registered.Definition.BaseUnit, registered.Definition.Multiplier, registered.Definition.Offset
		// A base unit states no transformation because it is the identity, and
		// the identity is exact whether or not the record says so.
		exact := registered.Definition.Exact
		if registered.Definition.Kind == "base" {
			base, multiplier, exact = registered.ID, "1", true
		}

		if offset == "" {
			offset = "0"
		}

		if entry.BaseUnit != base || entry.Multiplier != multiplier || entry.Offset != offset || entry.Exact != exact {
			t.Errorf("%s converts as %s×%s+%s exact=%v here, and as %s×%s+%s exact=%v in SREF",
				registered.ID, entry.BaseUnit, entry.Multiplier, entry.Offset, entry.Exact,
				base, multiplier, offset, exact)
		}
	}

	for id := range entries {
		if !published[id] {
			t.Errorf("this catalog holds %s, which SREF %s does not name", id, registry.Version)
		}
	}

	if len(published) == 0 {
		t.Fatal("the SREF registry named no active units")
	}

	implementedAliases := map[string]int{}
	for _, aliases := range units.DefaultCatalog().AliasSets() {
		implementedAliases[aliasKey(aliases.UnitID, aliases.Language, aliases.CaseSensitive, aliases.Forms)]++
	}

	for key, count := range publishedAliases {
		if implementedAliases[key] != count {
			t.Errorf("SREF alias set %q occurs %d time(s), implementation has %d", key, count, implementedAliases[key])
		}
	}

	for key, count := range implementedAliases {
		if publishedAliases[key] != count {
			t.Errorf("implemented alias set %q occurs %d time(s), SREF has %d", key, count, publishedAliases[key])
		}
	}
}
