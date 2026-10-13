import { expect, test } from "@playwright/test"

import { seedBook, seedRecipe } from "./library"

test("cooking has one bounded composer with editable date/time, optional notes and safe cancellation", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const recipe = await seedRecipe(request, "Remembered supper")
  await page.goto(`/recipes/${recipe.id}`)
  const history = page.getByRole("region", { name: "Cooking history" })
  await expect(history).toContainText("Not cooked yet")
  await history.getByRole("button", { name: "Log cook", exact: true }).click()
  let dialog = page.getByRole("dialog", { name: "Log cook", exact: true })
  const note = dialog.getByRole("textbox", { name: "Cooking note (optional)" })
  await note.pressSequentially("Less salt next time")
  await expect(note).toBeFocused()
  await expect(note).toHaveValue("Less salt next time")
  await page.keyboard.press("Escape")
  await expect(dialog).not.toBeVisible()
  await expect(history.getByRole("button", { name: "Log cook", exact: true })).toBeFocused()
  expect(
    (await (await request.get(`/api/v1/recipes/${recipe.id}/cookings`)).json()).times_cooked,
  ).toBe(0)
  await history.getByRole("button", { name: "Log cook", exact: true }).click()
  dialog = page.getByRole("dialog", { name: "Log cook", exact: true })
  await dialog.getByLabel("Date", { exact: true }).fill("2026-08-30")
  await dialog.getByLabel("Time", { exact: true }).fill("18:45")
  // A failed save leaves the intended occasion intact.
  await page.route("**/cookings", async (route) => {
    if (route.request().method() === "POST") {
      await route.fulfill({
        status: 503,
        json: { title: "Unavailable", status: 503, detail: "Try again." },
      })
      return
    }
    await route.continue()
  })
  await dialog.getByRole("button", { name: "Save cook" }).click()
  await expect(dialog.getByRole("alert")).toBeVisible()
  await expect(dialog.getByLabel("Time", { exact: true })).toHaveValue("18:45")
  await page.unroute("**/cookings")
  await dialog.getByRole("button", { name: "Save cook" }).click()
  await expect(dialog).not.toBeVisible()
  await expect(history).toContainText("18:45")
  const data = await (await request.get(`/api/v1/recipes/${recipe.id}/cookings`)).json()
  expect(data.items[0].cooked_time).toBe("18:45")
  expect(data.items[0].note ?? "").toBe("")
  const current = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(current.headers().etag).toBe('"1"')
  await history.getByRole("button", { name: /Edit cook on/ }).click()
  dialog = page.getByRole("dialog", { name: "Edit cook", exact: true })
  await dialog.getByLabel("Cooking note (optional)").fill("A long cooking note. ".repeat(60))
  await dialog.getByRole("button", { name: "Save cook" }).click()
  await expect(dialog).not.toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)
})

test("organization stages searchable choices in a dialog and retains them after a failed save", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Organized supper")
  const book = await seedBook(request, "Family supper collection", [])
  for (let i = 0; i < 15; i++)
    await request.post("/api/v1/tags", {
      data: { name: `Kitchen label ${i}` },
    })
  await page.goto(`/recipes/${recipe.id}`)
  await page.getByRole("button", { name: "Organize", exact: true }).click()
  let dialog = page.getByRole("dialog", { name: "Organize recipe" })
  // What the recipe already carries is the dialog's answer; the rest of the
  // library's vocabulary waits behind the control that names it.
  await expect(dialog.getByText("No tags on this recipe.")).toBeVisible()
  await dialog.getByRole("button", { name: "Add a tag", exact: true }).click()
  const search = dialog.getByRole("searchbox", { name: "Search tags" })
  await search.fill("Kitchen label 12")
  await dialog.getByRole("button", { name: "Add the tag Kitchen label 12", exact: true }).click()
  await dialog.getByRole("button", { name: "Add to a book", exact: true }).click()
  const bookSearch = dialog.getByRole("searchbox", { name: "Search books", exact: true })
  await bookSearch.pressSequentially(book.title)
  await expect(bookSearch).toBeFocused()
  await expect(bookSearch).toHaveValue(book.title)
  await dialog
    .getByRole("button", { name: `Put this recipe in ${book.title}`, exact: true })
    .click()
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByRole("button", { name: "Organize", exact: true })).toBeFocused()
  expect(
    (await (await request.get(`/api/v1/recipes/${recipe.id}/organization`)).json()).tag_ids ?? [],
  ).toHaveLength(0)
  await page.getByRole("button", { name: "Organize", exact: true }).click()
  dialog = page.getByRole("dialog", { name: "Organize recipe" })
  await dialog.getByRole("button", { name: "Add a tag", exact: true }).click()
  await dialog.getByRole("searchbox", { name: "Search tags" }).fill("Kitchen label 12")
  await dialog.getByRole("button", { name: "Add the tag Kitchen label 12", exact: true }).click()
  await dialog.getByRole("button", { name: "Add to a book", exact: true }).click()
  await dialog.getByRole("searchbox", { name: "Search books", exact: true }).fill(book.title)
  await dialog
    .getByRole("button", { name: `Put this recipe in ${book.title}`, exact: true })
    .click()
  await page.route("**/organization", async (route) => {
    if (route.request().method() === "PUT") {
      await route.fulfill({
        status: 503,
        json: { title: "Unavailable", status: 503 },
      })
      return
    }
    await route.continue()
  })
  await dialog.getByRole("button", { name: "Save changes" }).click()
  await expect(dialog.getByRole("alert")).toBeVisible()
  await expect(
    dialog.getByRole("button", { name: "Remove the tag Kitchen label 12", exact: true }),
  ).toBeVisible()
  await page.unroute("**/organization")
  await dialog.getByRole("button", { name: "Save changes" }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.locator('[aria-label="Library details"]')).toContainText("Kitchen label 12")
  await expect(page.locator('[aria-label="Library details"]')).toContainText(book.title)
  await page.reload()
  await expect(page.getByText("Kitchen label 12", { exact: true })).toBeVisible()
})
