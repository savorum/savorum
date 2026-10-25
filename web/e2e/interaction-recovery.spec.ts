import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "@playwright/test"

import { settleAnimations } from "./controls"
import { seedRecipe } from "./library"

for (const width of [390, 768]) {
  test(`the meal tray shortcut moves keyboard focus at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 844 })
    await page.goto("/plan")
    const shortcut = page.getByRole("button", { name: "Meal tray", exact: true })
    await expect(shortcut).toBeInViewport()
    await page.screenshot({ path: testInfo.outputPath("plan-shortcut.png") })
    await shortcut.focus()
    await page.keyboard.press("Enter")
    await expect(page.getByRole("heading", { name: "Meal tray", exact: true })).toBeFocused()
    await page.screenshot({ path: testInfo.outputPath("plan-tray.png") })
    await page.keyboard.press("Tab")
    await expect(page.getByRole("button", { name: "Unscheduled", exact: true })).toBeFocused()
    await page.keyboard.press("Tab")
    await page.keyboard.press("Enter")
    await expect(page.getByRole("button", { name: "Saved meals", exact: true })).toHaveAttribute(
      "aria-pressed",
      "true",
    )
  })
}

test.describe("tablet calendar touch targets", () => {
  test.use({ hasTouch: true, viewport: { width: 768, height: 1024 } })
  test("month dates expose a full touch target", async ({ page }) => {
    await page.goto("/plan?view=month")
    const dates = page.getByRole("table").getByRole("button", { name: /Open the week of/ })
    await expect(dates.first()).toBeVisible()
    expect(await dates.count()).toBeGreaterThan(0)
    for (const date of await dates.all()) {
      const box = await date.boundingBox()
      expect(box?.width).toBeGreaterThanOrEqual(44)
      expect(box?.height).toBeGreaterThanOrEqual(44)
    }
  })
})

test("meal dialogs and month previews support keyboard entry, escape and focus return", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Keyboard preview supper")
  const today = new Date().toLocaleDateString("en-CA")
  expect(
    (
      await request.post("/api/v1/meals", {
        data: {
          date: today,
          meal_type: "dinner",
          dishes: [{ entry_id: recipe.id, display_name: recipe.title }],
        },
      })
    ).ok(),
  ).toBe(true)
  await page.goto(`/plan?view=month&date=${today}`)
  const meal = page
    .getByRole("table")
    .getByRole("button", { name: `Dinner: ${recipe.title}`, exact: true })
  await meal.focus()
  await page.keyboard.press("Enter")
  const preview = page.getByRole("dialog")
  await expect(preview.getByText(recipe.title, { exact: true })).toBeVisible()
  await expect(preview.getByRole("button", { name: "Edit", exact: true })).toBeFocused()
  await settleAnimations(page)
  expect(
    (await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa"]).analyze())
      .violations,
  ).toEqual([])
  await page.keyboard.press("Escape")
  await expect(preview).toBeHidden()
  await expect(meal).toBeFocused()
  const newMeal = page.getByRole("button", { name: "Plan a meal", exact: true })
  await newMeal.focus()
  await page.keyboard.press("Enter")
  const dialog = page.getByRole("dialog", { name: "Plan a meal", exact: true })
  await expect(dialog).toBeVisible()
  for (let index = 0; index < 25; index++) {
    await page.keyboard.press(index < 15 ? "Tab" : "Shift+Tab")
    expect(await dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true)
  }
  expect(
    (await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa"]).analyze())
      .violations,
  ).toEqual([])
  await page.keyboard.press("Escape")
  await expect(dialog).toBeHidden()
  await expect(newMeal).toBeFocused()
})

test("a failed plan read stays honest and can be retried without navigation", async ({ page }) => {
  let failing = true
  await page.route("**/api/v1/meals?*", (route) =>
    failing
      ? route.fulfill({
          status: 503,
          contentType: "application/problem+json",
          body: JSON.stringify({ title: "Unavailable", detail: "The plan is not reachable." }),
        })
      : route.continue(),
  )
  await page.goto("/plan")
  await expect(page.getByRole("alert").first()).toBeVisible()
  await expect(page.getByText("No unscheduled meals.", { exact: true })).toHaveCount(0)
  failing = false
  await page.getByRole("button", { name: "Try again", exact: true }).first().click()
  await expect(page.getByRole("alert")).toHaveCount(0)
  await expect(page.getByRole("heading", { name: "Meal tray", exact: true })).toBeVisible()
})

test.describe("offline recipe reading", () => {
  test.use({ serviceWorkers: "allow" })
  test("cached recipes remain available and an offline reload recovers after reconnecting", async ({
    page,
    context,
    request,
  }) => {
    const recipe = await seedRecipe(request, "Offline kitchen supper")
    const other = await seedRecipe(request, "Unvisited offline supper")
    await page.goto("/")
    await expect
      .poll(() =>
        page.evaluate(async () => {
          await navigator.serviceWorker.ready
          return (await (await caches.open("savorum-scope-v1")).keys()).length
        }),
      )
      .toBeGreaterThan(0)
    await page.goto(`/recipes/${recipe.id}`)
    await expect(page.getByRole("heading", { name: recipe.title, level: 1 })).toBeVisible()
    await expect
      .poll(() =>
        page.evaluate(async (id) => {
          const keys = (await caches.keys()).filter((key) => key.startsWith("savorum-data-"))
          for (const key of keys)
            if (await (await caches.open(key)).match(`/api/v1/recipes/${id}`)) return true
          return false
        }, recipe.id),
      )
      .toBe(true)
    await context.setOffline(true)
    const cached = await page.evaluate(async (id) => {
      const result = await fetch(`/api/v1/recipes/${id}`)
      return { status: result.status, text: await result.text() }
    }, recipe.id)
    expect(cached.status).toBe(200)
    expect(cached.text).toContain(recipe.title)
    await page.reload()
    await expect(page.getByRole("alert")).toContainText("Savorum could not be reached")
    const response = await page.evaluate(async (id) => {
      const result = await fetch(`/api/v1/recipes/${id}`)
      return { status: result.status, text: await result.text() }
    }, other.id)
    expect(response.status).toBe(503)
    expect(response.text).not.toContain(other.title)
    await context.setOffline(false)
    await page.getByRole("button", { name: "Retry", exact: true }).click()
    await expect(page.getByRole("heading", { name: recipe.title, level: 1 })).toBeVisible()
  })
})

test("completed import timings are optional keyboard-accessible details", async ({
  page,
}, testInfo) => {
  const started = new Date(Date.now() - 12000).toISOString()
  await page.route("**/api/v1/import-jobs/timing", (route) =>
    route.fulfill({
      json: {
        id: "timing",
        lead_title: "Timing import",
        importer: "SREF",
        source: "recipes.json",
        state: "completed",
        on_duplicate: "skip",
        total_items: 2,
        created_at: started,
        started_at: started,
        finished_at: new Date().toISOString(),
        progress: {
          total: 2,
          pending: 0,
          running: 0,
          imported: 2,
          skipped: 0,
          failed: 0,
          cancelled: 0,
          resumable: 0,
          review_required: 0,
          informational_preservation: 0,
          without_diagnostics: 2,
          diagnostic_occurrences: 0,
          diagnostics: [],
          finished: 2,
          current_batch: 1,
          total_batches: 1,
        },
      },
    }),
  )
  await page.route("**/api/v1/import-jobs/timing/items?**", (route) =>
    route.fulfill({
      json: {
        items: ["Timing supper", "Timing lunch"].map((title, position) => ({
          position,
          title,
          state: "imported",
          review_required: false,
          dropped_assets: 0,
          deferred_assets: 0,
          diagnostics: [],
        })),
        total: 2,
        limit: 50,
        offset: 0,
      },
    }),
  )
  await page.goto("/imports/timing")
  await expect(page.getByRole("heading", { name: "2 recipes imported", exact: true })).toBeVisible()
  const timing = page.getByRole("button", { name: "Timing", exact: true })
  await expect(timing).toHaveAttribute("aria-expanded", "false")
  await expect(page.getByText(/^Started /)).not.toBeVisible()
  await page.screenshot({ path: testInfo.outputPath("import-timing-collapsed.png") })
  await timing.focus()
  await page.keyboard.press("Enter")
  await expect(timing).toHaveAttribute("aria-expanded", "true")
  await expect(page.getByText(/^Started /)).toBeVisible()
  await expect(page.getByText(/^Finished /)).toBeVisible()
  await settleAnimations(page)
  await page.screenshot({ path: testInfo.outputPath("import-timing-expanded.png") })
  expect(
    (await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa"]).analyze())
      .violations,
  ).toEqual([])
  await page.keyboard.press("Enter")
  await expect(timing).toHaveAttribute("aria-expanded", "false")
  await expect(timing).toBeFocused()
})

for (const colorScheme of ["light", "dark"] as const) {
  test(`primary task surfaces pass measured accessibility checks in ${colorScheme} mode`, async ({
    page,
  }, testInfo) => {
    await page.emulateMedia({ colorScheme })
    await page.setViewportSize({ width: 390, height: 844 })
    for (const path of ["/", "/plan", "/shopping", "/books"]) {
      await page.goto(path)
      await expect(page.locator("main")).toBeVisible()
      await settleAnimations(page)
      const result = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
        .analyze()
      await page.screenshot({
        path: testInfo.outputPath(`${path.slice(1) || "recipes"}-${colorScheme}.png`),
      })
      expect(result.violations, `${path} in ${colorScheme}`).toEqual([])
    }
  })
}
