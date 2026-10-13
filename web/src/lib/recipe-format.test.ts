import assert from "node:assert/strict"
import test from "node:test"

import type { components } from "@/api/schema"

import { calendarDate } from "./date-format.ts"
import { formatDuration } from "./duration-format.ts"
import { durationMinutes, minutesDuration } from "./duration.ts"
import { configurePresentation } from "./locale-format.ts"
import { divide, rational } from "./rational.ts"
import {
  asWritten,
  ingredientLine,
  ingredientsKeptAsWritten,
  ingredientsWithUnscaledAmounts,
  temperatureLine,
  visibleStepTitle,
  withNote,
  yieldAmount,
  yieldLine,
  yieldNoun,
} from "./recipe-format.ts"
import {
  summarizeTimes,
  totalContradictions,
  timeSummaryLine,
  timingLine,
} from "./timing-summary.ts"

test("structured ingredient quantity wins over preserved source text", () => {
  const line = ingredientLine(
    {
      id: "garlic",
      name: "garlic",
      source_text: "a couple of garlic cloves",
      quantity: {
        kind: "simple",
        amount: { value: "2" },
        unit: "count.clove",
      },
    },
    { system: "original", scale: rational(1n) },
  )
  assert.equal(line.amount, "2 cloves")
  assert.equal(line.text, "garlic")
})

test("scaled US ingredients use kitchen fractions and keep a space before the name", () => {
  const line = ingredientLine(
    {
      id: "oil",
      name: "neutral oil",
      quantity: {
        kind: "simple",
        amount: { value: "1/2" },
        unit: "volume.teaspoon.us.customary",
      },
    },
    { system: "original", scale: rational(15n, 8n) },
  )
  assert.equal(line.amount, "1 tsp")
  assert.equal(line.text, "neutral oil")
  assert.equal(line.approximate, true)
})

test("scaled US ingredients can use an exact compound measurement plan", () => {
  const line = ingredientLine(
    {
      id: "oil",
      name: "neutral oil",
      quantity: {
        kind: "simple",
        amount: { value: "1/2" },
        unit: "volume.teaspoon.us.customary",
      },
    },
    { system: "original", scale: rational(19n) },
  )
  assert.equal(line.amount, "3 tbsp + ½ tsp")
  assert.equal(line.approximate, false)
})

test("compound alternatives scale as written and select an authored requested-system option", () => {
  const ingredient = {
    id: "oil",
    name: "oil",
    quantity: {
      kind: "alternatives" as const,
      options: [
        {
          kind: "sum" as const,
          terms: [
            {
              kind: "simple" as const,
              amount: { value: "2" },
              unit: "volume.tablespoon.us.customary",
            },
            {
              kind: "simple" as const,
              amount: { value: "2" },
              unit: "volume.teaspoon.us.customary",
            },
          ],
        },
        { kind: "simple" as const, amount: { value: "40" }, unit: "volume.milliliter" },
      ],
    },
  }
  assert.equal(
    ingredientLine(ingredient, { system: "original", scale: rational(2n) }).amount,
    "¼ cup plus 1 tbsp + 1 tsp or 80 ml",
  )
  assert.equal(
    ingredientLine(ingredient, { system: "metric", scale: rational(1n) }).amount,
    "40 ml",
  )
  assert.equal(
    ingredientLine(ingredient, { system: "us", scale: rational(1n) }).amount,
    "2 tbsp plus 2 tsp",
  )
  assert.equal(
    ingredientLine(ingredient, { system: "au", scale: rational(1n) }).amount,
    "2 tbsp",
    "when no option is authored in the target system, the simple option converts cleanly",
  )
})

test("instruction temperatures render their structured amount and unit", () => {
  assert.equal(
    temperatureLine(
      {
        amount: { value: "350" },
        unit: "temperature.fahrenheit",
        source_text: "moderate oven",
        note: "preheated",
      },
      "original",
    ),
    "350°F",
  )
})

test("temperature presentation retains structured purpose through conversion", () => {
  const temperature = {
    amount: { value: "350" },
    unit: "temperature.fahrenheit",
    purpose: { kind: "internal" as const, label: "chicken breast" },
  }
  assert.equal(temperatureLine(temperature, "original"), "350°F internal (chicken breast)")
  assert.match(temperatureLine(temperature, "metric"), /°C internal \(chicken breast\)$/)
  assert.deepEqual(temperature.purpose, {
    kind: "internal",
    label: "chicken breast",
  })
  assert.equal(
    temperatureLine(
      {
        amount: { value: "240" },
        unit: "temperature.fahrenheit",
        purpose: { kind: "other", label: "sugar syrup" },
      },
      "original",
    ),
    "240°F sugar syrup",
  )
})

const method = {
  ingredient_sections: [{ id: "s", ingredients: [{ id: "i", name: "salt" }] }],
  instruction_sections: [{ id: "n", steps: [{ id: "p", text: "Cook it." }] }],
  id: "r",
  schema_version: 1,
  title: "Test",
}

test("an exact ISO duration reads and writes back unchanged", () => {
  assert.equal(durationMinutes("PT9H15M"), 555)
  assert.equal(durationMinutes("P1DT2H30M"), 26 * 60 + 30)
  assert.equal(durationMinutes("P1W"), 7 * 24 * 60)
  // A month has no fixed length without a calendar position.
  assert.equal(durationMinutes("P1M"), undefined)
  assert.equal(durationMinutes("P1Y"), undefined)
  assert.equal(minutesDuration(555), "PT9H15M")
  assert.equal(minutesDuration(26 * 60 + 30), "P1DT2H30M")
  assert.equal(minutesDuration(0), "")
})

test("timed phases separate the work from the waiting", () => {
  const recipe = {
    ...method,
    times: {
      total: { value: "PT9H15M" },
      assertions: [
        { id: "t1", kind: "marinate" as const, duration: { value: "PT8H" } },
        { id: "t2", kind: "prep" as const, duration: { value: "PT20M" } },
        { id: "t3", kind: "cook" as const, duration: { value: "PT45M" } },
        { id: "t4", kind: "rest" as const, duration: { value: "PT10M" } },
      ],
    },
  }
  assert.deepEqual(summarizeTimes(recipe), {
    prep: "PT20M",
    cook: "PT45M",
    // Cooking does not count as work by default.
    active: "PT20M",
    elapsed: "PT9H15M",
    writtenTotal: "",
  })
  assert.equal(timeSummaryLine(recipe), "20 min active · 9 hr 15 min total")
  assert.equal(timingLine(recipe.times.assertions[0]), "Marinate 8 hr")
})

test("an authored total is believed over adding overlapping phases", () => {
  const recipe = {
    ...method,
    times: {
      total: { value: "PT8H" },
      assertions: [
        { id: "t5", kind: "proof" as const, duration: { value: "PT8H" } },
        { id: "t6", kind: "prep" as const, duration: { value: "PT15M" } },
      ],
    },
  }
  assert.equal(summarizeTimes(recipe).elapsed, "PT8H")
  assert.equal(summarizeTimes(recipe).active, "PT15M")
})

test("a coarse summary still answers both questions", () => {
  const recipe = {
    ...method,
    times: { prep: { value: "PT45M" }, cook: { value: "PT30M" }, additional: { value: "PT1H" } },
  }
  assert.deepEqual(summarizeTimes(recipe), {
    prep: "PT45M",
    cook: "PT30M",
    active: "PT1H15M",
    elapsed: "PT2H15M",
    writtenTotal: "",
  })
})

test("a recipe that only says one time does not claim two", () => {
  const recipe = { ...method, times: { total: { value: "PT30M" } } }
  assert.equal(timeSummaryLine(recipe), "30 min")
})

test("phases alone do not claim a complete elapsed time", () => {
  const recipe = {
    ...method,
    times: { assertions: [{ id: "t7", kind: "chill" as const, duration: { value: "PT2H" } }] },
  }
  assert.equal(summarizeTimes(recipe).elapsed, "")
})

test("partial phases do not erase coarse working time", () => {
  const recipe = {
    ...method,
    times: {
      prep: { value: "PT20M" },
      cook: { value: "PT30M" },
      total: { value: "PT8H50M" },
      assertions: [{ id: "t8", kind: "marinate" as const, duration: { value: "PT8H" } }],
    },
  }
  assert.equal(summarizeTimes(recipe).active, "PT50M")
  assert.equal(summarizeTimes(recipe).elapsed, "PT8H50M")
})

test("a scalable yield is read as a number and scales to the wanted amount", () => {
  const yielded = {
    quantity: { kind: "simple" as const, amount: { value: "24" } },
    label: "cookies",
  }
  const written = yieldAmount(yielded)
  assert.ok(written)
  assert.equal(yieldNoun(yielded), "cookies")

  // "I need 12 cookies instead of 24" is a half batch, not a 0.5x request.
  const scale = divide(rational(12n), written)
  assert.equal(yieldLine(yielded, { system: "original", scale }), "12 cookies")
})

test("a yield that is only words is shown beside the batch rather than recalculated", () => {
  // The 9 describes the tin rather than counting what comes out of it.
  const yielded = { text: "Makes one 9-inch tart" }
  assert.equal(yieldAmount(yielded), undefined)
  assert.equal(
    yieldLine(yielded, { system: "original", scale: rational(2n) }),
    "Makes one 9-inch tart × 2",
  )
})

test("a yield written as words states what the scaled batch makes", () => {
  const rolls = { text: "24 rolls" }
  // A cook reading a doubled recipe needs 48, not 24 and a multiplication.
  assert.equal(yieldLine(rolls, { system: "original", scale: rational(2n) }), "48 rolls")
  assert.equal(yieldNoun(rolls), "rolls")
  const written = yieldAmount(rolls)
  assert.ok(written)
  assert.equal(
    yieldLine(rolls, { system: "original", scale: divide(rational(36n), written) }),
    "36 rolls",
  )

  const people = { text: "4 to 5 people" }
  assert.equal(yieldLine(people, { system: "original", scale: rational(3n) }), "12 to 15 people")
  // A range cannot be stepped to a single wanted amount.
  assert.equal(yieldAmount(people), undefined)

  const loaves = { text: "makes 3 loaves" }
  assert.equal(
    yieldLine(loaves, { system: "original", scale: rational(1n, 2n) }),
    "makes 1½ loaves",
  )
})

test("every duration the model accepts is also one the reader can read", () => {
  // The accepted vocabulary and the displayable vocabulary are the same set.
  assert.equal(formatDuration("PT20M"), "20 min")
  assert.equal(formatDuration("P1W"), "1 week")
  assert.equal(formatDuration("P3M"), "3 months")
  assert.equal(formatDuration("P1Y"), "1 year")
  assert.equal(formatDuration("P1DT2H30M"), "1 day 2 hr 30 min")
  assert.equal(formatDuration("P2W3D"), "2 weeks 3 days")
})

test("durations carry minutes into hours and read in the interface language", () => {
  assert.equal(formatDuration("PT375M"), "6 hr 15 min")
  assert.equal(formatDuration("PT90S"), "1 min 30 sec")
  assert.equal(formatDuration("PT48H"), "48 hr")
  configurePresentation(() => ({ locale: "de", timeZone: "UTC" }))
  try {
    assert.equal(formatDuration("PT75M"), "1 Std. 15 Min.")
    assert.equal(formatDuration("P2D"), "2 Tage")
  } finally {
    configurePresentation(() => ({ locale: "en", timeZone: "UTC" }))
  }
})

test("a note in brackets follows its ingredient without a comma", () => {
  assert.equal(withNote("chicken drumsticks", "(Note 1)"), "chicken drumsticks (Note 1)")
  assert.equal(withNote("onion", "chopped"), "onion, chopped")
  assert.equal(withNote("", "to serve"), "to serve")
})

test("a duration with no fixed length is shown but never added into a total", () => {
  const cured = {
    ...method,
    times: {
      assertions: [
        { id: "t9", kind: "cure" as const, duration: { value: "P3M" } },
        { id: "t10", kind: "prep" as const, duration: { value: "PT20M" } },
      ],
    },
  }
  assert.equal(formatDuration("P3M"), "3 months")
  assert.equal(durationMinutes("P3M"), undefined)
  assert.equal(summarizeTimes(cured).elapsed, "")
  assert.equal(summarizeTimes(cured).active, "PT20M")
})

test("today is the reader's calendar day rather than the day it is in UTC", () => {
  // 22:30 on the 20th in a zone behind UTC is still the 20th, though
  // toISOString would already call it the 21st.
  configurePresentation(() => ({ locale: "en", timeZone: "America/Denver" }))
  const evening = new Date("2026-08-20T22:30:00-06:00")
  assert.equal(calendarDate(evening), "2026-08-20")
  const earlyMorning = new Date("2026-01-01T00:30:00-07:00")
  assert.equal(calendarDate(earlyMorning), "2026-01-01")
  configurePresentation(() => ({ locale: "en", timeZone: "UTC" }))
})

test("a total the recipe contradicts is set aside rather than headlined", () => {
  const recipe = {
    ...method,
    times: {
      total: { value: "PT40M" },
      assertions: [
        { id: "t11", kind: "proof" as const, duration: { value: "PT1H30M" } },
        { id: "t12", kind: "cook" as const, duration: { value: "PT22M" }, label: "Bake" },
        // Hands-on work is stated as attention rather than a kind.
        { id: "t13", duration: { value: "PT15M" }, attention: "active" as const },
      ],
    },
  }
  const summary = summarizeTimes(recipe)
  assert.equal(summary.elapsed, "")
  assert.equal(summary.writtenTotal, "PT40M")
  // The headline says what can be defended, and never the disproved total.
  assert.equal(timeSummaryLine(recipe), "15 min active")
  assert.deepEqual(totalContradictions(recipe.times), [
    "At least one named phase is longer than the total.",
  ])
})

test("a contradicted coarse total falls back to the clock it contradicts", () => {
  const recipe = {
    ...method,
    times: { prep: { value: "PT20M" }, cook: { value: "PT30M" }, total: { value: "PT40M" } },
  }
  const summary = summarizeTimes(recipe)
  assert.equal(summary.elapsed, "PT50M")
  assert.equal(summary.writtenTotal, "PT40M")
})

test("an elapsed time Savorum added up is dropped when a phase disproves it", () => {
  // With no authored total, prep plus cook is disproved by the long prove.
  const recipe = {
    ...method,
    times: {
      prep: { value: "PT15M" },
      cook: { value: "PT25M" },
      assertions: [
        {
          id: "t14",
          kind: "proof" as const,
          duration: { value: "PT1H30M" },
          provenance: "derived" as const,
        },
        {
          id: "t15",
          kind: "cook" as const,
          duration: { min: "PT22M", max: "PT26M" },
          provenance: "derived" as const,
          label: "Bake",
        },
      ],
    },
  }
  const summary = summarizeTimes(recipe)
  assert.equal(summary.elapsed, "")
  assert.equal(summary.writtenTotal, "", "Savorum added this up; the recipe never stated it")
  assert.equal(summary.active, "PT40M")
  assert.equal(timeSummaryLine(recipe), "40 min active")
})

test("an elapsed time every phase fits inside is kept", () => {
  const recipe = {
    ...method,
    times: {
      prep: { value: "PT15M" },
      cook: { value: "PT45M" },
      additional: { value: "PT2H" },
      assertions: [{ id: "t16", kind: "rest" as const, duration: { value: "PT10M" } }],
    },
  }
  assert.equal(summarizeTimes(recipe).elapsed, "PT3H")
})

test("identity choices scale every branch and preserve authored relationships and scopes", () => {
  const choice = {
    id: "fat",
    relation: "preferred" as const,
    optional: true,
    note: "If unavailable",
    alternatives: [
      {
        id: "butter",
        name: "butter",
        quantity: {
          kind: "simple" as const,
          amount: { value: "1" },
          unit: "volume.cup.us.customary",
        },
      },
      {
        id: "oil",
        name: "oil",
        note: "neutral",
        quantity: {
          kind: "simple" as const,
          amount: { value: "3/4" },
          unit: "volume.cup.us.customary",
        },
      },
    ],
  }
  const shown = ingredientLine(choice, { system: "original", scale: rational(2n) })
  assert.match(
    shown.text,
    /Prefer 2 cups butter; alternatively, 1½ cups oil, neutral — If unavailable \(optional\)/,
  )
  assert.equal(choice.alternatives[1].quantity.amount.value, "3/4")
  assert.match(
    ingredientLine({ ...choice, relation: "and_or" }, { system: "original", scale: rational(1n) })
      .text,
    / and\/or /,
  )
})

test("an authored shared exact branch quantity moves to the amount column", () => {
  const quantity = {
    kind: "simple" as const,
    amount: { value: "1" },
    unit: "volume.cup.us.customary",
  }
  const line = ingredientLine(
    {
      id: "fat",
      relation: "or",
      alternatives: [
        { id: "butter", name: "butter", quantity },
        { id: "margarine", name: "margarine", quantity },
      ],
    },
    { system: "original", scale: rational(1n) },
  )
  assert.equal(line.amount, "1 cup")
  assert.equal(line.text, "butter or margarine")

  const preferred = ingredientLine(
    {
      id: "fat-preferred",
      relation: "preferred",
      alternatives: [
        { id: "butter-preferred", name: "butter", quantity },
        { id: "margarine-preferred", name: "margarine", quantity },
      ],
    },
    { system: "original", scale: rational(2n) },
  )
  assert.equal(preferred.amount, "Prefer 2 cups")
  assert.equal(preferred.text, "butter; alternatively, margarine")

  const andOr = ingredientLine(
    {
      id: "fat-and-or",
      relation: "and_or",
      alternatives: [
        { id: "butter-and-or", name: "butter", quantity },
        { id: "margarine-and-or", name: "margarine", quantity },
      ],
    },
    { system: "original", scale: rational(2n) },
  )
  assert.equal(andOr.amount, "2 cups")
  assert.equal(andOr.text, "butter and/or margarine")
})

test("alternative branches retain quantities unless their exact unit and value match", () => {
  const line = (alternatives: NonNullable<components["schemas"]["Ingredient"]["alternatives"]>) =>
    ingredientLine(
      {
        id: "choice",
        relation: "or",
        alternatives,
      },
      asWritten,
    )

  const differentAmount = line([
    {
      id: "lime",
      name: "lime",
      quantity: { kind: "simple", amount: { value: "1" }, unit: "count.item" },
    },
    {
      id: "lemon",
      name: "lemon",
      quantity: { kind: "simple", amount: { value: "2" }, unit: "count.item" },
    },
  ])
  assert.equal(differentAmount.amount, "")
  assert.equal(differentAmount.text, "1 lime or 2 lemon")

  const differentUnit = line([
    {
      id: "oil",
      name: "oil",
      quantity: { kind: "simple", amount: { value: "1" }, unit: "volume.cup.us.customary" },
    },
    {
      id: "oil-metric",
      name: "oil",
      quantity: { kind: "simple", amount: { value: "1" }, unit: "volume.liter" },
    },
  ])
  assert.equal(differentUnit.amount, "")
  assert.equal(differentUnit.text, "1 cup oil or 1 l oil")

  const differentPackageSize = line([
    {
      id: "tomatoes-large",
      name: "tomatoes",
      quantity: { kind: "simple", amount: { value: "1" }, unit: "count.item" },
      package_size: { kind: "simple", amount: { value: "400" }, unit: "mass.gram" },
    },
    {
      id: "tomatoes-small",
      name: "tomatoes",
      quantity: { kind: "simple", amount: { value: "1" }, unit: "count.item" },
      package_size: { kind: "simple", amount: { value: "14" }, unit: "mass.ounce.avoirdupois" },
    },
  ])
  assert.equal(differentPackageSize.amount, "")
  assert.match(differentPackageSize.text, /400 g/)
  assert.match(differentPackageSize.text, /14 oz/)

  const uncertainOrChoice = line([
    {
      id: "herbs",
      name: "herbs",
      quantity: { kind: "simple", amount: { value: "1", approximate: true }, unit: "count.item" },
    },
    {
      id: "spices",
      name: "spices",
      quantity: {
        kind: "alternatives",
        options: [
          { kind: "simple", amount: { value: "1" }, unit: "count.item" },
          { kind: "simple", amount: { value: "2" }, unit: "count.item" },
        ],
      },
    },
  ])
  assert.equal(uncertainOrChoice.amount, "")
  assert.equal(uncertainOrChoice.text, "1 herbs or 1 or 2 spices")
})

test("opaque temperatures retain authored wording and purpose in every measurement system", () => {
  const temperature = { text: "moderate oven", source_text: "source wording", note: "preheated" }
  for (const system of ["original", "metric", "us"] as const) {
    assert.equal(temperatureLine(temperature, system), "moderate oven")
    assert.equal(
      temperatureLine({ ...temperature, purpose: { kind: "oven" } }, system),
      "moderate oven (oven)",
    )
  }
})

test("a batch reports the amounts it could not recalculate", () => {
  // The source wrote its own metric equivalents into the line. The importer
  // could read a quantity out of two of these and nothing out of the third.
  const butter = {
    id: "butter",
    name: "unsalted butter",
    note: "softened, more for pans, 227 grams, 2 sticks",
    quantity: {
      kind: "simple" as const,
      amount: { value: "1" },
      unit: "volume.cup.us.customary",
    },
    source_text: "1 cup unsalted butter, softened, more for pans, 227 grams, 2 sticks",
  }
  const sugar = {
    id: "sugar",
    name: "0.33 cup/66 grams granulated sugar",
    source_text: "0.33 cup/66 grams granulated sugar",
  }
  const eggs = {
    id: "eggs",
    name: "large eggs",
    quantity: { kind: "simple" as const, amount: { value: "2" }, unit: "count.item" },
    source_text: "2 large eggs",
  }
  const salt = { id: "salt", name: "salt and pepper", source_text: "Salt and pepper" }
  const doubled = { system: "original" as const, scale: rational(2n) }

  assert.equal(ingredientLine(butter, doubled).amount, "2 cups")
  assert.equal(ingredientLine(butter, doubled).keptAmount, false)
  assert.equal(
    ingredientLine(butter, doubled).keptWording,
    true,
    "the 227 grams left in the note is still the recipe's own",
  )
  assert.equal(ingredientLine(sugar, doubled).keptAmount, true)
  assert.equal(ingredientLine(eggs, doubled).keptAmount, false)
  assert.equal(ingredientLine(eggs, doubled).keptWording, false)
  assert.equal(
    ingredientLine(salt, doubled).keptAmount,
    false,
    "a line stating no amount has no amount to keep",
  )

  for (const ingredient of [butter, sugar, eggs, salt]) {
    const line = ingredientLine(ingredient, asWritten)
    assert.equal(line.keptAmount, false, "nothing is kept at the recipe's own batch")
    assert.equal(line.keptWording, false, "nothing is kept at the recipe's own batch")
  }

  assert.equal(
    ingredientsKeptAsWritten(
      {
        ingredient_sections: [{ id: "s1", ingredients: [butter, sugar, eggs, salt] }],
      } as Parameters<typeof ingredientsKeptAsWritten>[0],
      doubled,
    ),
    2,
  )
})

test("an opaque amount is reported as kept rather than silently repeated", () => {
  const pinch = {
    id: "pinch",
    name: "saffron",
    quantity: { kind: "opaque" as const, text: "a generous pinch" },
    source_text: "A generous pinch of saffron",
  }
  assert.equal(ingredientLine(pinch, { system: "original", scale: rational(2n) }).keptAmount, true)
  assert.equal(ingredientLine(pinch, asWritten).keptAmount, false)
})

test("a step heading the prose already opens with is not shown twice", () => {
  assert.equal(visibleStepTitle("Make the dough", "Make the dough by mixing"), undefined)
  assert.equal(visibleStepTitle(" make the dough ", "MAKE THE DOUGH"), undefined)
  assert.equal(visibleStepTitle("Rest", "Cover and leave"), "Rest")
  assert.equal(visibleStepTitle("Rest", undefined), "Rest")
  assert.equal(visibleStepTitle("  ", "Cover"), undefined)
  assert.equal(visibleStepTitle(undefined, "Cover"), undefined)
})

test("a choice reports its branches as separate lines beside the sentence", () => {
  const grams = (value: string) => ({
    kind: "simple" as const,
    amount: { value },
    unit: "mass.gram",
  })
  const different = ingredientLine(
    {
      id: "flour",
      relation: "or",
      note: "pastry flour for a softer version",
      alternatives: [
        { id: "wheat", name: "wheat flour", quantity: grams("325") },
        { id: "pastry", name: "pastry flour", quantity: grams("165") },
      ],
    },
    asWritten,
  )
  assert.equal(different.amount, "")
  assert.deepEqual(different.choice, {
    sharedAmount: false,
    lead: "",
    joiner: "or",
    branches: [
      { amount: "325 g", text: "wheat flour" },
      { amount: "165 g", text: "pastry flour" },
    ],
    note: "pastry flour for a softer version",
    optional: false,
  })

  const shared = ingredientLine(
    {
      id: "fat",
      relation: "preferred",
      optional: true,
      alternatives: [
        { id: "butter", name: "butter", quantity: grams("200") },
        { id: "margarine", name: "margarine", quantity: grams("200"), optional: true },
      ],
    },
    asWritten,
  )
  assert.equal(shared.choice?.sharedAmount, true)
  assert.equal(shared.choice?.lead, "")
  assert.equal(shared.choice?.joiner, "alternatively")
  assert.deepEqual(shared.choice?.branches, [
    { amount: "", text: "butter" },
    { amount: "", text: "margarine (optional)" },
  ])
  assert.equal(shared.choice?.optional, true)

  const leading = ingredientLine(
    {
      id: "flour-preferred",
      relation: "preferred",
      alternatives: [
        { id: "wheat-preferred", name: "wheat flour", quantity: grams("325") },
        { id: "pastry-preferred", name: "pastry flour", quantity: grams("165") },
      ],
    },
    asWritten,
  )
  assert.equal(leading.choice?.lead, "Prefer")

  assert.equal(ingredientLine({ id: "salt", name: "salt" }, asWritten).choice, undefined)
})

test("global scaling reports unscaled amounts without repeating authored wording cues", () => {
  const presentation = { system: "original" as const, scale: rational(2n) }
  const recipe = {
    ingredient_sections: [
      {
        id: "s",
        ingredients: [
          {
            id: "bread",
            name: "bread",
            note: "cut into 2 cm cubes",
            quantity: { kind: "simple", amount: { value: "150" }, unit: "mass.gram" },
          },
        ],
      },
    ],
  } as Parameters<typeof ingredientsWithUnscaledAmounts>[0]
  assert.equal(ingredientsWithUnscaledAmounts(recipe, presentation), 0)
  recipe.ingredient_sections![0]!.ingredients!.push({
    id: "saffron",
    name: "saffron",
    quantity: { kind: "opaque", text: "a generous pinch" },
  })
  assert.equal(ingredientsWithUnscaledAmounts(recipe, presentation), 1)
})
