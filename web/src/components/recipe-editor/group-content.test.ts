import { expect, test } from "vitest"

import { hasGroupContent } from "./group-content"

test("empty lists and blank draft rows do not require confirmation", () => {
  for (const rows of [
    undefined,
    null,
    [],
    [{ id: "ingredient", name: "" }],
    [{ id: "step", title: "  ", text: "\n" }],
    [{ id: "step", text: "", image_refs: [], timing_refs: null }],
    [{ id: "ingredient", name: " ", optional: false, quantity: null }],
  ])
    expect(hasGroupContent(rows)).toBe(false)
})

test("authored text, choices, and recipe details require confirmation", () => {
  for (const row of [
    { id: "ingredient", name: "Flour" },
    { id: "step", title: "Bake" },
    { id: "step", text: "Let cool." },
    { id: "ingredient", note: "For dusting" },
    { id: "ingredient", quantity: { kind: "simple", amount: { value: "0" } } },
    { id: "ingredient", alternatives: [{ id: "branch", name: "Butter" }] },
    { id: "step", ingredient_uses: [{ ingredient_id: "flour" }] },
    { id: "step", temperatures: [{ text: "A warm oven" }] },
    { id: "step", timing_refs: ["rest"] },
    { id: "step", image_refs: ["photo"] },
  ])
    expect(hasGroupContent([row])).toBe(true)
})
