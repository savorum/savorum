import { expect, test } from "@playwright/test"

test("retains an uploaded original only when the reviewer opts in", async ({ page, request }) => {
  await page.goto("/")
  await page
    .locator('input[type="file"]')
    .setInputFiles("e2e/fixtures/source-retention-original.pdf")
  const review = page.getByRole("region", { name: "Save this recipe" })
  const candidate = review.locator("li").filter({ hasText: "Retained source tart" })
  const retention = candidate.getByRole("checkbox", { name: /Keep original/ })
  await expect(retention).not.toBeChecked()
  await retention.check()
  await expect(retention).toBeChecked()
  await review.getByRole("button", { name: "Save recipe", exact: true }).click()
  await expect(page).toHaveURL(/\/imports\//)
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()

  const recipe = page.getByRole("link", { name: "Retained source tart" })
  const recipePath = await recipe.getAttribute("href")
  expect(recipePath).toMatch(/^\/recipes\//)
  await page.goto(`${recipePath}/history`)
  await page.getByRole("button", { name: /Current/ }).click()
  const original = page.getByRole("link", { name: "source-retention-original.pdf" })
  await expect(original).toBeVisible()
  const download = await request.get(
    new URL((await original.getAttribute("href")) ?? "", page.url()).toString(),
  )
  expect(download.ok()).toBe(true)
  expect(await download.text()).toContain("Retained source tart")
})

test("leaves original uploads out of history by default", async ({ page }) => {
  await page.goto("/")
  await page
    .locator('input[type="file"]')
    .setInputFiles("e2e/fixtures/source-retention-default.pdf")
  const review = page.getByRole("region", { name: "Save this recipe" })
  const candidate = review.locator("li").filter({ hasText: "Unretained fennel pot" })
  await expect(candidate.getByRole("checkbox", { name: /Keep original/ })).not.toBeChecked()
  await review.getByRole("button", { name: "Save recipe", exact: true }).click()
  await expect(page).toHaveURL(/\/imports\//)
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()
  const recipe = page.getByRole("link", { name: "Unretained fennel pot" })
  await page.goto(`${await recipe.getAttribute("href")}/history`)
  await page.getByRole("button", { name: /Current/ }).click()
  await expect(
    page.getByRole("heading", { name: "Original files kept with this version" }),
  ).toHaveCount(0)
})

test("lets a reviewer correct extracted text before committing it", async ({ page }) => {
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/source-edit.pdf")
  const review = page.getByRole("region", { name: "Save this recipe" })
  const candidate = review.locator("li").filter({ hasText: "Extracted card soup" })
  await candidate.getByRole("button", { name: "Edit recipe text" }).click()
  const editor = page.getByRole("textbox", { name: "Paste or edit recipe text" })
  await expect(editor).toHaveValue(/Extracted card soup/)
  await editor.evaluate((node) => node.setAttribute("data-identity-marker", "stable"))
  await editor.fill(
    "Corrected extracted soup\n\nIngredients\n3 grams flour\n\nInstructions\nStir twice.",
  )
  await expect(editor).toHaveAttribute("data-identity-marker", "stable")
  await page.getByRole("button", { name: "Update preview" }).click()
  await expect(review).toContainText("Corrected extracted soup")
})
