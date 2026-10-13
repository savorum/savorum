import { expect, test } from "@playwright/test"

test("import history presents persisted diagnostics behind concise provenance", async ({
  page,
}) => {
  const title = "History warning cake"
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/import-history-warning.html")
  await page
    .getByRole("region", { name: "Save this recipe" })
    .getByRole("button", { name: "Save recipe", exact: true })
    .click()
  await expect(page.getByRole("link", { name: title })).toBeVisible()
  const recipePath = await page.getByRole("link", { name: title }).getAttribute("href")
  expect(recipePath).toMatch(/^\/recipes\//)

  await page.goto(`${recipePath}/history`)
  const history = page.getByRole("region", { name: "Import history" })
  await expect(history).toContainText("Imported from")
  await history.getByText("Details", { exact: true }).click()
  await expect(history).toContainText("Importer:")
  await expect(history).toContainText("could not be read")
  await expect(history.getByRole("link", { name: "View import job" })).toBeVisible()
})
