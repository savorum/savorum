import assert from "node:assert/strict"
import test from "node:test"

import { formatRational, parseRational } from "./rational.ts"
import {
  cleanRecipe,
  emptyIngredient,
  emptyRecipe,
  emptyStep,
  formatAmountInput,
  moveItem,
  newID,
  parseAmount,
  parseDurationInput,
  timingConsistencyWarnings,
} from "./recipe-draft.ts"

test("time guidance flags objectively contradictory authored clocks", () => {
  assert.deepEqual(
    timingConsistencyWarnings({
      prep: { value: "PT20M" },
      cook: { value: "PT30M" },
      total: { value: "PT45M" },
      assertions: [{ id: "t1", kind: "marinate", duration: { value: "PT8H" } }],
    }),
    [
      "The total is shorter than the preparation, cooking, and other time it states.",
      "At least one named phase is longer than the total.",
    ],
  )
})

test("time guidance does not compare calendar durations or overlapping phases", () => {
  assert.deepEqual(
    timingConsistencyWarnings({
      total: { value: "P1M" },
      assertions: [{ id: "t2", kind: "rest", duration: { value: "P1D" } }],
    }),
    [],
  )
  assert.deepEqual(
    timingConsistencyWarnings({
      total: { value: "PT8H" },
      assertions: [{ id: "t3", kind: "marinate", duration: { value: "PT8H" } }],
    }),
    [],
  )
})

test("newID generates unique portable identifiers with prefix", () => {
  const id1 = newID("r")
  const id2 = newID("r")
  assert.match(id1, /^r[a-z0-9]{16}$/)
  assert.match(id2, /^r[a-z0-9]{16}$/)
  assert.notEqual(id1, id2)
})

test("newID gives every character equal weight across the byte range", (t) => {
  let nextByte = 0
  t.mock.method(crypto, "getRandomValues", (bytes: Uint8Array) => {
    for (let index = 0; index < bytes.length; index++) {
      bytes[index] = nextByte
      nextByte = (nextByte + 1) % 256
    }
    return bytes
  })

  const counts = new Map<string, number>()
  for (let index = 0; index < 63; index++) {
    const id = newID("r")
    assert.match(id, /^r[a-z0-9]{16}$/)
    for (const character of id.slice(1)) {
      counts.set(character, (counts.get(character) ?? 0) + 1)
    }
  }
  assert.equal(counts.size, 36)
  for (const count of counts.values()) assert.equal(count, 28)
})

test("newID retries a rejected batch and returns the complete identifier", (t) => {
  let calls = 0
  t.mock.method(crypto, "getRandomValues", (bytes: Uint8Array) => {
    bytes.fill(calls++ === 0 ? 255 : 251)
    return bytes
  })

  assert.equal(newID("choice"), `choice${"9".repeat(16)}`)
  assert.equal(calls, 2)
})

test("newID works with only the crypto API available on insecure HTTP origins", (t) => {
  const getRandomValues = crypto.getRandomValues.bind(crypto)
  t.mock.getter(globalThis, "crypto", () => ({ getRandomValues }))

  assert.match(newID("r"), /^r[a-z0-9]{16}$/)
})

test("emptyIngredient creates an empty ingredient draft", () => {
  const ingredient = emptyIngredient()
  assert.match(ingredient.id, /^i[a-z0-9]{16}$/)
  assert.equal(ingredient.name, "")
})

test("emptyStep creates an empty step draft", () => {
  const step = emptyStep()
  assert.match(step.id, /^p[a-z0-9]{16}$/)
  assert.equal(step.text, "")
})

test("emptyRecipe creates an empty recipe draft with default sections", () => {
  const recipe = emptyRecipe()
  assert.match(recipe.id, /^r[a-z0-9]{16}$/)
  assert.equal(recipe.schema_version, 1)
  assert.equal(recipe.title, "")
  assert.equal(recipe.ingredient_sections?.length, 1)
  assert.match(recipe.ingredient_sections![0].id, /^s[a-z0-9]{16}$/)
  assert.equal(recipe.ingredient_sections![0].title, "")
  assert.equal(recipe.ingredient_sections![0].ingredients?.length, 1)
  assert.match(recipe.ingredient_sections![0].ingredients![0].id, /^i[a-z0-9]{16}$/)
  assert.equal(recipe.instruction_sections?.length, 1)
  assert.match(recipe.instruction_sections![0].id, /^n[a-z0-9]{16}$/)
  assert.equal(recipe.instruction_sections![0].title, "")
  assert.equal(recipe.instruction_sections![0].steps?.length, 1)
  assert.match(recipe.instruction_sections![0].steps![0].id, /^p[a-z0-9]{16}$/)
})

test("parseAmount parses mixed numbers, fractions, decimals, and whole amounts", () => {
  assert.equal(parseAmount(""), undefined)
  assert.equal(parseAmount("   "), undefined)
  assert.equal(parseAmount("abc"), undefined)
  assert.equal(parseAmount("1 1/0"), undefined)
  assert.equal(parseAmount("1/0"), undefined)

  assert.equal(parseAmount("1 1/2"), "3/2")
  assert.equal(parseAmount("2 4/8"), "5/2")
  assert.equal(parseAmount("3/4"), "3/4")
  assert.equal(parseAmount("2/4"), "1/2")
  assert.equal(parseAmount("4/2"), "2")
  assert.equal(parseAmount("2.5"), "5/2")
  assert.equal(parseAmount("0.25"), "1/4")
  assert.equal(parseAmount("3"), "3")
  assert.equal(parseAmount("0"), "0")

  // Negative amounts
  assert.equal(parseAmount("-1/2"), undefined)
  assert.equal(parseAmount("-1/2", false), undefined)
  assert.equal(parseAmount("-1/2", true), "-1/2")
  assert.equal(parseAmount("-2", true), "-2")
  assert.equal(parseAmount("-1 1/2", true), "-3/2")
  assert.equal(parseAmount("-0", true), "0")
})

test("amount parsers bound hostile text and preserve canonical fractions", () => {
  const huge = "9".repeat(100_000)
  assert.equal(parseAmount(huge), undefined)
  assert.equal(parseRational(huge), undefined)
  assert.equal(parseDurationInput(`${huge}h`), undefined)
  assert.equal(parseAmount("1/\u0000"), undefined)

  for (let numerator = 0; numerator < 100; numerator++) {
    const input = `${numerator}/${numerator + 1}`
    const canonical = parseAmount(input)
    assert.ok(canonical)
    const parsed = parseRational(canonical)
    assert.ok(parsed)
    assert.equal(formatRational(parsed), canonical)
  }
})

test("formatAmountInput formats stored rationals as mixed numbers", () => {
  assert.equal(formatAmountInput(), "")
  assert.equal(formatAmountInput(""), "")
  assert.equal(formatAmountInput("3"), "3")
  assert.equal(formatAmountInput("invalid/fraction/parts"), "invalid/fraction/parts")
  assert.equal(formatAmountInput("1/0"), "1/0")
  assert.equal(formatAmountInput("6/2"), "3")
  assert.equal(formatAmountInput("-6/2"), "-3")
  assert.equal(formatAmountInput("1/2"), "1/2")
  assert.equal(formatAmountInput("-1/2"), "-1/2")
  assert.equal(formatAmountInput("3/2"), "1 1/2")
  assert.equal(formatAmountInput("-3/2"), "-1 1/2")
  assert.equal(formatAmountInput("7/4"), "1 3/4")
})

test("parseDurationInput parses ISO durations, natural expressions, and bare numbers", () => {
  assert.equal(parseDurationInput(""), undefined)
  assert.equal(parseDurationInput("   "), undefined)
  assert.equal(parseDurationInput("not-a-duration"), undefined)

  // ISO duration
  assert.equal(parseDurationInput("pt30m"), "PT30M")
  assert.equal(parseDurationInput("P1D"), "P1D")
  assert.equal(parseDurationInput("p1dt2h"), "P1DT2H")

  // Pattern matching
  assert.equal(parseDurationInput("2 days"), "P2D")
  assert.equal(parseDurationInput("1 day"), "P1D")
  assert.equal(parseDurationInput("3d"), "P3D")
  assert.equal(parseDurationInput("2 hours"), "PT2H")
  assert.equal(parseDurationInput("1 hr"), "PT1H")
  assert.equal(parseDurationInput("4 hrs"), "PT4H")
  assert.equal(parseDurationInput("5h"), "PT5H")
  assert.equal(parseDurationInput("45 minutes"), "PT45M")
  assert.equal(parseDurationInput("1 min"), "PT1M")
  assert.equal(parseDurationInput("10 mins"), "PT10M")
  assert.equal(parseDurationInput("15m"), "PT15M")
  assert.equal(parseDurationInput("30 seconds"), "PT30S")
  assert.equal(parseDurationInput("1 sec"), "PT1S")
  assert.equal(parseDurationInput("20 secs"), "PT20S")
  assert.equal(parseDurationInput("10s"), "PT10S")
  assert.equal(parseDurationInput("1d 2h 30m 45s"), "P1DT2H30M45S")
  assert.equal(parseDurationInput("0d 0h 0m 0s"), undefined)

  // Bare number defaults to minutes
  assert.equal(parseDurationInput("90"), "PT90M")
})

test("cleanRecipe strips blank fields while preserving structure and opaque payloads", () => {
  const draft = {
    id: "r1",
    schema_version: 1,
    title: "Draft Cake",
    description: "",
    notes: "   ",
    tags: ["", "  "],
    numbers: [1, 2],
    metadata: { nested: { author: "Chef" }, empty_nested: {} },
    yields: [{ text: "1 cake" }],
    sref_opaque_raw: { raw_property: "retained" },
    extensions_raw: { custom_ext: 42 },
    ingredient_sections: [
      {
        id: "s1",
        title: "",
        ingredients: [
          {
            id: "i1",
            name: "Flour",
            amount: "2",
            unit: "cup",
            notes: "",
          },
          {
            id: "i2",
            name: "",
            alternatives: [{ id: "i2a", name: "Almond Flour" }],
          },
        ],
      },
    ],
    instruction_sections: [
      {
        id: "n1",
        title: "Bake",
        steps: [
          {
            id: "p1",
            title: "Mix",
            text: "Stir together.",
            notes: "",
          },
        ],
      },
    ],
  }

  const cleaned = cleanRecipe(draft as any)
  assert.equal(cleaned.id, "r1")
  assert.equal(cleaned.schema_version, 1)
  assert.equal(cleaned.title, "Draft Cake")
  assert.deepEqual(cleaned.yields, [{ text: "1 cake" }])
  assert.equal("description" in cleaned, false)
  assert.equal("notes" in cleaned, false)
  assert.deepEqual(cleaned.sref_opaque_raw, { raw_property: "retained" })
  assert.deepEqual(cleaned.extensions_raw, { custom_ext: 42 })
  assert.deepEqual((cleaned as any).metadata, { nested: { author: "Chef" } })
  assert.deepEqual((cleaned as any).numbers, [1, 2])

  assert.equal(cleaned.ingredient_sections?.length, 1)
  assert.equal(cleaned.ingredient_sections![0].id, "s1")
  assert.equal("title" in cleaned.ingredient_sections![0], false)
  assert.equal(cleaned.ingredient_sections![0].ingredients?.length, 2)
  assert.equal(cleaned.ingredient_sections![0].ingredients![0].name, "Flour")
  assert.equal("notes" in cleaned.ingredient_sections![0].ingredients![0], false)
  assert.equal("name" in cleaned.ingredient_sections![0].ingredients![1], false)
  assert.equal(cleaned.ingredient_sections![0].ingredients![1].alternatives?.length, 1)

  assert.equal(cleaned.instruction_sections?.length, 1)
  assert.equal(cleaned.instruction_sections![0].id, "n1")
  assert.equal(cleaned.instruction_sections![0].steps?.length, 1)
  assert.equal(cleaned.instruction_sections![0].steps![0].title, "Mix")
  assert.equal(cleaned.instruction_sections![0].steps![0].text, "Stir together.")
  assert.equal("notes" in cleaned.instruction_sections![0].steps![0], false)
})

test("moveItem reorders array elements and returns original array on out-of-bounds", () => {
  const original = ["a", "b", "c"]
  assert.deepEqual(moveItem(original, 0, 2), ["b", "c", "a"])
  assert.deepEqual(moveItem(original, 2, 0), ["c", "a", "b"])
  assert.deepEqual(moveItem(original, 1, 1), ["a", "b", "c"])

  // Out of bounds
  assert.equal(moveItem(original, 0, -1), original)
  assert.equal(moveItem(original, 0, 3), original)
})
