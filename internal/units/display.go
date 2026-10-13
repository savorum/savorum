package units

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

// MeasurementSystem is the measures a reader asked to see. It is a projection
// over the stored recipe and never rewrites it.
type MeasurementSystem string

// Measurement systems identify the projection requested for display.
const (
	// MeasureAsWritten keeps every amount in the unit the recipe states.
	MeasureAsWritten MeasurementSystem = "original"
	MeasureMetric    MeasurementSystem = "metric"
	MeasureUS        MeasurementSystem = "us"
	MeasureAU        MeasurementSystem = "au"
)

// ValidMeasurementSystem reports whether value names a system a reader may ask
// for. Australian measures use metric mass and temperature alongside their
// distinct 250 mL cup and 20 mL tablespoon.
func ValidMeasurementSystem(value MeasurementSystem) bool {
	switch value {
	case MeasureAsWritten, MeasureMetric, MeasureUS, MeasureAU:
		return true
	default:
		return false
	}
}

// Label is how a unit is written for a cook: display policy, not part of the
// physical definition. The browser and the document renderer share this table.
type Label struct {
	One  string
	Many string
	// Tight marks a label written without a space before it in prose, such as
	// a degree sign.
	Tight bool
}

// labels covers every identity DefaultCatalog holds. A unit added to the
// catalog without a label fails TestEveryCatalogUnitCanBeReadAloud rather than
// reaching a reader as a bare registry identifier.
var labels = map[string]Label{
	"mass.gram":                         {One: "g", Many: "g", Tight: true},
	"mass.kilogram":                     {One: "kg", Many: "kg", Tight: true},
	"mass.ounce.avoirdupois":            {One: "oz", Many: "oz"},
	"mass.pound.avoirdupois":            {One: "lb", Many: "lb"},
	"volume.milliliter":                 {One: "ml", Many: "ml", Tight: true},
	"volume.liter":                      {One: "l", Many: "l", Tight: true},
	"volume.teaspoon.metric.culinary":   {One: "tsp", Many: "tsp"},
	"volume.deciliter":                  {One: "dl", Many: "dl", Tight: true},
	"volume.centiliter":                 {One: "cl", Many: "cl", Tight: true},
	"volume.cup.metric.culinary":        {One: "cup", Many: "cups"},
	"volume.cup.us.customary":           {One: "cup", Many: "cups"},
	"volume.tablespoon.us.customary":    {One: "tbsp", Many: "tbsp"},
	"volume.teaspoon.us.customary":      {One: "tsp", Many: "tsp"},
	"volume.cup.us.legal":               {One: "cup", Many: "cups"},
	"volume.tablespoon.us.legal":        {One: "legal tbsp", Many: "legal tbsp"},
	"volume.tablespoon.metric.culinary": {One: "tbsp", Many: "tbsp"},
	"volume.teaspoon.us.legal":          {One: "legal tsp", Many: "legal tsp"},
	"volume.fluidounce.us":              {One: "fl oz", Many: "fl oz"},
	"volume.pint.us.liquid":             {One: "pint", Many: "pints"},
	"volume.quart.us.liquid":            {One: "quart", Many: "quarts"},
	"volume.gallon.us.liquid":           {One: "gallon", Many: "gallons"},
	"volume.cup.us.culinary":            {One: "cup", Many: "cups"},
	"volume.tablespoon.us.culinary":     {One: "tbsp", Many: "tbsp"},
	"volume.teaspoon.us.culinary":       {One: "tsp", Many: "tsp"},
	"volume.cup.au":                     {One: "cup", Many: "cups"},
	"volume.tablespoon.au":              {One: "tbsp", Many: "tbsp"},
	"volume.teaspoon.au":                {One: "tsp", Many: "tsp"},
	"volume.cup.jp":                     {One: "cup", Many: "cups"},
	"volume.cup.kr":                     {One: "cup", Many: "cups"},
	"volume.fluidounce.imperial":        {One: "fl oz", Many: "fl oz"},
	"volume.pint.imperial":              {One: "pint", Many: "pints"},
	"volume.quart.imperial":             {One: "quart", Many: "quarts"},
	"volume.gallon.imperial":            {One: "gallon", Many: "gallons"},
	"length.millimeter":                 {One: "mm", Many: "mm", Tight: true},
	"length.centimeter":                 {One: "cm", Many: "cm", Tight: true},
	"length.inch.international":         {One: "in", Many: "in"},
	"temperature.celsius":               {One: "°C", Many: "°C", Tight: true},
	"temperature.fahrenheit":            {One: "°F", Many: "°F", Tight: true},
	"count.item":                        {One: "", Many: ""},
	"count.serving":                     {One: "serving", Many: "servings"},
	"count.egg":                         {One: "egg", Many: "eggs"},
	"count.clove":                       {One: "clove", Many: "cloves"},
	"count.sprig":                       {One: "sprig", Many: "sprigs"},
	"count.bunch":                       {One: "bunch", Many: "bunches"},
	"count.pinch":                       {One: "pinch", Many: "pinches"},
	"count.handful":                     {One: "handful", Many: "handfuls"},
	"container.can":                     {One: "can", Many: "cans"},
	"container.tin":                     {One: "tin", Many: "tins"},
	"container.packet":                  {One: "packet", Many: "packets"},
}

// LookupLabel reports how one unit is written. The second result is false for
// an identity the catalog does not know.
func LookupLabel(id string) (Label, bool) {
	label, found := labels[id]
	return label, found
}

// LabelFor writes the unit beside an amount, choosing the singular wording for
// an amount of one or less the way a cook would say it.
func LabelFor(id string, amount *big.Rat) string {
	label, found := labels[id]
	if !found {
		return id
	}

	if amount != nil && amount.Cmp(big.NewRat(1, 1)) <= 0 {
		return label.One
	}

	return label.Many
}

// Rung is one step of a rounding ladder: the unit a converted amount is
// written in while it stays below Ceiling, and the practical measure it is
// rounded to. Ceiling is stated in the rung's own unit so this policy never
// restates a conversion factor the registry already holds.
type Rung struct {
	Unit    string
	Ceiling string
	Step    string
}

// VolumePresentationPolicy defines acceptable kitchen approximations for US
// and Australian volume measures. It is published to the frontend so screens
// and printed documents make the same choice.
type VolumePresentationPolicy struct {
	// Fractions are the physical measures available per rung of the volume
	// family: teaspoon, tablespoon, and cup. Cup measures come in halves,
	// thirds, and quarters.
	Fractions [][]string
	// CompoundFractions is what the larger measure of a two-measure plan may
	// read as, given per rung. Cups come as graduated vessels, so half a cup
	// and two tablespoons is practical; one and a half tablespoons is not.
	CompoundFractions [][]string
	// RemainderSteps is the step the smaller measure of a compound plan is
	// rounded to, given per rung of that smaller measure. A remainder counted
	// out in tablespoons is a whole number of them.
	RemainderSteps        []string
	MaxRelativeError      string
	MaxTeaspoonError      string
	RelativeErrorWeight   string
	MinimumSimplicityGain string
}

var volumePresentationPolicy = VolumePresentationPolicy{
	Fractions: [][]string{
		{"1", "1/2", "1/3", "1/4", "1/8"},
		{"1", "1/2"},
		{"1", "1/2", "1/3", "1/4"},
	},
	CompoundFractions: [][]string{
		{"1"},
		{"1"},
		{"1", "1/2", "1/3", "1/4"},
	},
	RemainderSteps:        []string{"1/4", "1"},
	MaxRelativeError:      "1/12",
	MaxTeaspoonError:      "1/2",
	RelativeErrorWeight:   "30",
	MinimumSimplicityGain: "1/4",
}

// VolumePresentation returns the kitchen-friendly volume policy.
func VolumePresentation() VolumePresentationPolicy {
	fractions := make([][]string, len(volumePresentationPolicy.Fractions))
	for index, rung := range volumePresentationPolicy.Fractions {
		fractions[index] = append([]string(nil), rung...)
	}

	compound := make([][]string, len(volumePresentationPolicy.CompoundFractions))
	for index, rung := range volumePresentationPolicy.CompoundFractions {
		compound[index] = append([]string(nil), rung...)
	}

	return VolumePresentationPolicy{
		Fractions:             fractions,
		CompoundFractions:     compound,
		RemainderSteps:        append([]string(nil), volumePresentationPolicy.RemainderSteps...),
		MaxRelativeError:      volumePresentationPolicy.MaxRelativeError,
		MaxTeaspoonError:      volumePresentationPolicy.MaxTeaspoonError,
		RelativeErrorWeight:   volumePresentationPolicy.RelativeErrorWeight,
		MinimumSimplicityGain: volumePresentationPolicy.MinimumSimplicityGain,
	}
}

// rungFractions reports the measures the rung at index offers, falling back to
// the coarsest rung the policy names for a family with more rungs than the
// policy enumerates.
func rungFractions(index int) []string {
	if index < len(volumePresentationPolicy.Fractions) {
		return volumePresentationPolicy.Fractions[index]
	}

	return volumePresentationPolicy.Fractions[len(volumePresentationPolicy.Fractions)-1]
}

// compoundRungFractions reports what the larger measure of a two-measure plan
// may read as at the rung at index.
func compoundRungFractions(index int) []string {
	if index < len(volumePresentationPolicy.CompoundFractions) {
		return volumePresentationPolicy.CompoundFractions[index]
	}

	return volumePresentationPolicy.CompoundFractions[len(volumePresentationPolicy.CompoundFractions)-1]
}

// remainderStep reports the step a compound plan's smaller measure rounds to.
func remainderStep(index int) (*big.Rat, error) {
	value := volumePresentationPolicy.RemainderSteps[len(volumePresentationPolicy.RemainderSteps)-1]
	if index < len(volumePresentationPolicy.RemainderSteps) {
		value = volumePresentationPolicy.RemainderSteps[index]
	}

	step, ok := new(big.Rat).SetString(value)
	if !ok || step.Sign() <= 0 {
		return nil, fmt.Errorf("invalid volume remainder step %q", value)
	}

	return step, nil
}

// ladders is the rounding policy for each system: a converted amount is
// rounded to something a cook can measure and marked approximate when the
// rounding moved it.
var ladders = map[MeasurementSystem]map[recipe.Dimension][]Rung{
	MeasureMetric: {
		recipe.DimensionMass: {
			{Unit: "mass.gram", Ceiling: "20", Step: "1"},
			{Unit: "mass.gram", Ceiling: "100", Step: "5"},
			{Unit: "mass.gram", Ceiling: "1000", Step: "10"},
			{Unit: "mass.kilogram", Step: "1/10"},
		},
		recipe.DimensionVolume: {
			{Unit: "volume.milliliter", Ceiling: "20", Step: "1"},
			{Unit: "volume.milliliter", Ceiling: "100", Step: "5"},
			{Unit: "volume.milliliter", Ceiling: "1000", Step: "10"},
			{Unit: "volume.liter", Step: "1/10"},
		},
		recipe.DimensionLength: {
			{Unit: "length.millimeter", Ceiling: "10", Step: "1"},
			{Unit: "length.centimeter", Step: "1/2"},
		},
		recipe.DimensionTemperature: {
			{Unit: "temperature.celsius", Step: "5"},
		},
	},
	MeasureUS: {
		// Use ounces up to two pounds (32 oz) for finer resolution before transitioning to pounds.
		recipe.DimensionMass: {
			{Unit: "mass.ounce.avoirdupois", Ceiling: "32", Step: "1/4"},
			{Unit: "mass.pound.avoirdupois", Step: "1/8"},
		},
		recipe.DimensionVolume: {
			{Unit: "volume.teaspoon.us.customary", Ceiling: "4", Step: "1/4"},
			{Unit: "volume.tablespoon.us.customary", Ceiling: "4", Step: "1/2"},
			{Unit: "volume.cup.us.customary", Step: "1/8"},
		},
		recipe.DimensionLength: {
			{Unit: "length.inch.international", Step: "1/4"},
		},
		recipe.DimensionTemperature: {
			{Unit: "temperature.fahrenheit", Step: "5"},
		},
	},
	MeasureAU: {
		recipe.DimensionMass: {
			{Unit: "mass.gram", Ceiling: "20", Step: "1"},
			{Unit: "mass.gram", Ceiling: "100", Step: "5"},
			{Unit: "mass.gram", Ceiling: "1000", Step: "10"},
			{Unit: "mass.kilogram", Step: "1/10"},
		},
		recipe.DimensionVolume: {
			{Unit: "volume.teaspoon.au", Ceiling: "4", Step: "1/4"},
			{Unit: "volume.tablespoon.au", Ceiling: "4", Step: "1/2"},
			{Unit: "volume.cup.au", Step: "1/8"},
		},
		recipe.DimensionLength: {
			{Unit: "length.millimeter", Ceiling: "10", Step: "1"},
			{Unit: "length.centimeter", Step: "1/2"},
		},
		recipe.DimensionTemperature: {
			{Unit: "temperature.celsius", Step: "5"},
		},
	},
}

// Ladders reports the complete rounding policy so a client can be published
// from it rather than restating it.
func Ladders() map[MeasurementSystem]map[recipe.Dimension][]Rung {
	return ladders
}

// Presented is one amount written for a reader: the value, the unit it is
// written in, and whether reaching a measurable amount moved it.
type Presented struct {
	Amount *big.Rat
	Unit   string
	Parts  []PresentedPart
	// Approximate is true when the shown amount was rounded to a practical
	// measure, or when the registry itself calls the definition a culinary
	// approximation.
	Approximate bool
}

// PresentedPart is one measure in a practical compound plan.
type PresentedPart struct {
	Amount *big.Rat
	Unit   string
}

var errNoProjection = errors.New("amount is shown as written")

// Present writes one amount in the measures a reader asked for. An amount that
// cannot be projected, because the system is as-written, the unit has no
// physical definition, or the unit already belongs to that convention, comes
// back unchanged rather than through a conversion that would say the same
// thing less exactly.
func (c Catalog) Present(value recipe.Rational, unitID string, system MeasurementSystem) (Presented, error) {
	return c.present(value, unitID, system, system != MeasureAsWritten)
}

// PresentNormalized also normalizes an as-written amount after scaling.
func (c Catalog) PresentNormalized(value recipe.Rational, unitID string, system MeasurementSystem) (Presented, error) {
	if unitID != "" {
		if _, found := c.units[unitID]; !found {
			amount, err := value.Rat()
			if err != nil {
				return Presented{}, fmt.Errorf("parse displayed amount: %w", err)
			}

			return Presented{Amount: amount, Unit: unitID}, fmt.Errorf("%w: %s", ErrUnknownUnit, unitID)
		}
	}

	return c.present(value, unitID, system, true)
}

func (c Catalog) present(value recipe.Rational, unitID string, system MeasurementSystem, normalize bool) (Presented, error) {
	amount, err := value.Rat()
	if err != nil {
		return Presented{}, fmt.Errorf("parse displayed amount: %w", err)
	}

	unchanged := Presented{Amount: amount, Unit: unitID}
	if unitID == "" || system == MeasureAsWritten {
		if !normalize {
			return unchanged, nil
		}

		if shown, ok, err := c.presentFriendlyVolume(value, unitID, system); err != nil {
			return unchanged, err
		} else if ok {
			return shown, nil
		}

		return unchanged, nil
	}

	unit, found := c.units[unitID]
	if !found || unit.Kind != recipe.UnitPhysical {
		return unchanged, nil
	}
	// Named variants within one measuring convention are editorially the same
	// measure to a cook. Asking for US measurements must not turn 3 culinary
	// cups into an exact-but-absurd pile of tablespoons and teaspoons merely to
	// account for the small registry difference from a customary cup.
	if system == MeasureUS && strings.Contains(unitID, ".us.culinary") {
		return unchanged, nil
	}

	if shown, ok, err := c.presentFriendlyVolume(value, unitID, system); err != nil {
		return unchanged, err
	} else if ok {
		return shown, nil
	}

	rungs := ladders[system][unit.Dimension]
	if len(rungs) == 0 {
		return unchanged, nil
	}

	rung, err := c.rungFor(rungs, value, unitID)
	if err != nil {
		return unchanged, nil
	}

	converted, err := c.Convert(value, unitID, rung.Unit)
	if err != nil {
		return unchanged, nil
	}

	step, ok := new(big.Rat).SetString(rung.Step)
	if !ok || step.Sign() <= 0 {
		return unchanged, nil
	}

	exact := converted.Rat()
	rounded := roundToStep(exact, step)
	// Rounding a very small amount to zero would delete an ingredient's
	// measure, so the unrounded value survives instead.
	shown := rounded
	if rounded.Sign() == 0 {
		shown = exact
	}

	faithful := converted.Exact()

	return Presented{
		Amount:      shown,
		Unit:        rung.Unit,
		Approximate: rounded.Cmp(exact) != 0 || !faithful,
	}, nil
}

type volumeCandidate struct {
	amount *big.Rat
	unit   string
	parts  []PresentedPart
	error  *big.Rat
	score  *big.Rat
}

// presentFriendlyVolume chooses a kitchen-scale volume representation before
// a larger-volume conversion reaches the millilitre ladder.
func (c Catalog) presentFriendlyVolume(value recipe.Rational, unitID string, system MeasurementSystem) (Presented, bool, error) {
	unit, found := c.units[unitID]
	if !found || unit.Kind != recipe.UnitPhysical || unit.Dimension != recipe.DimensionVolume {
		return Presented{}, false, nil
	}

	convention := c.conventions[unitID]
	if system != MeasureAsWritten {
		convention = Convention(system)
	}

	if convention != ConventionMetric && convention != ConventionUS && convention != ConventionAU {
		return Presented{}, false, nil
	}

	units, err := c.volumeUnits(convention, unitID, system)
	if err != nil {
		return Presented{}, false, err
	}

	actual, err := c.Convert(value, unitID, units[0])
	if err != nil {
		return Presented{}, false, err
	}

	actualTsp := actual.Rat()
	// Teaspoons remain practical equipment in metric kitchens. Larger volumes
	// are clearer as mL or L, so they continue to the metric ladder below.
	if convention == ConventionMetric && actualTsp.Cmp(big.NewRat(6, 1)) > 0 {
		return Presented{}, false, nil
	}

	maxRelative, ok := new(big.Rat).SetString(volumePresentationPolicy.MaxRelativeError)
	if !ok {
		return Presented{}, false, errors.New("invalid volume maximum relative error")
	}

	if convention == ConventionMetric {
		maxRelative = big.NewRat(1, 8)
	}

	maxAbsolute, ok := new(big.Rat).SetString(volumePresentationPolicy.MaxTeaspoonError)
	if !ok {
		return Presented{}, false, errors.New("invalid volume maximum teaspoon error")
	}

	errorWeight, ok := new(big.Rat).SetString(volumePresentationPolicy.RelativeErrorWeight)
	if !ok {
		return Presented{}, false, errors.New("invalid volume relative error weight")
	}

	minimumGain, ok := new(big.Rat).SetString(volumePresentationPolicy.MinimumSimplicityGain)
	if !ok {
		return Presented{}, false, errors.New("invalid volume minimum simplicity gain")
	}

	var bestExact, bestApproximate *volumeCandidate

	if err := c.considerSimpleVolumes(value, unitID, units, actualTsp, maxAbsolute, maxRelative, errorWeight, &bestExact, &bestApproximate); err != nil {
		return Presented{}, false, err
	}

	if err := c.considerMixedVolumes(units, actualTsp, maxAbsolute, maxRelative, errorWeight, &bestExact, &bestApproximate); err != nil {
		return Presented{}, false, err
	}

	best := bestExact

	if bestApproximate != nil {
		approximateWithGain := new(big.Rat).Add(bestApproximate.score, minimumGain)
		if bestExact == nil || approximateWithGain.Cmp(bestExact.score) < 0 {
			best = bestApproximate
		}
	}

	if best != nil {
		return Presented{
			Amount:      best.amount,
			Unit:        best.unit,
			Parts:       best.parts,
			Approximate: best.error.Sign() != 0 || !actual.Exact(),
		}, true, nil
	}

	return Presented{Amount: actualTsp, Unit: units[0], Approximate: !actual.Exact()}, true, nil
}

func considerVolumeCandidate(exact, approximate **volumeCandidate, candidate volumeCandidate) {
	best := approximate
	if candidate.error.Sign() == 0 {
		best = exact
	}

	if *best == nil || candidate.score.Cmp((*best).score) < 0 || (candidate.score.Cmp((*best).score) == 0 && candidate.error.Cmp((*best).error) < 0) {
		*best = &candidate
	}
}

func (c Catalog) considerSimpleVolumes(value recipe.Rational, unitID string, units []string, actualTsp, maxAbsolute, maxRelative, errorWeight *big.Rat, bestExact, bestApproximate **volumeCandidate) error {
	for unitIndex, candidateUnit := range units {
		converted, err := c.Convert(value, unitID, candidateUnit)
		if err != nil {
			return err
		}

		for _, fraction := range rungFractions(unitIndex) {
			step, ok := new(big.Rat).SetString(fraction)
			if !ok || step.Sign() <= 0 {
				return fmt.Errorf("invalid volume presentation fraction %q", fraction)
			}

			rounded := roundToStep(converted.Rat(), step)
			if rounded.Sign() <= 0 {
				continue
			}

			inTsp, err := c.Convert(recipe.Rational(rounded.RatString()), candidateUnit, units[0])
			if err != nil {
				return err
			}

			difference := absoluteDifference(inTsp.Rat(), actualTsp)
			if difference.Cmp(maxAbsolute) > 0 || relativeError(difference, actualTsp).Cmp(maxRelative) > 0 {
				continue
			}

			relative := relativeError(difference, actualTsp)
			score := new(big.Rat).SetInt64(int64(volumeScore(rounded, unitIndex)))
			score.Add(score, new(big.Rat).Mul(relative, errorWeight))
			considerVolumeCandidate(bestExact, bestApproximate, volumeCandidate{amount: rounded, unit: candidateUnit, error: difference, score: score})
		}
	}

	return nil
}

// considerMixedVolumes offers the plans a cook would actually count out: so
// many of a larger measure and a remainder in the next smaller one. Every
// adjacent pair in the family is tried, so nine tablespoons can be half a cup
// and one tablespoon rather than nine of anything.
func (c Catalog) considerMixedVolumes(units []string, actualTsp, maxAbsolute, maxRelative, errorWeight *big.Rat, bestExact, bestApproximate **volumeCandidate) error {
	if actualTsp.Sign() <= 0 {
		return nil
	}

	for larger := 1; larger < len(units); larger++ {
		if err := c.considerCompoundPlans(units, larger, actualTsp, maxAbsolute, maxRelative, errorWeight, bestExact, bestApproximate); err != nil {
			return err
		}
	}

	return nil
}

func (c Catalog) considerCompoundPlans(units []string, larger int, actualTsp, maxAbsolute, maxRelative, errorWeight *big.Rat, bestExact, bestApproximate **volumeCandidate) error {
	smaller := larger - 1

	largerInTsp, err := c.Convert("1", units[larger], units[0])
	if err != nil {
		return err
	}

	smallerInTsp, err := c.Convert("1", units[smaller], units[0])
	if err != nil {
		return err
	}

	step, err := remainderStep(smaller)
	if err != nil {
		return err
	}

	actualInLarger := new(big.Rat).Quo(actualTsp, largerInTsp.Rat())

	for _, whole := range compoundLargerAmounts(actualInLarger, larger) {
		used := new(big.Rat).Mul(whole, largerInTsp.Rat())
		exact := new(big.Rat).Quo(new(big.Rat).Sub(actualTsp, used), smallerInTsp.Rat())
		nearest := roundToStep(exact, step)

		for _, remainder := range []*big.Rat{nearest, floorToStep(exact, step), ceilToStep(exact, step)} {
			if remainder.Sign() <= 0 || (remainder != nearest && remainder.Cmp(nearest) == 0) {
				continue
			}

			measured := new(big.Rat).Add(used, new(big.Rat).Mul(remainder, smallerInTsp.Rat()))
			difference := absoluteDifference(measured, actualTsp)
			relative := relativeError(difference, actualTsp)

			if difference.Cmp(maxAbsolute) > 0 || relative.Cmp(maxRelative) > 0 {
				continue
			}

			score := new(big.Rat).SetInt64(int64(volumeOperationScore(whole, larger) + volumeOperationScore(remainder, smaller)))
			score.Add(score, new(big.Rat).Mul(relative, errorWeight))
			considerVolumeCandidate(bestExact, bestApproximate, volumeCandidate{
				amount: actualTsp, unit: units[0], error: difference, score: score,
				parts: []PresentedPart{{Amount: whole, Unit: units[larger]}, {Amount: remainder, Unit: units[smaller]}},
			})
		}
	}

	return nil
}

// compoundLargerAmounts lists how much of the larger measure a plan might use:
// every whole number of it that fits, and the largest amount each fraction the
// rung offers as a graduated vessel can reach, so five-eighths of a cup can be
// counted as half a cup and two tablespoons.
func compoundLargerAmounts(actualInLarger *big.Rat, rung int) []*big.Rat {
	var amounts []*big.Rat

	add := func(candidate *big.Rat) {
		if candidate.Sign() <= 0 || candidate.Cmp(actualInLarger) > 0 {
			return
		}

		for _, existing := range amounts {
			if existing.Cmp(candidate) == 0 {
				return
			}
		}

		amounts = append(amounts, candidate)
	}

	whole := new(big.Int).Quo(actualInLarger.Num(), actualInLarger.Denom()).Int64()
	for count := int64(1); count <= whole && count <= 64; count++ {
		add(big.NewRat(count, 1))
	}

	for _, fraction := range compoundRungFractions(rung) {
		step, ok := new(big.Rat).SetString(fraction)
		if !ok || step.Sign() <= 0 {
			continue
		}

		add(floorToStep(actualInLarger, step))
	}

	return amounts
}

func floorToStep(value, step *big.Rat) *big.Rat {
	quotient := new(big.Rat).Quo(value, step)
	whole := new(big.Int).Quo(quotient.Num(), quotient.Denom())

	return new(big.Rat).Mul(new(big.Rat).SetInt(whole), step)
}

func ceilToStep(value, step *big.Rat) *big.Rat {
	floor := floorToStep(value, step)
	if floor.Cmp(value) == 0 {
		return floor
	}

	return new(big.Rat).Add(floor, step)
}

func (Catalog) volumeUnits(convention Convention, source string, system MeasurementSystem) ([]string, error) {
	switch convention {
	case ConventionMetric:
		return []string{"volume.teaspoon.metric.culinary"}, nil
	case ConventionAU:
		return []string{"volume.teaspoon.au", "volume.tablespoon.au", "volume.cup.au"}, nil
	case ConventionUS:
		// US legal measures are exact labeling identities, not a kitchen family.
		// Borrowing customary siblings would quietly restate a quantity as an
		// identity the recipe did not use, so each stays itself as written.
		if system == MeasureAsWritten && strings.HasSuffix(source, ".us.legal") {
			return []string{source}, nil
		}

		variant := "customary"
		if system == MeasureAsWritten && strings.HasSuffix(source, ".us.culinary") {
			variant = "culinary"
		}

		return []string{
			"volume.teaspoon.us." + variant,
			"volume.tablespoon.us." + variant,
			"volume.cup.us." + variant,
		}, nil
	case ConventionNone, ConventionImperial:
		return nil, fmt.Errorf("unsupported volume convention %q", convention)
	default:
		return nil, fmt.Errorf("unsupported volume convention %q", convention)
	}
}

func absoluteDifference(left, right *big.Rat) *big.Rat {
	result := new(big.Rat).Sub(left, right)
	return result.Abs(result)
}

func relativeError(difference, value *big.Rat) *big.Rat {
	if value.Sign() == 0 {
		return new(big.Rat)
	}

	return new(big.Rat).Quo(difference, new(big.Rat).Abs(value))
}

func volumeScore(value *big.Rat, unitIndex int) int {
	return volumeOperationScore(value, unitIndex)
}

func volumeOperationScore(value *big.Rat, unitIndex int) int {
	magnitude := new(big.Rat).Abs(value)
	whole := new(big.Int).Quo(magnitude.Num(), magnitude.Denom())
	score := int(whole.Int64())

	denominator := magnitude.Denom()
	if denominator.Cmp(big.NewInt(1)) == 0 {
		return score
	}

	if unitIndex == 0 {
		switch denominator.Int64() {
		case 2:
			return score + 1
		case 4:
			if new(big.Int).Mod(magnitude.Num(), denominator).Int64() == 3 {
				return score + 2
			}

			return score + 1
		case 3:
			return score + 4
		default:
			return score + 4
		}
	}

	if unitIndex == 1 {
		if denominator.Int64() == 2 {
			if score == 0 {
				return 2
			}

			return score + 1
		}

		return score + 5
	}

	if unitIndex == 2 {
		switch denominator.Int64() {
		case 2, 3, 4:
			return score + 1
		default:
			return score + 5
		}
	}

	return score
}

// rungFor chooses the step a converted amount belongs on, comparing in each
// rung's own measure so the ladder never has to know a conversion factor.
func (c Catalog) rungFor(rungs []Rung, value recipe.Rational, unitID string) (Rung, error) {
	for _, rung := range rungs {
		if rung.Ceiling == "" {
			return rung, nil
		}

		converted, err := c.Convert(value, unitID, rung.Unit)
		if err != nil {
			return Rung{}, err
		}

		ceiling, ok := new(big.Rat).SetString(rung.Ceiling)
		if !ok {
			return Rung{}, fmt.Errorf("rounding ladder ceiling %q is not a rational", rung.Ceiling)
		}

		if converted.Rat().Cmp(ceiling) < 0 {
			return rung, nil
		}
	}

	if len(rungs) == 0 {
		return Rung{}, errNoProjection
	}

	return rungs[len(rungs)-1], nil
}

// roundToStep rounds to the nearest whole number of steps, away from zero on a
// tie, which is how a cook reads a measuring spoon.
func roundToStep(value, step *big.Rat) *big.Rat {
	quotient := new(big.Rat).Quo(value, step)
	denominator := quotient.Denom()
	doubled := new(big.Int).Lsh(quotient.Num(), 1)
	negative := doubled.Sign() < 0
	magnitude := new(big.Int).Abs(doubled)
	whole := new(big.Int).Add(magnitude, denominator)
	whole.Quo(whole, new(big.Int).Lsh(denominator, 1))

	if negative {
		whole.Neg(whole)
	}

	return new(big.Rat).Mul(new(big.Rat).SetInt(whole), step)
}

// MeasurementConventionOf reports the convention a set of volume units agrees
// on, which is how a recipe states once that it was written in United States
// or Australian measures instead of repeating it on every line.
func (c Catalog) MeasurementConventionOf(unitIDs []string) Convention {
	found := ConventionNone

	for _, id := range unitIDs {
		unit, known := c.units[id]
		if !known || unit.Dimension != recipe.DimensionVolume {
			continue
		}

		convention := c.conventions[id]
		if convention == ConventionNone {
			continue
		}

		if found != ConventionNone && found != convention {
			return ConventionNone
		}

		found = convention
	}

	return found
}
