import { expect, test } from "@playwright/test"

test("import history presents persisted diagnostics behind concise provenance", async ({
  page,
}) => {
  const title = "History warning cake"
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/import-history-warning.html")
  await page
    .getByRole("region", { name: "Review import" })
    .getByRole("button", { name: "Save recipe", exact: true })
    .click()
  await expect(page).toHaveURL(/\/recipes\/[^/]+$/)
  await expect(page.getByRole("heading", { name: title, level: 1 })).toBeVisible()
  const recipePath = new URL(page.url()).pathname

  await page.goto(`${recipePath}/history`)
  const history = page.getByRole("region", { name: "Import history" })
  await expect(history).toContainText("Imported from")
  await history.getByText("Details", { exact: true }).click()
  await expect(history).toContainText("Importer:")
  await expect(history).toContainText("could not be read")
  await expect(history.getByRole("link", { name: "View import job" })).toBeVisible()
})
