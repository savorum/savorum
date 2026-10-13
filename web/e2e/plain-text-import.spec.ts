import { expect, test, type Page } from "@playwright/test"

async function openTextImport(page: Page) {
  await page.getByRole("button", { name: "Add recipes" }).click()
  await page.getByRole("menuitem", { name: /From text/ }).click()
  return page.getByRole("textbox", { name: "Paste or edit recipe text" })
}

test("paste remains editable through caret and composition before commit", async ({
  page,
  request,
}) => {
  await page.goto("/")
  const editor = await openTextImport(page)
  await editor.fill(`Draft soup\n\nIngredients\n2 grams flour\n\nInstructions\nStir well.`)
  await editor.focus()
  await editor.pressSequentially("\nNote: family recipe")
  await expect(editor).toBeFocused()
  await expect(editor).toHaveValue(/Note: family recipe/)
  await editor.fill(
    `Corrected soup\n\nIngredients\n2 grams flour\n1 tsp salt\n\nInstructions\nStir well.\nServe warm.\n\nNote: family recipe`,
  )
  await page.getByRole("button", { name: "Preview pasted recipe" }).click()
  const review = page.getByRole("region", { name: "Save this recipe" })
  await expect(review).toContainText("Corrected soup")
  await review.getByRole("button", { name: "Save recipe", exact: true }).click()
  await expect(page).toHaveURL(/\/imports\//)
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()
  const link = page.getByRole("link", { name: "Corrected soup" })
  const href = await link.getAttribute("href")
  expect(href).toMatch(/^\/recipes\//)
  const stored = await request.get(`/api/v1${href}`)
  expect(stored.ok()).toBe(true)
  const body = await stored.json()
  expect(
    body.recipe.ingredient_sections[0].ingredients.map(
      (item: { source_text: string }) => item.source_text,
    ),
  ).toEqual(["2 grams flour", "1 tsp salt"])
  expect(
    body.recipe.instruction_sections[0].steps.map((step: { text: string }) => step.text),
  ).toEqual(["Stir well.", "Serve warm."])
  expect(body.recipe.notes).toContain("Note: family recipe")
})

test(".txt files load into the editable text surface before preview", async ({ page }) => {
  await page.goto("/")
  const editor = await openTextImport(page)
  await page
    .getByRole("dialog", { name: "Import recipe text" })
    .locator('input[type="file"][accept*=".txt"]')
    .setInputFiles("e2e/fixtures/plain-file.txt")
  await expect(editor).toHaveValue(/File soup/)
  await editor.fill(`Corrected file soup\n\nIngredients\n3 grams flour\n\nInstructions\nStir.`)
  await page.getByRole("button", { name: "Preview pasted recipe" }).click()
  await expect(page.getByRole("region", { name: "Save this recipe" })).toContainText(
    "Corrected file soup",
  )
})
