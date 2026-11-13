package sref

import (
	_ "embed"
	"maps"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

// Registries 0.1.0 and 0.2.0 share unit IDs.
//
//go:embed registry-0.2.0-units.txt
var registry020Units string

type registryUnits map[string]bool

func (r registryUnits) Lookup(id string) (recipe.Unit, bool) {
	if !r[id] {
		return recipe.Unit{}, false
	}

	return units.DefaultCatalog().Lookup(id)
}

func earlierRegistryUnits(version string) (registryUnits, bool) {
	if version != "0.1.0" && version != "0.2.0" {
		return nil, false
	}

	known := registryUnits{}
	for id := range strings.FieldsSeq(registry020Units) {
		known[id] = true
	}

	return known, true
}

func upgradeDeclaredVersions(opaque map[string]any, declared envelope) map[string]any {
	if _, newer := compatibleVersion(declared.Version, Version); !newer {
		declared.Version = Version
	}

	if _, newer := compatibleVersion(declared.UnitRegistry, UnitRegistryVersion); !newer {
		declared.UnitRegistry = UnitRegistryVersion
	}

	upgraded := maps.Clone(opaque)
	if carried, ok := opaque["document/sref"].(map[string]any); ok {
		upgraded["document/sref"] = maps.Clone(carried)
	}

	return preserveDeclaredVersions(upgraded, declared)
}
