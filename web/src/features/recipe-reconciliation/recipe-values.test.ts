import { expect, test } from "vitest"

import type { Recipe } from "@/api/client"

import {
  areaMatches,
  areaReading,
  comparableAreas,
  conflictLines,
  readableLines,
  readableValue,
} from "./recipe-values"

function recipe(fields: Record<string, unknown>): Recipe {
  return {
    id: "document",
    title: "Recipe",
    schema_version: 1,
    ingredient_sections: [],
    instruction_sections: [],
    ...fields,
  } as Recipe
}

test("readableValue reads empty, encoded, and primitive values", () => {
  expect(readableValue(undefined)).toBe("Empty")
  expect(readableValue(null)).toBe("Empty")
  expect(readableValue("")).toBe("Empty")
  expect(readableValue([])).toBe("Empty")
  expect(readableValue("plain text")).toBe("plain text")
  expect(readableValue('{"text":"encoded text"}')).toBe("encoded text")
  expect(readableValue(5)).toBe("5")
  expect(readableValue(true)).toBe("true")
})

test("readableValue reads ingredient quantities and nested titled sections", () => {
  const ingredient = readableValue({
    name: "flour",
    quantity: { kind: "simple", amount: { value: "200", unit: "g" } },
  })
  expect(ingredient).toContain("200")
  expect(ingredient).toContain("flour")

  expect(readableValue({ title: "Dough", steps: [{ text: "Mix." }, { text: "Rest." }] })).toBe(
    "Dough\nMix.\nRest.",
  )

  expect(readableValue({ custom_key: "surprise" })).toBe("Custom key: surprise")
})

test("readableLines returns one line per readable part and none for empty values", () => {
  expect(readableLines(undefined)).toEqual([])
  expect(readableLines([])).toEqual([])
  expect(readableLines(["first", "second"])).toEqual(["first", "second"])
})

test("areaReading reads photos once, main first, with unresolved references retained", () => {
  const document = recipe({
    primary_image: "a1",
    image_refs: ["a1", "a2", "a9"],
    assets: [
      { id: "a1", path: "x/main.jpg", media_type: "image/jpeg", alt: "Main" },
      { id: "a2", path: "x/second.jpg", media_type: "image/jpeg" },
      { id: "a3", path: "x/extra.jpg", media_type: "image/jpeg" },
    ],
  })

  const readings = areaReading(document, "photos")
  expect(readings).toHaveLength(1)
  expect(readings[0].lines).toEqual(["Main (main photo)", "second.jpg", "extra.jpg", "a9"])
})

test("areaReading other preserves and humanizes unknown portable members", () => {
  const document = recipe({
    custom_field: "kept",
    weird_thing: { custom_inner: "inside" },
  })

  const readings = areaReading(document, "other")
  expect(readings.map((reading) => reading.field)).toEqual(["custom_field", "weird_thing"])
  expect(readings[0].lines).toEqual(["kept"])
  expect(readings[1].lines).toEqual(["Custom inner: inside"])
})

test("conflictLines reads top-level recipe fields and falls back to stored values for nested paths", () => {
  const document = recipe({
    yields: [{ quantity: { kind: "simple", amount: { value: "4" } } }],
    source: { name: "The Book", url: "https://example.invalid/book" },
  })

  const yieldLines = conflictLines(document, "yields", { amount: 99 })
  expect(yieldLines).toHaveLength(1)
  expect(yieldLines[0]).toContain("4")

  const sourceLines = conflictLines(document, "source", "raw source")
  expect(sourceLines.some((line) => line.includes("The Book"))).toBe(true)
  expect(sourceLines.some((line) => line.includes("https://example.invalid/book"))).toBe(true)

  expect(
    conflictLines(document, "ingredient_sections[0].ingredients[1]", { text: "stored part" }),
  ).toEqual(["stored part"])
})

test("areaMatches detects authored order and content changes", () => {
  const left = recipe({ notes: ["first note", "second note"] })
  const same = recipe({ notes: ["first note", "second note"] })
  const reordered = recipe({ notes: ["second note", "first note"] })
  const changed = recipe({ notes: ["first note", "different note"] })

  expect(areaMatches(left, same, "notes")).toBe(true)
  expect(areaMatches(left, reordered, "notes")).toBe(false)
  expect(areaMatches(left, changed, "notes")).toBe(false)
  expect(areaMatches(left, changed, "title")).toBe(true)
})

test("comparableAreas includes either side's nonempty areas", () => {
  const left = recipe({ notes: ["only here"] })
  const right = recipe({ description: "only there" })

  const areas = comparableAreas(left, right)
  expect(areas).toContain("notes")
  expect(areas).toContain("description")
  expect(areas).not.toContain("equipment")
})

test("readableValue handles value/unit objects, symbols, and unsupported values", () => {
  expect(readableValue({ value: "250", unit: "ml" })).toBe("250 ml")
  expect(readableValue({ value: "250" })).toBe("250")
  expect(readableValue({ unit: "g" })).toBe("g")

  expect(readableValue(Symbol("my-symbol"))).toBe("my-symbol")
  expect(readableValue(Symbol())).toBeTruthy()
  expect(readableValue(() => {})).toBeTruthy()
})

test("assetLine falls back to asset id when path has no filename and no alt/caption", () => {
  expect(readableValue({ id: "hero-id", path: "", media_type: "image/jpeg" })).toBe("hero-id")
})

test("conflictLines formats source members and image references", () => {
  const document = recipe({
    primary_image: "img-1",
    image_refs: ["img-1", "img-2"],
    assets: [
      { id: "img-1", path: "photos/cover.jpg", media_type: "image/jpeg", alt: "Cover" },
      { id: "img-2", path: "photos/step.jpg", media_type: "image/jpeg" },
    ],
    source: {
      name: "Cookbook",
      url: "https://example.invalid",
      attribution: "Author Name",
      application: "App 1.0",
      source_id: "src-123",
      retrieved_at: "2026-01-01",
      custom_note: "Special edition",
    },
  })

  const primaryLines = conflictLines(document, "primary_image", "fallback")
  expect(primaryLines).toEqual(["Cover"])

  const imageRefLines = conflictLines(document, "image_refs", "fallback")
  expect(imageRefLines).toEqual(["Cover", "step.jpg"])

  const sourceDetails = conflictLines(document, "source", "fallback")
  expect(sourceDetails.some((line) => line.includes("Author Name"))).toBe(true)
  expect(sourceDetails.some((line) => line.includes("App 1.0"))).toBe(true)
  expect(sourceDetails.some((line) => line.includes("src-123"))).toBe(true)
  expect(sourceDetails.some((line) => line.includes("2026-01-01"))).toBe(true)
  expect(sourceDetails.some((line) => line.includes("Special edition"))).toBe(true)
})
