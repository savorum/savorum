package library

import (
	"reflect"
	"testing"
)

func TestStatedClaimsAreTheirOwnQualifiers(t *testing.T) {
	t.Parallel()

	criteria, err := ParseSearchQuery(`stated:vegan free:"tree nuts" dietary:Vegan`)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(criteria.StatedDiets, []string{"vegan"}) || !reflect.DeepEqual(criteria.StatedFreeFrom, []string{"tree nuts"}) {
		t.Fatalf("criteria = %#v", criteria)
	}

	if len(criteria.Facets[FacetDietary]) != 1 {
		t.Fatalf("the library's own dietary label was folded into a stated claim: %#v", criteria.Facets)
	}
}
