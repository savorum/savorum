import { expect, test } from "@playwright/test"

test("a committed import remains discoverable after leaving and reloading its job page", async ({
  page,
}) => {
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/discovery-tart.json")
  const review = page.getByRole("region", { name: "Save this recipe" })
  await review.getByRole("button", { name: "Save recipe", exact: true }).click()
  await expect(page).toHaveURL(/\/imports\/[^/]+$/)
  const jobURL = page.url()
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()

  await page.getByRole("link", { name: "Recipes", exact: true }).click()
  await expect(page).toHaveURL("/")
  await page.getByRole("button", { name: "More", exact: true }).click()
  await page.getByRole("menuitem", { name: "Imports", exact: true }).click()
  await expect(page).toHaveURL("/imports")
  await expect(page.getByRole("link", { name: /discovery-tart\.json/ })).toBeVisible()
  await page.getByRole("link", { name: /discovery-tart\.json/ }).click()
  await expect(page).toHaveURL(jobURL)
  await page.reload()
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()
  await expect(page.getByRole("link", { name: "Import discovery tart" })).toBeVisible()
})

test("loading more imports and refreshing keeps one row per job", async ({ page }) => {
  let completed = false
  await page.route("**/api/v1/import-jobs?**", async (route) => {
    const url = new URL(route.request().url())
    const limit = Number(url.searchParams.get("limit"))
    const offset = Number(url.searchParams.get("offset"))
    expect(limit).toBeLessThanOrEqual(100)
    const items = Array.from({ length: Math.min(limit, 130 - offset) }, (_, index) => {
      const position = offset + index
      return {
        id: `job-${position + 1}`,
        importer: "SREF",
        source: `recipe-${position + 1}.json`,
        state: position === 0 && !completed ? "running" : "completed",
        on_duplicate: "skip",
        total_items: 1,
        created_at: new Date(2026, 0, 1 + position).toISOString(),
        progress: {
          total: 1,
          pending: 0,
          running: 0,
          imported: 1,
          skipped: 0,
          failed: 0,
          cancelled: 0,
          review_required: 0,
          informational_preservation: 0,
          without_diagnostics: 1,
          diagnostic_occurrences: 0,
          diagnostics: [],
          finished: 1,
          current_batch: 1,
          total_batches: 1,
        },
      }
    })
    await route.fulfill({ json: { items, total: 130, limit, offset } })
  })
  await page.goto("/imports")
  await expect(page.getByRole("link", { name: /recipe-1\.json/ })).toBeVisible()
  for (const count of [50, 75, 100, 125, 130]) {
    await page.getByRole("button", { name: "Load more imports" }).click()
    await expect(page.locator('ul[aria-label="Import jobs"] > li')).toHaveCount(count)
  }
  await expect(page.getByRole("link", { name: /recipe-130\.json/ })).toBeVisible()
  expect(await page.locator('ul[aria-label="Import jobs"] > li').count()).toBe(130)
  expect(
    await page
      .locator('ul[aria-label="Import jobs"] a')
      .evaluateAll((links) => new Set(links.map((link) => link.getAttribute("href"))).size),
  ).toBe(130)

  const refresh = page.getByRole("button", { name: "Refresh imports" })
  const firstLink = page.locator('ul[aria-label="Import jobs"] a').first()
  await expect(firstLink).toContainText("Importing")
  await firstLink.evaluate((node) => node.setAttribute("data-identity-check", "stable-job-link"))
  completed = true
  await refresh.focus()
  await refresh.click()
  await expect(refresh).toBeFocused()
  await expect(page.locator('ul[aria-label="Import jobs"] > li')).toHaveCount(130)
  await expect(page.locator('ul[aria-label="Import jobs"] a').first()).toContainText("imported")
  await expect(page.locator('ul[aria-label="Import jobs"] a').first()).toHaveAttribute(
    "data-identity-check",
    "stable-job-link",
  )
})

test("a slow final batch keeps elapsed time without an indefinite estimate", async ({ page }) => {
  await page.clock.install({ time: new Date("2026-09-04T12:00:00Z") })
  let finished = 0
  await page.route("**/api/v1/import-jobs/slow-final**", async (route) => {
    if (new URL(route.request().url()).pathname.endsWith("/items")) {
      await route.fulfill({ json: { items: [], total: 0, limit: 100, offset: 0 } })
      return
    }
    await route.fulfill({
      json: {
        id: "slow-final",
        source: "CookBook.zip",
        importer: "CookBook",
        state: "running",
        created_at: "2026-09-04T12:00:00Z",
        started_at: "2026-09-04T12:00:00Z",
        on_duplicate: "skip",
        total_items: 124,
        progress: {
          total: 124,
          finished,
          pending: 124 - finished,
          running: 1,
          imported: finished,
          skipped: 0,
          failed: 0,
          cancelled: 0,
          review_required: 0,
          informational_preservation: 0,
          without_diagnostics: finished,
          diagnostic_occurrences: 0,
          diagnostics: [],
          current_batch: finished ? 5 : 1,
          total_batches: 5,
        },
      },
    })
  })
  await page.goto("/imports/slow-final")
  await expect(page.getByText("Elapsed 0 sec", { exact: true })).toBeVisible()
  await expect(page.getByText(/calculating|measuring processing rate/i)).toHaveCount(0)
  await page.clock.runFor(4_000)
  finished = 100
  await page.clock.runFor(1_000)
  await expect(page.getByText("Batch 5 of 5", { exact: true })).toBeVisible()
  await expect(page.getByText(/about .* left/)).toBeVisible()
  await page.clock.runFor(60_000)
  await expect(page.getByText(/about .* left/)).toHaveCount(0)
  await expect(page.getByText(/^Elapsed 1 min(?: \d+ sec)?$/)).toBeVisible()
  await expect(page.getByRole("heading", { name: "Importing 100 of 124 recipes" })).toBeVisible()
})

test("the bar is the places a cook reads from and the rest is grouped by what it is for", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto("/")
  const primary = page.getByRole("navigation", { name: "Primary" })
  for (const name of ["Recipes", "Plan", "Shopping"])
    await expect(primary.getByRole("link", { name, exact: true })).toBeVisible()
  await expect(page.getByRole("link", { name: "Books", exact: true })).toBeVisible()
  // Imports is a place, but it is one you visit while something is coming in.
  await expect(primary.getByRole("link", { name: "Imports", exact: true })).toHaveCount(0)
  await page.getByRole("button", { name: "Add recipes", exact: true }).click()
  const menu = page.getByRole("menu")
  await expect(menu.getByRole("group", { name: "Bring recipes in", exact: true })).toBeVisible()
  for (const name of ["From a file", "From a website", "From text", "Recent imports"])
    await expect(menu.getByRole("menuitem", { name, exact: true })).toBeVisible()
  await expect(menu).not.toContainText("Compare")
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)
  await menu.getByRole("menuitem", { name: "Recent imports", exact: true }).click()
  await expect(page).toHaveURL("/imports")

  await primary.getByRole("link", { name: "Recipes", exact: true }).click()
  await expect(page).toHaveURL("/")
  await page.getByRole("button", { name: "More", exact: true }).click()
  const more = page.getByRole("menu")
  // Comparison needs two recipes, so it is offered where they are selected.
  await expect(more).not.toContainText("Compare")
  for (const name of ["Imports", "Tags and categories", "Saved searches", "Trash"])
    await expect(more.getByRole("menuitem", { name, exact: true })).toBeVisible()
  for (const name of ["Preferences", "Your account"])
    await expect(more.getByRole("menuitem", { name, exact: true })).toBeVisible()
  await expect(more.getByRole("group", { name: "Your library", exact: true })).toBeVisible()
  await expect(more.getByRole("group", { name: "You", exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)
  await more.getByRole("menuitem", { name: "Imports", exact: true }).click()
  await expect(page).toHaveURL("/imports")
})
