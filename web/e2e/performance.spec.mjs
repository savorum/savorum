import { readFileSync, writeFileSync } from "node:fs"
import process from "node:process"

import { expect, test } from "@playwright/test"

const contract = JSON.parse(
  readFileSync(new URL("../../internal/scalefixture/contract.json", import.meta.url), "utf8"),
)
test("representative library first render stays within the product budget", async ({ page }) => {
  test.skip(!process.env.SAVORUM_SCALE_REPORT, "Requires the shared scalecheck fixture")
  const timings = []
  // Fresh browser contexts are supplied by Playwright; each navigation discards
  // the document. Disable cache so this includes shell, scripts and card assets.
  const session = await page.context().newCDPSession(page)
  await session.send("Network.enable")
  await session.send("Network.setCacheDisabled", { cacheDisabled: true })
  for (let i = 0; i < 5; i++) {
    const start = performance.now()
    await page.goto("/")
    await expect(page.locator(".recipe-card-title").first()).toBeVisible()
    await page.waitForFunction(() => {
      const image = document.querySelector("a:has(.recipe-card-title) img")
      return image instanceof HTMLImageElement && image.complete && image.naturalWidth > 0
    })
    await page.evaluate(
      () =>
        new Promise((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        ),
    )
    timings.push(performance.now() - start)
  }
  timings.sort((a, b) => a - b)
  const milliseconds = timings.at(-1)
  const path = process.env.SAVORUM_SCALE_REPORT
  const report = JSON.parse(readFileSync(path, "utf8"))
  report.milliseconds.browser_first_render = milliseconds
  writeFileSync(path, JSON.stringify(report, null, 2) + "\n")
  expect(
    milliseconds,
    `cold library first render at ${report.recipes} recipes`,
  ).toBeLessThanOrEqual(contract.budgets_ms.browser_first_render)
})
