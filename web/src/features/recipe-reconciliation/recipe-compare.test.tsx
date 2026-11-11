import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import type { LibraryEntry, Recipe } from "@/api/client"
import type { RecipeMergePreview } from "@/api/recipe-reconciliation"

import { RecipeCombine, RecipeComparison } from "./recipe-compare"

vi.mock("@/components/recipe-article", () => ({ RecipeArticle: () => null }))

afterEach(cleanup)

function entry(id: string, title: string, ingredient: string): LibraryEntry {
  const recipe = {
    id: `${id}-document`,
    schema_version: 1,
    title,
    ingredient_sections: [{ id: "s", ingredients: [{ id: "i", name: ingredient }] }],
    instruction_sections: [],
  } as Recipe
  return { id, revision: 1, recipe } as LibraryEntry
}

const left = entry("left", "Stew", "beef")
const right = entry("right", "Hearty stew", "lamb")
const preview: RecipeMergePreview = {
  left,
  right,
  suggested: left.recipe,
  conflicts: [
    {
      path: "ingredient_sections",
      left: left.recipe.ingredient_sections,
      right: right.recipe.ingredient_sections,
    },
    { path: "title", left: left.recipe.title, right: right.recipe.title },
  ],
}

function combine() {
  const merge = vi.fn()
  const [survivor, setSurvivor] = createSignal<"left" | "right">("left")
  const screen = render(() => (
    <RecipeCombine
      preview={preview}
      survivor={survivor()}
      onSurvivorChange={setSurvivor}
      onMerge={merge}
    />
  ))
  return { merge, screen }
}

test("every part follows the kept recipe until the other version is chosen", () => {
  const { merge, screen } = combine()
  const headings = screen
    .getAllByRole("heading", { level: 2 })
    .map((heading) => heading.textContent)
  expect(headings).toEqual([
    "Which recipe do you keep?",
    "Title",
    "Ingredients",
    "What combining does",
  ])

  fireEvent.click(screen.getByRole("button", { name: "Combine recipes" }))
  expect(merge.mock.calls[0][0]).toMatchObject({ title: "Stew" })

  const ingredients = screen.getByRole("group", { name: "Ingredients" })
  fireEvent.click(ingredients.querySelectorAll("input")[1]!)
  expect(screen.getByText(/Ingredients: from .*Hearty stew/)).toBeTruthy()

  fireEvent.click(screen.getByRole("button", { name: "Combine recipes" }))
  expect(merge.mock.calls[1][0].title).toBe("Stew")
  expect(merge.mock.calls[1][0].ingredient_sections[0].ingredients[0].name).toBe("lamb")
})

test("keeping the other recipe takes all of its parts and names its title", () => {
  const { merge, screen } = combine()
  const survivor = screen.getByRole("group", { name: /Which recipe do you keep/ })
  fireEvent.click(survivor.querySelectorAll("input")[1]!)

  expect(screen.getByText(/Hearty stew.* stays in the library/)).toBeTruthy()
  expect(screen.getByText(/Stew.* moves to Trash/)).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Combine recipes" }))
  expect(merge.mock.calls[0][0]).toMatchObject({ title: "Hearty stew" })
})

test("the comparison offers to keep two different recipes", () => {
  const keepBoth = vi.fn()
  const screen = render(() => (
    <RecipeComparison
      left={left}
      right={entry("cake", "Lemon cake", "lemon")}
      onCombine={() => {}}
      onKeepBoth={keepBoth}
    />
  ))
  expect(screen.getByText("Likely different recipes.")).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Keep both" }))
  expect(keepBoth).toHaveBeenCalled()
})
