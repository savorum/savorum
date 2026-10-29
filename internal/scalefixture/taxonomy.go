package scalefixture

import (
	"context"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

var facetKinds = []library.FacetKind{library.FacetCourse, library.FacetCuisine, library.FacetDietary, library.FacetMethod}

func seedTaxonomy(ctx context.Context, store *sqlite.Store, stamp time.Time) error {
	c := Policy()
	for i := range c.Taxonomy.Tags {
		if err := store.CreateTag(ctx, library.Tag{ID: fmt.Sprintf("tscale%d", i), Name: fmt.Sprintf("Kitchen label %d", i), Revision: 1, CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
			return err
		}
	}

	for id, name := range map[string]string{"tscale-common": "Every recipe", "tscale-popular": "Popular label", "tscale-singleton": "One recipe"} {
		if err := store.CreateTag(ctx, library.Tag{ID: id, Name: name, Revision: 1, CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
			return err
		}
	}

	for _, kind := range facetKinds {
		for i := range c.Taxonomy.FacetsPerKind {
			if err := store.CreateFacet(ctx, library.Facet{ID: fmt.Sprintf("fscale-%s-%d", kind, i), Kind: kind, Value: fmt.Sprintf("%s %d", kind, i), Revision: 1, CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
				return err
			}
		}
	}

	return nil
}

// Labels assigns rotating labels, popular categories, a ubiquitous tag and a singleton.
func Labels(index int) (tags, facets []string) {
	c := Policy()

	tags = []string{"tscale-common", fmt.Sprintf("tscale%d", index%c.Taxonomy.Tags), fmt.Sprintf("tscale%d", (index+1)%c.Taxonomy.Tags)}
	if index%2 == 0 {
		tags = append(tags, "tscale-popular")
	}

	if index == 0 {
		tags = append(tags, "tscale-singleton")
	}

	for n, kind := range facetKinds {
		value := index % c.Taxonomy.FacetsPerKind
		if kind != library.FacetMethod {
			value = 1 + index%(c.Taxonomy.FacetsPerKind-1)
			if index%[]int{2, 3, 5}[n] == 0 {
				value = 0
			}
		}

		facets = append(facets, fmt.Sprintf("fscale-%s-%d", kind, value))
	}

	return tags, facets
}
