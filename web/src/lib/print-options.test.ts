import assert from "node:assert/strict"
import test from "node:test"

import {
  type PrintSettings,
  defaultSettings,
  familyOf,
  parsePrintSearch,
  searchFromSettings,
  settingsFromSearch,
  toRequestOptions,
} from "./print-options.ts"

const settings = (change: Partial<PrintSettings> = {}): PrintSettings => ({
  ...defaultSettings(),
  ...change,
})

test("a card size and a page size are different composition environments", () => {
  assert.equal(familyOf("letter"), "full-page")
  assert.equal(familyOf("a4"), "full-page")
  for (const size of ["card-3x5", "card-4x6", "card-5x8"] as const) {
    assert.equal(familyOf(size), "card")
  }
})

test("printing starts by keeping the optional parts of a recipe", () => {
  const opening = defaultSettings()
  assert.equal(opening.description, true)
  assert.equal(opening.notes, true)
  assert.equal(opening.source, false)
  assert.equal(opening.image, "automatic")
  assert.equal(opening.layout, "automatic")
})

test("the settings on screen survive a reload of the address", () => {
  const chosen = settings({
    size: "card-3x5",
    output: "sheet-letter",
    image: "omit",
    notes: false,
    measurement: "metric",
  })
  const search = parsePrintSearch(searchFromSettings(chosen) as Record<string, unknown>)
  assert.deepEqual(settingsFromSearch(search), chosen)
})

test("an address naming nothing valid falls back rather than failing", () => {
  const search = parsePrintSearch({ size: "postcard", output: "fax", batch: "0", scale: "custom" })
  assert.equal(search.size, undefined)
  assert.equal(search.output, undefined)
  assert.equal(search.batch, undefined)
  assert.equal(search.scale, undefined)
  assert.deepEqual(settingsFromSearch(search), defaultSettings())
})

test("a selection travels as the recipes it names", () => {
  assert.deepEqual(parsePrintSearch({ recipes: ["r1", "r2"] }).recipes, ["r1", "r2"])
  assert.deepEqual(parsePrintSearch({ recipes: "r1,r2" }).recipes, ["r1", "r2"])
  assert.equal(parsePrintSearch({ recipes: [] }).recipes, undefined)
})

test("a full page is never arranged on sheets", () => {
  const options = toRequestOptions(settings({ size: "letter", output: "sheet-letter" }), {
    measurement: "original",
  })
  assert.equal(options.output, "native")
})

test("the batch on screen is sent as the exact factor it is", () => {
  const options = toRequestOptions(settings({ scale: "current" }), {
    scale: "3/2",
    measurement: "original",
  })
  assert.equal(options.scale, "custom")
  assert.equal(options.custom_scale, "3/2")
})

test("reading a recipe at its own batch prints it as written", () => {
  const options = toRequestOptions(settings({ scale: "current" }), { measurement: "original" })
  assert.equal(options.scale, "original")
  assert.equal(options.custom_scale, undefined)
})

test("the batch a reader always starts at is resolved by the server", () => {
  const options = toRequestOptions(settings({ scale: "preferred" }), {
    scale: "2",
    measurement: "original",
  })
  assert.equal(options.scale, "preferred")
  assert.equal(options.custom_scale, undefined)
})

test("the measures on screen carry over unless another set is chosen", () => {
  assert.equal(
    toRequestOptions(settings({ measurement: "current" }), { measurement: "metric" }).measurement,
    "metric",
  )
  assert.equal(
    toRequestOptions(settings({ measurement: "us" }), { measurement: "metric" }).measurement,
    "us",
  )
})
