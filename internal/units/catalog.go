package units

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sync"

	"github.com/savorum/savorum/internal/recipe"
)

var unitIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`)

// Conversion errors distinguish unknown, incompatible, and invalid quantities.
var (
	// ErrUnknownUnit reports a unit identity absent from the catalog.
	ErrUnknownUnit       = errors.New("unknown unit")
	ErrNotPhysical       = errors.New("unit is not physically convertible")
	ErrDimensionMismatch = errors.New("unit dimensions do not match")
	ErrNegativeAmount    = errors.New("non-temperature amount cannot be negative")
)

// DefinitionKind describes how a unit relates to its dimensional base unit.
type DefinitionKind string

// Definition kinds describe how each unit relates to its dimensional base.
const (
	// DefinitionNone identifies a non-physical unit with no conversion definition.
	DefinitionNone   DefinitionKind = "none"
	DefinitionBase   DefinitionKind = "base"
	DefinitionLinear DefinitionKind = "linear"
	DefinitionAffine DefinitionKind = "affine"
)

// Convention names the culinary measuring convention a unit belongs to, which
// authoring and display depend on. Conversion never consults it, and it is not
// a geography: Australia measures metrically yet keeps a 250 mL cup.
type Convention string

// Conventions identify the culinary measuring systems used for display.
const (
	// ConventionNone identifies a unit with no measuring convention.
	ConventionNone   Convention = ""
	ConventionMetric Convention = "metric"
	ConventionUS     Convention = "us"
	ConventionAU     Convention = "au"
	// ConventionImperial covers the British imperial volumes, which are
	// neither US nor metric. It is not offered as a display projection; it
	// preserves an imperial original as what it is.
	ConventionImperial Convention = "imperial"
)

// Definition gives a unit an exact transformation to its dimensional base.
// Base value = source value × Multiplier + Offset. Exact is false when the
// registry definition itself is a culinary approximation.
type Definition struct {
	Unit       recipe.Unit
	Convention Convention
	Kind       DefinitionKind
	BaseUnit   string
	Multiplier string
	Offset     string
	Exact      bool
}

// Entry is one registry record: a unit's identity, the convention it belongs
// to, and its resolved transformation to the dimensional base. Multiplier and
// Offset are canonical rationals regardless of how the definition spelled
// them, so implementations outside Go can reproduce the same conversion.
type Entry struct {
	Unit       recipe.Unit
	Convention Convention
	BaseUnit   string
	Multiplier string
	Offset     string
	Exact      bool
}

type normalizedDefinition struct {
	multiplier *big.Rat
	offset     *big.Rat
	exact      bool
}

// Catalog resolves registered units, aliases, conventions, and conversions.
type Catalog struct {
	units       map[string]recipe.Unit
	definitions map[string]normalizedDefinition
	// conventions records the measuring convention each identity belongs to,
	// which authoring and display depend on and conversion never consults.
	conventions map[string]Convention
	entries     []Entry
	aliasSets   []AliasSet
	// aliasExact and aliasFolded index every registered form by the text
	// matchKey compares: case-sensitive sets exactly, the others case-folded.
	aliasExact  map[string][]aliasRef
	aliasFolded map[string][]aliasRef
	// profiles expands each measuring convention into the exact identities it
	// selects, so an ingredient-wide decision resolves through a registry
	// mapping rather than through the word a reader was shown.
	profiles []ConventionProfile
}

// NewCatalog validates unit definitions and builds conversion indexes.
func NewCatalog(definitions []Definition) (Catalog, error) {
	catalog := Catalog{
		units:       make(map[string]recipe.Unit, len(definitions)),
		definitions: make(map[string]normalizedDefinition, len(definitions)),
		conventions: make(map[string]Convention, len(definitions)),
	}
	indexed := make(map[string]Definition, len(definitions))
	bases := make(map[recipe.Dimension]string)

	for _, definition := range definitions {
		if !unitIDPattern.MatchString(definition.Unit.ID) {
			return Catalog{}, fmt.Errorf("unit ID %q is invalid", definition.Unit.ID)
		}

		if _, duplicate := indexed[definition.Unit.ID]; duplicate {
			return Catalog{}, fmt.Errorf("unit %q is duplicated", definition.Unit.ID)
		}

		indexed[definition.Unit.ID] = definition
		catalog.units[definition.Unit.ID] = definition.Unit

		catalog.conventions[definition.Unit.ID] = definition.Convention
		switch definition.Unit.Kind {
		case recipe.UnitPhysical:
			switch definition.Unit.Dimension {
			case recipe.DimensionMass, recipe.DimensionVolume, recipe.DimensionTemperature, recipe.DimensionLength:
			default:
				return Catalog{}, fmt.Errorf("unit %q has unsupported dimension %q", definition.Unit.ID, definition.Unit.Dimension)
			}
		case recipe.UnitCount, recipe.UnitContainer:
		default:
			return Catalog{}, fmt.Errorf("unit %q has unsupported kind %q", definition.Unit.ID, definition.Unit.Kind)
		}

		switch definition.Convention {
		case ConventionNone, ConventionMetric, ConventionUS, ConventionAU, ConventionImperial:
		default:
			return Catalog{}, fmt.Errorf("unit %q has unsupported convention %q", definition.Unit.ID, definition.Convention)
		}

		if definition.Unit.Kind == recipe.UnitPhysical && definition.Convention == ConventionNone {
			return Catalog{}, fmt.Errorf("physical unit %q must declare a measuring convention", definition.Unit.ID)
		}

		if definition.Kind == DefinitionBase {
			if existing, duplicate := bases[definition.Unit.Dimension]; duplicate {
				return Catalog{}, fmt.Errorf("dimension %q has multiple base units %q and %q", definition.Unit.Dimension, existing, definition.Unit.ID)
			}

			bases[definition.Unit.Dimension] = definition.Unit.ID
		}
	}

	catalog.entries = make([]Entry, 0, len(definitions))
	for _, definition := range definitions {
		normalized, err := normalizeDefinition(definition, indexed)
		if err != nil {
			return Catalog{}, fmt.Errorf("unit %q: %w", definition.Unit.ID, err)
		}

		catalog.definitions[definition.Unit.ID] = normalized
		catalog.entries = append(catalog.entries, entryOf(definition, normalized))
	}

	return catalog, nil
}

// entryOf states a definition in the canonical form the registry publishes.
// A base unit reports the identity transformation rather than nothing, so a
// consumer never has to special-case which unit a dimension is measured in.
func entryOf(definition Definition, normalized normalizedDefinition) Entry {
	entry := Entry{Unit: definition.Unit, Convention: definition.Convention}
	if definition.Unit.Kind != recipe.UnitPhysical {
		return entry
	}

	entry.BaseUnit = definition.BaseUnit
	if definition.Kind == DefinitionBase {
		entry.BaseUnit = definition.Unit.ID
	}

	entry.Multiplier = normalized.multiplier.RatString()
	entry.Offset = normalized.offset.RatString()
	entry.Exact = normalized.exact

	return entry
}

// Entries reports the whole registry in declaration order, for publishing to
// other consumers.
func (c Catalog) Entries() []Entry {
	return append([]Entry(nil), c.entries...)
}

// Lookup returns the registered unit with the given identity.
func (c Catalog) Lookup(id string) (recipe.Unit, bool) {
	unit, found := c.units[id]
	return unit, found
}

// ConventionOf reports the measuring convention attached to one registered
// unit. Presentation uses it to prefer an authored alternative already written
// in the reader's requested system before converting another equivalent.
func (c Catalog) ConventionOf(id string) (Convention, bool) {
	if _, found := c.units[id]; !found {
		return ConventionNone, false
	}

	return c.conventions[id], true
}

// Conversion is the exact result of converting between compatible units.
type Conversion struct {
	value *big.Rat
	exact bool
}

func (conversion Conversion) String() string {
	if conversion.value == nil {
		return ""
	}

	return conversion.value.RatString()
}

// Rat returns a copy of the converted rational value.
func (conversion Conversion) Rat() *big.Rat {
	if conversion.value == nil {
		return nil
	}

	return new(big.Rat).Set(conversion.value)
}

// Exact reports whether the registry defines the conversion as exact.
func (conversion Conversion) Exact() bool {
	return conversion.exact
}

// Convert performs only same-dimension physical conversion. Ingredient-density
// transformations are intentionally outside this service.
func (c Catalog) Convert(value recipe.Rational, fromID, toID string) (Conversion, error) {
	from, found := c.units[fromID]
	if !found {
		return Conversion{}, fmt.Errorf("%w: %s", ErrUnknownUnit, fromID)
	}

	to, found := c.units[toID]
	if !found {
		return Conversion{}, fmt.Errorf("%w: %s", ErrUnknownUnit, toID)
	}

	if from.Kind != recipe.UnitPhysical || to.Kind != recipe.UnitPhysical {
		return Conversion{}, fmt.Errorf("%w: %s to %s", ErrNotPhysical, fromID, toID)
	}

	if from.Dimension != to.Dimension {
		return Conversion{}, fmt.Errorf("%w: %s to %s", ErrDimensionMismatch, from.Dimension, to.Dimension)
	}

	input, err := value.Rat()
	if err != nil {
		return Conversion{}, fmt.Errorf("parse conversion value: %w", err)
	}

	if from.Dimension != recipe.DimensionTemperature && input.Sign() < 0 {
		return Conversion{}, ErrNegativeAmount
	}

	if fromID == toID {
		return Conversion{value: input, exact: true}, nil
	}

	fromDefinition := c.definitions[fromID]
	toDefinition := c.definitions[toID]
	base := new(big.Rat).Mul(input, fromDefinition.multiplier)
	base.Add(base, fromDefinition.offset)
	result := new(big.Rat).Sub(base, toDefinition.offset)
	result.Quo(result, toDefinition.multiplier)

	return Conversion{value: result, exact: fromDefinition.exact && toDefinition.exact}, nil
}

func normalizeDefinition(definition Definition, indexed map[string]Definition) (normalizedDefinition, error) {
	unit := definition.Unit
	if unit.Kind != recipe.UnitPhysical {
		if definition.Kind != DefinitionNone || definition.BaseUnit != "" || definition.Multiplier != "" || definition.Offset != "" {
			return normalizedDefinition{}, errors.New("nonphysical unit has a conversion definition")
		}

		if unit.Dimension != "" {
			return normalizedDefinition{}, errors.New("nonphysical unit has a physical dimension")
		}

		return normalizedDefinition{}, nil
	}

	if unit.Dimension == "" {
		return normalizedDefinition{}, errors.New("physical unit requires a dimension")
	}

	switch definition.Kind {
	case DefinitionBase:
		if definition.BaseUnit != "" || definition.Multiplier != "" || definition.Offset != "" {
			return normalizedDefinition{}, errors.New("base unit cannot define a transformation")
		}

		return normalizedDefinition{multiplier: big.NewRat(1, 1), offset: big.NewRat(0, 1), exact: true}, nil
	case DefinitionLinear, DefinitionAffine:
		base, found := indexed[definition.BaseUnit]
		if !found {
			return normalizedDefinition{}, fmt.Errorf("base unit %q is not registered", definition.BaseUnit)
		}

		if base.Kind != DefinitionBase || base.Unit.Kind != recipe.UnitPhysical || base.Unit.Dimension != unit.Dimension {
			return normalizedDefinition{}, fmt.Errorf("base unit %q is not the base for dimension %q", definition.BaseUnit, unit.Dimension)
		}

		multiplier, err := parsePositive(definition.Multiplier)
		if err != nil {
			return normalizedDefinition{}, fmt.Errorf("invalid multiplier: %w", err)
		}

		offset, err := definitionOffset(definition)
		if err != nil {
			return normalizedDefinition{}, err
		}

		return normalizedDefinition{multiplier: multiplier, offset: offset, exact: definition.Exact}, nil
	case DefinitionNone:
		return normalizedDefinition{}, fmt.Errorf("unsupported definition kind %q", definition.Kind)
	default:
		return normalizedDefinition{}, fmt.Errorf("unsupported definition kind %q", definition.Kind)
	}
}

func definitionOffset(definition Definition) (*big.Rat, error) {
	if definition.Kind == DefinitionLinear {
		if definition.Offset != "" {
			return nil, errors.New("linear unit cannot define an offset")
		}

		return big.NewRat(0, 1), nil
	}

	if definition.Unit.Dimension != recipe.DimensionTemperature {
		return nil, errors.New("affine conversion is restricted to temperature")
	}

	if definition.Offset == "" {
		return nil, errors.New("affine unit requires an offset")
	}

	parsed, err := recipe.ParseSignedRational(definition.Offset)
	if err != nil {
		return nil, fmt.Errorf("invalid offset: %w", err)
	}

	offset, err := parsed.Rat()
	if err != nil {
		return nil, fmt.Errorf("parse offset: %w", err)
	}

	return offset, nil
}

func parsePositive(value string) (*big.Rat, error) {
	parsed, err := recipe.ParseRational(value)
	if err != nil {
		return nil, err
	}

	rational, err := parsed.Rat()
	if err != nil {
		return nil, err
	}

	if rational.Sign() <= 0 {
		return nil, errors.New("value must be positive")
	}

	return rational, nil
}

// DefaultCatalog contains the standard unit identities and physical
// definitions understood by the domain model. Once emitted, an identity must
// remain known here or in a schema-specific upcaster; obsolete identities are
// deprecated rather than removed or reassigned. Labels and formatting policy
// remain separate from physical conversion.
//
// The catalog is built once and is read-only, so every caller shares it.
func DefaultCatalog() Catalog {
	return defaultCatalog()
}

var defaultCatalog = sync.OnceValue(buildDefaultCatalog)

func buildDefaultCatalog() Catalog {
	physical := func(id string, dimension recipe.Dimension, convention Convention, kind DefinitionKind, base, multiplier, offset string, exact bool) Definition {
		return Definition{Unit: recipe.Unit{ID: id, Kind: recipe.UnitPhysical, Dimension: dimension}, Convention: convention, Kind: kind, BaseUnit: base, Multiplier: multiplier, Offset: offset, Exact: exact}
	}
	nonphysical := func(id string, kind recipe.UnitKind) Definition {
		return Definition{Unit: recipe.Unit{ID: id, Kind: kind}, Kind: DefinitionNone}
	}

	catalog, err := NewCatalog([]Definition{
		physical("mass.gram", recipe.DimensionMass, ConventionMetric, DefinitionBase, "", "", "", true),
		physical("mass.kilogram", recipe.DimensionMass, ConventionMetric, DefinitionLinear, "mass.gram", "1000", "", true),
		physical("mass.ounce.avoirdupois", recipe.DimensionMass, ConventionUS, DefinitionLinear, "mass.gram", "45359237/1600000", "", true),
		physical("mass.pound.avoirdupois", recipe.DimensionMass, ConventionUS, DefinitionLinear, "mass.gram", "45359237/100000", "", true),
		physical("volume.milliliter", recipe.DimensionVolume, ConventionMetric, DefinitionBase, "", "", "", true),
		physical("volume.liter", recipe.DimensionVolume, ConventionMetric, DefinitionLinear, "volume.milliliter", "1000", "", true),
		physical("volume.deciliter", recipe.DimensionVolume, ConventionMetric, DefinitionLinear, "volume.milliliter", "100", "", true),
		physical("volume.centiliter", recipe.DimensionVolume, ConventionMetric, DefinitionLinear, "volume.milliliter", "10", "", true),
		physical("volume.teaspoon.us.legal", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "5", "", true),
		physical("volume.teaspoon.metric.culinary", recipe.DimensionVolume, ConventionMetric, DefinitionLinear, "volume.milliliter", "5", "", true),
		physical("volume.tablespoon.us.legal", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "15", "", true),
		physical("volume.tablespoon.metric.culinary", recipe.DimensionVolume, ConventionMetric, DefinitionLinear, "volume.milliliter", "15", "", true),
		physical("volume.cup.metric.culinary", recipe.DimensionVolume, ConventionMetric, DefinitionLinear, "volume.milliliter", "250", "", true),
		physical("volume.cup.us.customary", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "473176473/2000000", "", true),
		physical("volume.tablespoon.us.customary", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "473176473/32000000", "", true),
		physical("volume.teaspoon.us.customary", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "157725491/32000000", "", true),
		physical("volume.cup.us.legal", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "240", "", true),
		physical("volume.fluidounce.us", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "473176473/16000000", "", true),
		physical("volume.pint.us.liquid", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "473176473/1000000", "", true),
		physical("volume.quart.us.liquid", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "473176473/500000", "", true),
		physical("volume.gallon.us.liquid", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "473176473/125000", "", true),
		physical("volume.cup.us.culinary", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "240", "", false),
		physical("volume.tablespoon.us.culinary", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "15", "", false),
		physical("volume.teaspoon.us.culinary", recipe.DimensionVolume, ConventionUS, DefinitionLinear, "volume.milliliter", "5", "", false),
		physical("volume.cup.au", recipe.DimensionVolume, ConventionAU, DefinitionLinear, "volume.milliliter", "250", "", true),
		physical("volume.tablespoon.au", recipe.DimensionVolume, ConventionAU, DefinitionLinear, "volume.milliliter", "20", "", true),
		physical("volume.teaspoon.au", recipe.DimensionVolume, ConventionAU, DefinitionLinear, "volume.milliliter", "5", "", true),
		physical("volume.cup.jp", recipe.DimensionVolume, ConventionMetric, DefinitionLinear, "volume.milliliter", "200", "", true),
		physical("volume.cup.kr", recipe.DimensionVolume, ConventionMetric, DefinitionLinear, "volume.milliliter", "200", "", true),
		physical("volume.fluidounce.imperial", recipe.DimensionVolume, ConventionImperial, DefinitionLinear, "volume.milliliter", "454609/16000", "", true),
		physical("volume.pint.imperial", recipe.DimensionVolume, ConventionImperial, DefinitionLinear, "volume.milliliter", "454609/800", "", true),
		physical("volume.quart.imperial", recipe.DimensionVolume, ConventionImperial, DefinitionLinear, "volume.milliliter", "454609/400", "", true),
		physical("volume.gallon.imperial", recipe.DimensionVolume, ConventionImperial, DefinitionLinear, "volume.milliliter", "454609/100", "", true),
		physical("length.millimeter", recipe.DimensionLength, ConventionMetric, DefinitionBase, "", "", "", true),
		physical("length.centimeter", recipe.DimensionLength, ConventionMetric, DefinitionLinear, "length.millimeter", "10", "", true),
		physical("length.inch.international", recipe.DimensionLength, ConventionUS, DefinitionLinear, "length.millimeter", "127/5", "", true),
		physical("temperature.celsius", recipe.DimensionTemperature, ConventionMetric, DefinitionBase, "", "", "", true),
		physical("temperature.fahrenheit", recipe.DimensionTemperature, ConventionUS, DefinitionAffine, "temperature.celsius", "5/9", "-160/9", true),
		nonphysical("count.item", recipe.UnitCount),
		nonphysical("count.serving", recipe.UnitCount),
		nonphysical("count.egg", recipe.UnitCount),
		nonphysical("count.clove", recipe.UnitCount),
		nonphysical("count.sprig", recipe.UnitCount),
		nonphysical("count.bunch", recipe.UnitCount),
		nonphysical("count.pinch", recipe.UnitCount),
		nonphysical("count.handful", recipe.UnitCount),
		nonphysical("container.can", recipe.UnitContainer),
		nonphysical("container.tin", recipe.UnitContainer),
		nonphysical("container.packet", recipe.UnitContainer),
	})
	if err != nil {
		panic("invalid built-in unit catalog: " + err.Error())
	}

	if err := registerAliasSets(&catalog, defaultAliasSets()); err != nil {
		panic("invalid built-in unit aliases: " + err.Error())
	}

	if err := registerConventionProfiles(&catalog, defaultConventionProfiles()); err != nil {
		panic("invalid built-in convention profiles: " + err.Error())
	}

	return catalog
}
