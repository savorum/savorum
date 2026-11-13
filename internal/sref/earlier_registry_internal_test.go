package sref

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/savorum/savorum/internal/srefcheckout"
)

func TestTheEmbeddedEarlierRegistryMatchesItsRelease(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(srefcheckout.Path(t, "snapshots/0.4.0/registry/units.json"))
	if err != nil {
		t.Fatal(err)
	}

	var registry struct {
		Version string `json:"version"`
		Units   []struct {
			ID string `json:"id"`
		} `json:"units"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatal(err)
	}

	released := make([]string, 0, len(registry.Units))
	for _, unit := range registry.Units {
		released = append(released, unit.ID)
	}

	slices.Sort(released)

	embedded, _ := earlierRegistryUnits(registry.Version)
	if got := slices.Sorted(maps.Keys(embedded)); !slices.Equal(got, released) {
		t.Fatalf("embedded registry %s units differ from the release", registry.Version)
	}
}
