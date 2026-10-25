package shopping

import (
	"math/big"
	"slices"
	"strings"
	"sync"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

var catalog = sync.OnceValue(units.DefaultCatalog)

type measure struct {
	amount *big.Rat
	unit   string
}

// RefreshAmounts totals each row's exact amounts. Units of one measuring
// system combine only when every conversion is exact.
func RefreshAmounts(list *ShoppingList) {
	for i := range list.Items {
		item := &list.Items[i]
		measures := []measure{}
		text := []string{}

		for _, source := range item.Sources {
			amount, ok := new(big.Rat).SetString(source.Amount)
			if !ok {
				if source.Quantity != "" {
					text = append(text, source.Quantity)
				}

				continue
			}

			measures = append(measures, measure{amount, source.Unit})
		}

		totals := []string{}
		for _, total := range combine(measures) {
			totals = append(totals, strings.TrimSpace(total.amount.RatString()+" "+units.LabelFor(total.unit, total.amount)))
		}

		item.Needed = strings.Join(append(totals, text...), " + ")
	}
}

func combine(measures []measure) []measure {
	groups := map[string][]measure{}
	order := []string{}

	for _, value := range measures {
		key := groupKey(value.unit)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}

		groups[key] = append(groups[key], value)
	}

	result := []measure{}

	for _, key := range order {
		result = append(result, sumGroup(groups[key])...)
	}

	return result
}

func groupKey(unit string) string {
	found, known := catalog().Lookup(unit)
	if !known || found.Kind != recipe.UnitPhysical {
		return "unit:" + unit
	}

	convention, _ := catalog().ConventionOf(unit)

	return "dimension:" + string(found.Dimension) + ":" + string(convention)
}

func sumGroup(values []measure) []measure {
	distinct := []string{}

	for _, value := range values {
		if !slices.Contains(distinct, value.unit) {
			distinct = append(distinct, value.unit)
		}
	}

	if len(distinct) == 1 {
		total := new(big.Rat)
		for _, value := range values {
			total.Add(total, value.amount)
		}

		return []measure{{total, distinct[0]}}
	}

	var best *measure

	for _, target := range distinct {
		total, ok := convertedTotal(values, target)
		if !ok {
			continue
		}

		candidate := measure{total, target}
		if best == nil || preferred(candidate, *best) {
			best = &candidate
		}
	}

	if best != nil {
		return []measure{*best}
	}

	result := []measure{}

	for _, unit := range distinct {
		same := []measure{}

		for _, value := range values {
			if value.unit == unit {
				same = append(same, value)
			}
		}

		result = append(result, sumGroup(same)...)
	}

	return result
}

func convertedTotal(values []measure, target string) (*big.Rat, bool) {
	total := new(big.Rat)

	for _, value := range values {
		if value.unit == target {
			total.Add(total, value.amount)
			continue
		}

		conversion, err := catalog().Convert(recipe.Rational(value.amount.RatString()), value.unit, target)
		if err != nil || !conversion.Exact() {
			return nil, false
		}

		total.Add(total, conversion.Rat())
	}

	return total, true
}

// preferred picks the largest unit whose total is at least one.
func preferred(candidate, current measure) bool {
	one := big.NewRat(1, 1)
	candidateWhole, currentWhole := candidate.amount.Cmp(one) >= 0, current.amount.Cmp(one) >= 0

	if candidateWhole != currentWhole {
		return candidateWhole
	}

	if candidateWhole {
		return candidate.amount.Cmp(current.amount) < 0
	}

	return candidate.amount.Cmp(current.amount) > 0
}

func compatible(left, right string) bool {
	return left == right || (groupKey(left) == groupKey(right) && strings.HasPrefix(groupKey(left), "dimension:"))
}

func mergeNeed(list *ShoppingList, need shoppingNeed) bool {
	if need.source.Amount == "" {
		return false
	}

	for i := range list.Items {
		item := &list.Items[i]
		if item.Bought || item.Deleted || item.Arranged || len(item.Fields) > 0 || len(item.Sources) == 0 || item.Sources[0].Amount == "" || !compatible(item.Sources[0].Unit, need.source.Unit) {
			continue
		}

		if !sameIngredient(*item, need.choice) {
			continue
		}

		item.Sources = append(item.Sources, need.source)
		item.Revision = list.Revision + 1

		return true
	}

	return false
}

// sameIngredient deliberately never fuzzy-matches names.
func sameIngredient(item ShoppingItem, choice ShoppingChoice) bool {
	return strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(choice.Name))
}
