import { expect, test } from "@playwright/test"

import { seedBook, seedRecipe } from "./library"

/**
 * Adding recipes to a collection is performed directly from the book reader,
 * supports adding multiple recipes at once, and discards changes on cancel.
 */
test("recipes are added to a collection from the book itself", async ({ page, request }) => {
  const suffix = Math.random().toString(36).slice(2, 8)
  const held = await seedRecipe(request, `Collection held ${suffix}`)
  const first = await seedRecipe(request, `Collection wanted first ${suffix}`)
  const second = await seedRecipe(request, `Collection wanted second ${suffix}`)
  const book = await seedBook(request, `Collection weeknights ${suffix}`, [held.id])

  await page.goto(`/books/${book.id}`)
  await expect(page.getByText("1 recipe", { exact: true })).toBeVisible()

  // Cancelling leaves the book exactly as it was.
  await page.getByRole("button", { name: "Add recipes", exact: true }).click()
  const dialog = page.getByRole("dialog", { name: `Add recipes to ${book.title}` })
  await dialog.getByRole("searchbox", { name: "Search recipes to add" }).fill(first.title)
  await expect(dialog.getByRole("checkbox", { name: new RegExp(first.title) })).toBeVisible()
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click()
  await expect(page.getByText("1 recipe", { exact: true })).toBeVisible()

  // Two recipes, found separately, go in together.
  await page.getByRole("button", { name: "Add recipes", exact: true }).click()
  const search = dialog.getByRole("searchbox", { name: "Search recipes to add" })
  await search.fill(first.title)
  await dialog.getByRole("checkbox", { name: new RegExp(first.title) }).check()
  await search.fill(second.title)
  await dialog.getByRole("checkbox", { name: new RegExp(second.title) }).check()
  await expect(dialog.getByText("2 recipes chosen.")).toBeVisible()

  // Nothing about publishing a book stands between here and adding to it.
  await expect(dialog).not.toContainText("ISBN")
  await expect(dialog).not.toContainText("Publisher")
  await dialog.getByRole("button", { name: "Add 2 recipes", exact: true }).click()

  await expect(dialog).toHaveCount(0)
  await expect(page.getByRole("link", { name: first.title })).toBeVisible()
  await expect(page.getByRole("link", { name: second.title })).toBeVisible()
  await expect(page.getByText("3 recipes", { exact: true })).toBeVisible()

  // Saved, not merely staged: a reload shows the same three recipes.
  await page.reload()
  await expect(page.getByText("3 recipes", { exact: true })).toBeVisible()

  // A recipe the book already holds cannot be added to it twice.
  await page.getByRole("button", { name: "Add recipes", exact: true }).click()
  await dialog.getByRole("searchbox", { name: "Search recipes to add" }).fill(held.title)
  const already = dialog.getByRole("checkbox", { name: new RegExp(held.title) })
  await expect(already).toBeDisabled()
  await expect(already).toBeChecked()
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click()

  // Removing membership is named as leaving the book, not as deleting a recipe.
  await page.getByRole("button", { name: "Organize book", exact: true }).click()
  await expect(
    page.getByRole("button", { name: `Remove ${first.title} from book`, exact: true }),
  ).toBeVisible()
})
