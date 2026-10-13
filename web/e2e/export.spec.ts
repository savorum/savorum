import { type Page, expect, test } from "@playwright/test"

import { seedRecipe } from "./library"

/**
 * Browser interactions and failure presentation for export. Archive contents
 * and semantics are covered by the Go service and API tests.
 */

/** On a recipe page, export lives in the overflow menu. */
async function chooseRecipeExport(page: Page) {
  const saved = page.waitForEvent("download")
  await page.getByRole("button", { name: "More actions" }).click()
  await page.getByRole("menuitem", { name: "Export as SREF" }).click()
  return saved
}

test("a recipe is exported as SREF from the recipe page", async ({ page, request }) => {
  const recipe = await seedRecipe(request, "Exportable weeknight recipe")

  await page.goto(`/recipes/${recipe.id}`)
  await expect(page.getByRole("heading", { name: recipe.title })).toBeVisible()

  const download = await chooseRecipeExport(page)
  expect(download.suggestedFilename()).toBe("exportable-weeknight-recipe.sref")

  // Choosing a format is the whole interaction: nothing is configurable, so
  // the reader is not taken anywhere and no dialogue opens.
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  await expect(page.getByRole("dialog")).toHaveCount(0)
})

test("the export menu is reachable and operable from the keyboard", async ({ page, request }) => {
  const recipe = await seedRecipe(request, "Keyboard exportable recipe")
  await page.goto(`/recipes/${recipe.id}`)

  const trigger = page.getByRole("button", { name: "More actions" })
  await trigger.focus()
  await expect(trigger).toBeFocused()

  const saved = page.waitForEvent("download")
  await page.keyboard.press("Enter")
  const item = page.getByRole("menuitem", { name: "Export as SREF" })
  await expect(item).toBeVisible()
  await item.click()
  const download = await saved
  expect(download.suggestedFilename()).toBe("keyboard-exportable-recipe.sref")
})

test("recipes chosen in the library are exported as one bundle", async ({ page, request }) => {
  const first = await seedRecipe(request, "Bundled first recipe")
  const second = await seedRecipe(request, "Bundled second recipe")

  await page.goto("/")
  await page.getByRole("button", { name: "Select" }).click()
  await page.getByRole("button", { name: new RegExp(first.title) }).click()
  await page.getByRole("button", { name: new RegExp(second.title) }).click()
  await expect(page.getByText("2 selected")).toBeVisible()

  const saved = page.waitForEvent("download")
  await page
    .getByRole("region", { name: "Selected recipes" })
    .getByRole("button", { name: "Export" })
    .click()
  await page.getByRole("menuitem", { name: "SREF Bundle" }).click()
  const download = await saved
  expect(download.suggestedFilename()).toBe("recipes.srefbundle")

  // The library is untouched: exporting is a read.
  await page.getByRole("button", { name: "Done" }).click()
  await expect(page.getByRole("link", { name: first.title })).toBeVisible()
})

test("a recipe in the trash can still be kept before it is deleted permanently", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Discarded but not yet gone")
  const loaded = await request.get(`/api/v1/recipes/${recipe.id}`)
  const trashed = await request.delete(`/api/v1/recipes/${recipe.id}`, {
    headers: { "If-Match": loaded.headers().etag },
  })
  expect(trashed.ok()).toBe(true)

  await page.goto("/trash")
  const row = page.getByRole("listitem").filter({ hasText: recipe.title })
  await expect(row).toBeVisible()
  await expect(row.getByRole("button", { name: "Restore", exact: true })).toBeVisible()
  await expect(row.getByRole("button", { name: "Delete permanently", exact: true })).toBeVisible()

  const saved = page.waitForEvent("download")
  await row.getByRole("button", { name: "Export" }).click()
  await page.getByRole("menuitem", { name: "SREF" }).click()
  expect((await saved).suggestedFilename()).toBe("discarded-but-not-yet-gone.sref")
})

test("an export that cannot be completed explains itself on the page", async ({
  page,
  request,
}) => {
  // A recipe naming a photograph this library holds no bytes for. Export must
  // refuse rather than quietly hand over a recipe missing its image.
  const recipe = await seedRecipe(request, "Recipe with a lost photograph", {
    assets: [
      {
        id: "hero",
        path: "assets/hero.jpg",
        media_type: "image/jpeg",
        sha256: "a".repeat(64),
        size: 22,
        role: "image",
      },
    ],
    image_refs: ["hero"],
    primary_image: "hero",
  })

  await page.goto(`/recipes/${recipe.id}`)
  await page.getByRole("button", { name: "More actions" }).click()
  await page.getByRole("menuitem", { name: "Export as SREF" }).click()

  // The menu closes as it is chosen, so a failure has to be reported on the
  // page rather than on the control that started it.
  const failure = page.getByRole("alert")
  await expect(failure).toBeVisible()
  await expect(failure).toContainText("Recipe with a lost photograph")
  // A reader is told what went wrong, not shown the response body.
  await expect(failure).not.toContainText("{")
  await expect(failure).not.toContainText("urn:savorum")
  // And they are still on the recipe rather than on a dead page.
  await expect(page.getByRole("heading", { name: recipe.title })).toBeVisible()
})

test("a failed selection export is reported beside the library action", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Selected recipe with a lost photograph", {
    assets: [
      {
        id: "hero",
        path: "assets/hero.jpg",
        media_type: "image/jpeg",
        sha256: "b".repeat(64),
        size: 22,
        role: "image",
      },
    ],
    image_refs: ["hero"],
    primary_image: "hero",
  })

  await page.goto("/")
  await page.getByRole("button", { name: "Select" }).click()
  await page.getByRole("button", { name: new RegExp(recipe.title) }).click()
  const selected = page.getByRole("region", { name: "Selected recipes" })
  await selected.getByRole("button", { name: "Export" }).click()
  await page.getByRole("menuitem", { name: "SREF Bundle" }).click()

  const failure = selected.getByRole("alert")
  await expect(failure).toBeVisible()
  await expect(failure).toContainText(recipe.title)
  await expect(page).toHaveURL("/")
})
