import { expect, test } from "vitest"

import { displayName, neededText, rowNote, shoppingSections } from "./shopping-display"
import type { ShoppingItem } from "./shopping-store"

function item(
  name: string,
  extra: Partial<ShoppingItem> & { units?: [string, string, string?][] } = {},
): ShoppingItem {
  const { units = [], ...rest } = extra
  return {
    id: rest.id ?? name,
    name,
    buy: "",
    needed: "",
    note: "",
    category: "",
    bought: false,
    deleted: false,
    revision: 1,
    fields: {},
    sources: units.map(([amount, unit, note], index) => ({
      id: `${name}-${index}`,
      label: "Tacos",
      name,
      quantity: `${amount} ${unit}`,
      amount,
      unit,
      note,
    })),
    ...rest,
  }
}

test("display names start with a capital without changing the rest", () => {
  expect(displayName("lemons")).toBe("Lemons")
  expect(displayName("iceberg lettuce (US)")).toBe("Iceberg lettuce (US)")
  expect(displayName("istiridye", "tr")).toBe("İstiridye")
  expect(displayName("2 eggs")).toBe("2 eggs")
  expect(displayName("")).toBe("")
})

test("recipe preparation notes stay out of the row; notes written for the list stay in", () => {
  expect(
    rowNote(
      item("garlic", {
        note: "minced\nremove shoots\norganic",
        units: [["4", "count.clove", "minced\nremove shoots"]],
      }),
    ),
  ).toBe("organic")
  expect(rowNote(item("garlic", { note: "minced", units: [["4", "count.clove", "minced"]] }))).toBe(
    "",
  )
  expect(
    rowNote(
      item("onion", {
        note: "chopped\nthe red ones",
        units: [
          ["1", "count.item", "chopped"],
          ["1", "count.item", "sliced"],
        ],
      }),
    ),
  ).toBe("the red ones")
  expect(rowNote(item("bread", { note: "sourdough if they have it" }))).toBe(
    "sourdough if they have it",
  )
})

test("rows group by the section someone set, with unsorted rows last", () => {
  const sections = shoppingSections(
    [
      item("Milk"),
      item("napkins", { category: "Household" }),
      item("apples", { category: "Produce" }),
      item("soap", { category: " Household " }),
    ],
    "Other",
  )
  expect(sections.map((section) => [section.label, section.ids])).toEqual([
    ["Household", ["napkins", "soap"]],
    ["Produce", ["apples"]],
    ["Other", ["Milk"]],
  ])
})

test("each part of a combined total reads in kitchen notation", () => {
  expect(neededText("1 tbsp + 1/4 tsp")).toBe("1 tbsp + ¼ tsp")
  expect(neededText("5/2 tsp")).toBe("2½ tsp")
  expect(neededText("2 kg or 4 lb")).toBe("2 kg or 4 lb")
})

test("shopping totals use practical quantities while source wording stays intact", () => {
  expect(neededText("23/5 cloves")).toBe("≈ 5 cloves")
  expect(neededText("103/100 tsp")).toBe("≈ 1 tsp")
  expect(neededText("11/50 cup")).toBe("≈ 3½ tbsp")
  expect(neededText("2/3 cups")).toBe("⅔ cup")
  expect(neededText("1/4 kg")).toBe("250 g")
  expect(neededText("0")).toBe("0")
  expect(neededText("to taste")).toBe("to taste")
})

test("small volumes use smaller measures instead of inflating a minimum cup step", () => {
  expect(neededText("1/48 cup")).toBe("1 tsp")
  expect(neededText("1/96 cup")).toBe("½ tsp")
  expect(neededText("1/192 cup")).toBe("¼ tsp")
})
