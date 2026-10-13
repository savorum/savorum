import assert from "node:assert/strict"
import test from "node:test"

import type { Recipe } from "@/api/client"

import { formatRational, rational } from "./rational.ts"
import { cookNumber } from "./recipe-format.ts"
import { recipeMeasurementRegion } from "./recipe-format.ts"
import { unitGroups } from "./unit-choices.ts"
import { unitRegistry } from "./unit-registry.generated.ts"
import {
  convertAmount,
  isRegionallyAmbiguous,
  knownUnits,
  measurementConvention,
  unitDetail,
  unitLabel,
} from "./units.ts"

test("the presentation catalog covers exactly the registry the server publishes", () => {
  const registry = unitRegistry.map((entry) => entry.id)
  assert.deepEqual([...knownUnits].sort(), [...registry].sort())
  for (const id of registry) {
    assert.notEqual(unitLabel(id, rational(2n)), id, `${id} is shown as its identifier`)
  }
})

test("an unregistered identity is shown as itself rather than guessed at", () => {
  assert.equal(
    unitLabel("future.length.international", rational(2n)),
    "future.length.international",
  )
  assert.equal(convertAmount(rational(1n), "future.length.international", "metric"), undefined)
})

test("the measures an editor offers are all identities the server accepts", () => {
  const registry = new Set<string>(unitRegistry.map((entry) => entry.id))
  for (const style of ["metric", "us", "au"] as const) {
    for (const group of unitGroups(style)) {
      for (const choice of group.choices) {
        assert.ok(registry.has(choice.value), `${choice.value} is not in the registry`)
      }
    }
  }
})

test("conversion reproduces the registry definitions exactly", () => {
  const cases: {
    amount: [bigint, bigint]
    from: string
    to: "metric" | "us"
    expected: string
    unit: string
  }[] = [
    // One US customary cup is 473176473/2000000 mL, rounded to the nearest 10.
    {
      amount: [1n, 1n],
      from: "volume.cup.us.customary",
      to: "metric",
      expected: "240",
      unit: "volume.milliliter",
    },
    {
      amount: [1n, 1n],
      from: "volume.cup.au",
      to: "metric",
      expected: "250",
      unit: "volume.milliliter",
    },
    {
      amount: [350n, 1n],
      from: "temperature.fahrenheit",
      to: "metric",
      expected: "175",
      unit: "temperature.celsius",
    },
    // Below two pounds the ladder stays in ounces, because pounds round too
    // coarsely to describe a baking weight honestly.
    {
      amount: [500n, 1n],
      from: "mass.gram",
      to: "us",
      expected: "71/4",
      unit: "mass.ounce.avoirdupois",
    },
    {
      amount: [1n, 1n],
      from: "mass.kilogram",
      to: "us",
      expected: "9/4",
      unit: "mass.pound.avoirdupois",
    },
    {
      amount: [2n, 1n],
      from: "length.inch.international",
      to: "metric",
      expected: "5",
      unit: "length.centimeter",
    },
  ]
  for (const entry of cases) {
    const converted = convertAmount(
      rational(entry.amount[0], entry.amount[1]),
      entry.from,
      entry.to,
    )
    assert.ok(converted, `${entry.from} did not convert`)
    assert.equal(formatRational(converted.amount), entry.expected, entry.from)
    assert.equal(converted.unit, entry.unit, entry.from)
  }
})

test("an exact metric amount is not marked approximate", () => {
  const converted = convertAmount(rational(250n), "volume.milliliter", "metric")
  assert.deepEqual(converted, {
    amount: rational(250n),
    unit: "volume.milliliter",
    approximate: false,
  })
  const litre = convertAmount(rational(1n), "volume.liter", "us")
  assert.ok(litre)
  assert.equal(litre.approximate, true)
})

test("a definition the registry calls a culinary approximation converts approximately", () => {
  // 240 mL exactly, yet the US culinary cup is itself a rounded convention, so
  // the projection is honest about that rather than claiming precision.
  const converted = convertAmount(rational(1n), "volume.cup.us.culinary", "metric")
  assert.ok(converted)
  assert.equal(formatRational(converted.amount), "240")
  assert.equal(converted.approximate, true)
})

test("a recipe already written in the reader’s measures is left alone", () => {
  const converted = convertAmount(rational(1n), "volume.cup.au", "original")
  assert.deepEqual(converted, {
    amount: rational(1n),
    unit: "volume.cup.au",
    approximate: false,
  })
})

test("a count scales without being converted", () => {
  const converted = convertAmount(rational(3n), "count.clove", "metric")
  assert.deepEqual(converted, {
    amount: rational(3n),
    unit: "count.clove",
    approximate: false,
  })
})

test("the measuring convention comes from the registry, not from the label", () => {
  assert.equal(measurementConvention(["volume.cup.au", "volume.tablespoon.au"]), "au")
  assert.equal(measurementConvention(["volume.cup.au", "volume.cup.us.customary"]), undefined)
  assert.equal(measurementConvention(["mass.gram"]), undefined)
  assert.equal(unitDetail("volume.cup.au"), "Australian cups")
})

test("US volume presentation favors familiar fractions after exact scaling", () => {
  const cases: {
    amount: [bigint, bigint]
    expected: string
    unit: string
    approximate: boolean
  }[] = [
    {
      amount: [15n, 16n],
      expected: "1",
      unit: "volume.teaspoon.us.customary",
      approximate: true,
    },
    {
      amount: [5n, 16n],
      expected: "1/3",
      unit: "volume.teaspoon.us.customary",
      approximate: true,
    },
    {
      amount: [5n, 8n],
      expected: "5/8",
      unit: "volume.teaspoon.us.customary",
      approximate: false,
    },
    {
      amount: [5n, 4n],
      expected: "5/4",
      unit: "volume.teaspoon.us.customary",
      approximate: false,
    },
  ]
  for (const entry of cases) {
    const shown = convertAmount(
      rational(...entry.amount),
      "volume.teaspoon.us.customary",
      "original",
      true,
    )
    assert.ok(shown)
    assert.equal(formatRational(shown.amount), entry.expected)
    assert.equal(shown.unit, entry.unit)
    assert.equal(shown.approximate, entry.approximate)
  }
})

test("US volume presentation chooses a simpler equivalent unit", () => {
  const cases: {
    amount: [bigint, bigint?]
    expected: string
    unit: string
    approximate?: boolean
  }[] = [
    { amount: [3n], expected: "1", unit: "volume.tablespoon.us.customary" },
    { amount: [4n], expected: "4", unit: "volume.teaspoon.us.customary" },
    { amount: [5n], expected: "5", unit: "volume.teaspoon.us.customary" },
    { amount: [6n], expected: "2", unit: "volume.tablespoon.us.customary" },
    {
      amount: [19n, 4n],
      expected: "3/2",
      unit: "volume.tablespoon.us.customary",
      approximate: true,
    },
    {
      amount: [19n, 8n],
      expected: "5/2",
      unit: "volume.teaspoon.us.customary",
      approximate: true,
    },
    {
      amount: [13n, 8n],
      expected: "3/2",
      unit: "volume.teaspoon.us.customary",
      approximate: true,
    },
    { amount: [12n], expected: "1/4", unit: "volume.cup.us.customary" },
    { amount: [24n], expected: "1/2", unit: "volume.cup.us.customary" },
  ]
  for (const entry of cases) {
    const shown = convertAmount(
      rational(entry.amount[0], entry.amount[1]),
      "volume.teaspoon.us.customary",
      "original",
      true,
    )
    assert.ok(shown)
    assert.equal(formatRational(shown.amount), entry.expected)
    assert.equal(shown.unit, entry.unit)
    assert.equal(shown.approximate, entry.approximate ?? false)
  }

  const oil = convertAmount(rational(19n, 2n), "volume.teaspoon.us.customary", "original", true)
  assert.deepEqual(oil, {
    amount: rational(19n, 2n),
    unit: "volume.teaspoon.us.customary",
    parts: [
      { amount: rational(3n), unit: "volume.tablespoon.us.customary" },
      { amount: rational(1n, 2n), unit: "volume.teaspoon.us.customary" },
    ],
    approximate: false,
  })

  const salt = convertAmount(rational(19n, 8n), "volume.tablespoon.us.customary", "original", true)
  assert.deepEqual(salt, {
    amount: rational(57n, 8n),
    unit: "volume.teaspoon.us.customary",
    parts: [
      { amount: rational(2n), unit: "volume.tablespoon.us.customary" },
      { amount: rational(1n), unit: "volume.teaspoon.us.customary" },
    ],
    approximate: true,
  })

  assert.deepEqual(convertAmount(rational(13n, 8n), "volume.teaspoon.us.customary", "original"), {
    amount: rational(13n, 8n),
    unit: "volume.teaspoon.us.customary",
    approximate: false,
  })

  assert.deepEqual(convertAmount(rational(9n), "volume.teaspoon.au", "original", true)?.parts, [
    { amount: rational(2n), unit: "volume.tablespoon.au" },
    { amount: rational(1n), unit: "volume.teaspoon.au" },
  ])
})

test("US preference keeps an authored US cup instead of expanding it into spoons", () => {
  const shown = convertAmount(rational(3n), "volume.cup.us.culinary", "us")
  assert.deepEqual(shown, {
    amount: rational(3n),
    unit: "volume.cup.us.culinary",
    approximate: false,
  })
})

test("US legal measures remain their exact identity when normalized as written", () => {
  for (const unit of [
    "volume.cup.us.legal",
    "volume.tablespoon.us.legal",
    "volume.teaspoon.us.legal",
  ]) {
    assert.deepEqual(convertAmount(rational(2n), unit, "original", true), {
      amount: rational(2n),
      unit,
      parts: undefined,
      approximate: false,
    })
  }
})

test("metric presentation keeps spoon-scale volumes in practical spoons", () => {
  const cases: { amount: [bigint, bigint]; expected: string }[] = [
    { amount: [5n, 4n], expected: "5/4" },
    { amount: [15n, 16n], expected: "1" },
    { amount: [5n, 8n], expected: "5/8" },
    { amount: [5n, 16n], expected: "1/3" },
  ]
  for (const entry of cases) {
    const shown = convertAmount(
      rational(...entry.amount),
      "volume.teaspoon.us.customary",
      "metric",
      true,
    )
    assert.ok(shown)
    assert.equal(formatRational(shown.amount), entry.expected)
    assert.equal(shown.unit, "volume.teaspoon.metric.culinary")
  }
})

test("Australian presentation keeps a precise spoon fraction when simplification is too lossy", () => {
  const cases: { amount: [bigint, bigint]; expected: string }[] = [
    { amount: [5n, 4n], expected: "5/4" },
    { amount: [15n, 16n], expected: "1" },
    { amount: [5n, 8n], expected: "5/8" },
    { amount: [5n, 16n], expected: "1/3" },
  ]
  for (const entry of cases) {
    const shown = convertAmount(
      rational(...entry.amount),
      "volume.teaspoon.us.customary",
      "au",
      true,
    )
    assert.ok(shown)
    assert.equal(formatRational(shown.amount), entry.expected)
    assert.equal(shown.unit, "volume.teaspoon.au")
  }
})

test("measurement plans choose exact equal-operation candidates and respect regional tablespoon volume", () => {
  const exact = convertAmount(rational(25n, 4n), "volume.teaspoon.us.customary", "us", true)
  assert.deepEqual(exact?.parts, [
    { amount: rational(2n), unit: "volume.tablespoon.us.customary" },
    { amount: rational(1n, 4n), unit: "volume.teaspoon.us.customary" },
  ])
  assert.equal(exact?.approximate, false)

  const simpler = convertAmount(rational(25n, 8n), "volume.teaspoon.us.customary", "us", true)
  assert.equal(simpler?.amount.numerator, 1n)
  assert.equal(simpler?.amount.denominator, 1n)
  assert.equal(simpler?.unit, "volume.tablespoon.us.customary")
  assert.equal(simpler?.approximate, true)

  const australian = convertAmount(rational(3n), "volume.tablespoon.us.customary", "au", true)
  assert.deepEqual(australian?.parts, [
    { amount: rational(2n), unit: "volume.tablespoon.au" },
    { amount: rational(1n), unit: "volume.teaspoon.au" },
  ])
})

// Scaled volumes use measures a kitchen owns, not three-eighths of a cup.
// internal/units/display_test.go holds the same cases for the server.
test("a scaled volume is counted out in measures a kitchen has", () => {
  const cases: { teaspoons: bigint; parts: string[] }[] = [
    { teaspoons: 18n, parts: ["1/4 volume.cup.us.customary", "2 volume.tablespoon.us.customary"] },
    { teaspoons: 27n, parts: ["1/2 volume.cup.us.customary", "1 volume.tablespoon.us.customary"] },
    { teaspoons: 30n, parts: ["1/2 volume.cup.us.customary", "2 volume.tablespoon.us.customary"] },
  ]
  for (const entry of cases) {
    const shown = convertAmount(
      rational(entry.teaspoons),
      "volume.teaspoon.us.customary",
      "original",
      true,
    )
    assert.ok(shown)
    assert.deepEqual(
      (shown.parts ?? []).map((part) => `${formatRational(part.amount)} ${part.unit}`),
      entry.parts,
    )
  }
})

test("a tenth-litre rounding reads as a decimal rather than an invented fifth", () => {
  const shown = convertAmount(rational(6n), "volume.cup.us.customary", "metric")
  assert.ok(shown)
  assert.equal(cookNumber(shown.amount), "1.4")
  assert.equal(shown.unit, "volume.liter")
})

test("a recipe with a measure nobody resolved states no convention", () => {
  // With any measure unresolved, the region is unknown; metric is never the
  // fallback.
  const recipe: Pick<Recipe, "ingredient_sections"> = {
    ingredient_sections: [
      {
        id: "main",
        ingredients: [
          {
            id: "salt",
            name: "salt",
            quantity: {
              kind: "simple",
              amount: { value: "1" },
              unit: "volume.teaspoon.metric.culinary",
            },
          },
          {
            id: "flour",
            name: "flour",
            source_text: "2 cups flour",
            normalization: { method: "unresolved", warnings: ["Kept as written."] },
          },
        ],
      },
    ],
  }
  assert.equal(recipeMeasurementRegion(recipe), undefined)

  const resolved: Pick<Recipe, "ingredient_sections"> = {
    ingredient_sections: [
      {
        id: "main",
        ingredients: [
          {
            id: "flour",
            name: "flour",
            quantity: { kind: "simple", amount: { value: "2" }, unit: "volume.cup.us.customary" },
          },
        ],
      },
    ],
  }
  assert.equal(recipeMeasurementRegion(resolved), "us")
})

test("a measure that belongs to a convention can be put back to its words", () => {
  // A teaspoon imported under unit registry 0.1.0 became the metric spoon with
  // no review warning, and must still be revertible.
  assert.equal(isRegionallyAmbiguous("volume.teaspoon.metric.culinary"), true)
  assert.equal(isRegionallyAmbiguous("volume.cup.us.customary"), true)
  assert.equal(isRegionallyAmbiguous("volume.pint.imperial"), true)

  // Belonging to a convention is not the test. A gram is metric and nothing
  // else is called a gram, so there is nothing to put back.
  assert.equal(isRegionallyAmbiguous("mass.gram"), false)
  assert.equal(isRegionallyAmbiguous("volume.milliliter"), false)
  assert.equal(isRegionallyAmbiguous("count.clove"), false)
  assert.equal(isRegionallyAmbiguous(undefined), false)
})
