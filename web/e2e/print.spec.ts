import { expect, test } from "@playwright/test"

import { choose, chooser } from "./controls"
import { seedRecipe } from "./library"

/** The preview draws the real document, so a page is a canvas rather than markup. */
const previewPages = (page: import("@playwright/test").Page) => page.locator("main canvas")

/**
 * The document engine is prepared once per process, and the first document of
 * a fresh server pays for it. Doing that here keeps the wait out of whichever
 * test happens to run first.
 */
test.beforeAll(async ({ playwright, baseURL }) => {
  const api = await playwright.request.newContext({ baseURL })
  const recipe = await seedRecipe(api, "Renderer warm-up")
  const warmed = await api.post("/api/v1/print/documents", {
    data: { recipes: [recipe.id] },
    timeout: 120_000,
  })
  if (!warmed.ok()) throw new Error(`could not warm the renderer: ${warmed.status()}`)
  await api.dispose()
})

test("a recipe is printed from a print view of its own", async ({ page, request }) => {
  const recipe = await seedRecipe(request, "Printable weeknight recipe")

  await page.goto(`/recipes/${recipe.id}`)
  await expect(page.getByRole("heading", { name: recipe.title })).toBeVisible()

  await page.getByRole("link", { name: "Print" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}/print`))
  await expect(page.getByRole("heading", { name: `Print ${recipe.title}` })).toBeVisible()
  await expect(previewPages(page)).toHaveCount(1)

  // It is a route, not a dialogue: going back returns to the recipe.
  await page.goBack()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
})

test("obsolete PDF work is cancelled during rapid zoom and route exit", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Disposable preview work")
  const errors: Error[] = []
  page.on("pageerror", (error) => errors.push(error))

  await page.goto(`/recipes/${recipe.id}/print`)
  await expect(previewPages(page)).toHaveCount(1)
  const larger = page.getByRole("button", { name: "Show larger" })
  for (let attempt = 0; attempt < 4; attempt++) await larger.click()

  await page.goto("/")
  await expect(page.getByRole("heading", { name: "Recipes" })).toBeVisible()
  // Errors from the abandoned preview surface when its pending frames and
  // promise turns drain, so flush a few of each rather than guessing a delay.
  await page.evaluate(async () => {
    for (let turn = 0; turn < 3; turn++) {
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))
      await new Promise<void>((resolve) => setTimeout(resolve, 0))
    }
  })
  expect(errors).toEqual([])
})

test("long PDF previews keep offscreen pages as bounded placeholders", async ({
  page,
  request,
}) => {
  const recipes = []
  for (let number = 1; number <= 8; number++) {
    recipes.push(await seedRecipe(request, `Virtual page ${number}`))
  }
  await page.goto(`/print?recipes=${recipes.map((recipe) => recipe.id).join(",")}`)

  const pageSlots = page.locator("[data-pdf-page]")
  await expect(pageSlots).toHaveCount(8)
  await expect(pageSlots.first().locator("canvas")).toBeVisible()
  expect(await previewPages(page).count()).toBeLessThanOrEqual(4)
  await expect(pageSlots.locator("text=Page").first()).toBeAttached()

  await pageSlots.last().scrollIntoViewIfNeeded()
  await expect(pageSlots.last().locator("canvas")).toBeVisible()
  expect(await previewPages(page).count()).toBeLessThanOrEqual(4)
})

test("choosing cards changes the document rather than the description of it", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Card format recipe")
  await page.goto(`/recipes/${recipe.id}/print`)
  await expect(previewPages(page)).toHaveCount(1)
  const pageSize = await previewPages(page).first().boundingBox()

  await page.getByRole("button", { name: "Recipe cards" }).click()
  await expect(page).toHaveURL(/size=card-4x6/)
  await expect(chooser(page, "Card size")).toHaveText("4 × 6 in")

  // A card is wider than it is tall; a page is not.
  await expect
    .poll(async () => {
      const box = await previewPages(page).first().boundingBox()
      return box ? box.width > box.height : false
    })
    .toBe(true)

  await choose(chooser(page, "Card size"), "card-3x5")
  await expect(page).toHaveURL(/size=card-3x5/)
  await expect
    .poll(async () => {
      const box = await previewPages(page).first().boundingBox()
      return box && pageSize ? Math.round(box.width) !== Math.round(pageSize.width) : false
    })
    .toBe(true)
})

test("the batch on screen carries into the print without changing the recipe", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Scaled batch recipe")
  await page.goto(`/recipes/${recipe.id}`)

  await page.getByRole("button", { name: "One more serving" }).click()
  await page.getByRole("button", { name: "One more serving" }).click()
  await page.getByRole("button", { name: "One more serving" }).click()
  await page.getByRole("button", { name: "One more serving" }).click()
  await expect(page.getByRole("textbox", { name: "How many servings" })).toHaveValue("8")

  await page.getByRole("link", { name: "Print" }).click()
  await expect(page).toHaveURL(/batch=%222%22/)
  await expect(page.getByRole("combobox", { name: "Batch to print" })).toContainText("(2×)")

  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  const body = (await stored.json()) as {
    recipe: {
      ingredient_sections: { ingredients: { quantity: { amount: { value: string } } }[] }[]
    }
  }
  expect(body.recipe.ingredient_sections[0].ingredients[0].quantity.amount.value).toBe("1/2")
  expect(stored.headers().etag).toBe('"1"')
})

test("several recipes are chosen in the library and printed together", async ({
  page,
  request,
}) => {
  const first = await seedRecipe(request, "Bulk print one")
  const second = await seedRecipe(request, "Bulk print two")

  await page.goto("/")
  await page.getByRole("button", { name: "Select" }).click()
  await page.getByRole("button", { name: new RegExp(first.title) }).click()
  await page.getByRole("button", { name: new RegExp(second.title) }).click()
  await expect(page.getByText("2 selected")).toBeVisible()

  await page.getByRole("link", { name: "Print" }).click()
  await expect(page).toHaveURL(/\/print\?/)
  await expect(page.getByRole("heading", { name: "Print 2 recipes" })).toBeVisible()
  await expect(previewPages(page).first()).toBeVisible()
})

test("a failure is reported in the view rather than in a message that disappears", async ({
  page,
  request,
}) => {
  await page.goto("/print?recipes=rnothinglikethis")
  await expect(page.getByRole("alert")).toContainText("does not exist")
  await expect(page.getByRole("button", { name: "Print" })).toBeDisabled()

  // Recovering means choosing something that exists, not reloading the page.
  const recipe = await seedRecipe(request, "Recovered recipe")
  await page.goto(`/print?recipes=${recipe.id}`)
  await expect(page.getByRole("alert")).toHaveCount(0)
  await expect(previewPages(page).first()).toBeVisible()
})
