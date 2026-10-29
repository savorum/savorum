import { readFileSync, writeFileSync } from "node:fs"
import process from "node:process"

import { expect, test } from "@playwright/test"

const contract = JSON.parse(
  readFileSync(new URL("../../internal/scalefixture/contract.json", import.meta.url), "utf8"),
)
test("representative library first render stays within the product budget", async ({ context }) => {
  test.skip(!process.env.SAVORUM_SCALE_REPORT, "Requires the shared scalecheck fixture")
  const timings = []
  const labelRequests = []
  const reportPath = process.env.SAVORUM_SCALE_REPORT
  const report = JSON.parse(readFileSync(reportPath, "utf8"))
  expect(report.workload_version).toBe(contract.version)
  for (let i = 0; i < 5; i++) {
    const page = await context.newPage()
    const requests = { tags: 0, facets: 0, total: 0, recipeDetails: 0 }
    page.on("request", (request) => {
      const url = new URL(request.url())
      if (url.pathname === "/api/v1/tags") requests.tags++
      if (url.pathname === "/api/v1/facets") requests.facets++
      if (url.pathname === "/api/v1/recipes" && url.searchParams.get("limit") === "1")
        requests.total++
      if (/^\/api\/v1\/recipes\/rscale\d+$/.test(url.pathname)) requests.recipeDetails++
    })
    const session = await context.newCDPSession(page)
    await session.send("Network.enable")
    await session.send("Network.setCacheDisabled", { cacheDisabled: true })
    const start = performance.now()
    const metadata = ["/api/v1/tags", "/api/v1/facets"].map((path) =>
      page.waitForResponse((response) => new URL(response.url()).pathname === path),
    )
    const total = page.waitForResponse((response) => {
      const url = new URL(response.url())
      return url.pathname === "/api/v1/recipes" && url.searchParams.get("limit") === "1"
    })
    await page.goto("/?sort=title-asc")
    for (const response of await Promise.all([...metadata, total])) expect(response.ok()).toBe(true)
    await expect(page.locator(".recipe-card-title").first()).toBeVisible()
    await page.waitForFunction(() => {
      const image = document.querySelector("a:has(.recipe-card-title) img")
      const first = document.querySelector('a[href="/recipes/rscale000000"]')
      const mixed = document.querySelector('a[href="/recipes/rscale000002"]')
      const labels = (card) =>
        Array.from(card?.querySelectorAll("p:last-child > span") ?? [], (node) =>
          node.textContent.trim(),
        )
      return (
        image instanceof HTMLImageElement &&
        image.complete &&
        image.naturalWidth > 0 &&
        JSON.stringify(labels(first)) === JSON.stringify(["course 0", "cuisine 0", "+7"]) &&
        JSON.stringify(labels(mixed)) === JSON.stringify(["course 0", "Popular label", "+6"])
      )
    })
    await page.evaluate(
      () =>
        new Promise((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        ),
    )
    timings.push(performance.now() - start)
    expect(requests).toEqual({ tags: 1, facets: 1, total: 1, recipeDetails: 0 })
    labelRequests.push(requests)
    await page.close()
  }
  timings.sort((a, b) => a - b)
  const milliseconds = timings.at(-1)
  report.milliseconds.browser_first_render = milliseconds
  report.browser_samples_ms = timings
  report.browser_label_requests = labelRequests
  writeFileSync(reportPath, JSON.stringify(report, null, 2) + "\n")
  expect(
    milliseconds,
    `cold library first render at ${report.recipes} recipes`,
  ).toBeLessThanOrEqual(contract.budgets_ms.browser_first_render)
})
