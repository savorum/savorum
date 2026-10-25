import { expect, test } from "@playwright/test"

import { choose } from "./controls"

test("file imports are reviewed inline and clean recipes can be committed selectively", async ({
  page,
}) => {
  const title = "Previewed lemon tart"
  await page.goto("/")

  await page
    .locator('input[type="file"]')
    .setInputFiles(["e2e/fixtures/lemon-tart.json", "e2e/fixtures/not-a-recipe.html"])

  const review = page.getByRole("region", { name: "Review import" })
  await expect(review).toBeVisible()
  await expect(review).toContainText("1 recipe will be added to your library.")
  await expect(review).toContainText("1 recipe could not be read.")
  await expect(page.getByRole("link", { name: title })).toHaveCount(0)

  const checkbox = review.getByRole("checkbox", { name: `Import ${title}` })
  const candidate = checkbox.locator("xpath=ancestor::li")
  await expect(checkbox).toBeChecked()
  // Facts are labelled pairs, so the label carries the noun.
  await expect(candidate).toContainText("Ingredients2")
  await expect(candidate).toContainText("Steps2")
  await candidate.getByText("Preview recipe").click()
  await expect(candidate).toContainText("A bright tart held for review")
  await expect(candidate).toContainText("200 g flour")
  await expect(candidate).toContainText("Fill and bake.")

  await choose(review.getByRole("combobox", { name: "Filter import candidates" }), "blocked")
  await expect(review).toContainText("not-a-recipe.html")
  await expect(review.getByRole("checkbox", { name: "Import not-a-recipe.html" })).toBeDisabled()
  // The reason reads as a sentence rather than behind a severity label.
  await expect(review).toContainText("does not publish a recipe Savorum can read")

  // Importing hands the selection to a background job with a page of its own.
  await review.getByRole("button", { name: "Import selected", exact: true }).click()
  await expect(page).toHaveURL(/\/imports\//)
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()
  await expect(page.getByRole("link", { name: title })).toBeVisible()

  // The recipe is in the library, and the file that could not be read is still
  // waiting to be reviewed.
  await page.getByRole("link", { name: "Back to your library" }).click()
  await expect(page.getByRole("link", { name: title })).toBeVisible()
  await expect(page.getByRole("region", { name: "Review import" })).toHaveCount(0)
})

test("regional measurement ambiguities are previewed, excluded, applied, and removable within one import", async ({
  page,
  request,
}) => {
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/measurement-review.yml")

  const review = page.getByRole("region", { name: "Save this recipe" })
  // Two spellings of one measure are one question, and the review says how
  // many measurements it settles rather than how many words it found.
  await expect(review).toContainText(
    "3 measurements name sizes that differ by country. Unanswered ones import as written.",
  )

  // The whole-library answer is the first thing offered, because most people
  // have one answer for every measure they own.
  const decisions = page.getByRole("region", { name: "Measurements to check" })
  await decisions.getByRole("radio", { name: "Australian", exact: true }).check()
  await expect(decisions).toContainText("Reads 3 measurements in 1 recipe.")
  await decisions.getByRole("button", { name: "Clear" }).click()

  // Answering one measure at a time is the same decision at a narrower scope,
  // and it keeps its own exceptions.
  await decisions.getByText("Answer the 2 measures separately").click()
  const tablespoon = page.getByRole("listitem").filter({ hasText: /^“tbsp”/ })
  await expect(tablespoon).toContainText("2 measurements · 1 recipe")
  await expect(tablespoon).toContainText("1 Tbsp olive oil")
  await expect(tablespoon).toContainText("1 ½ Tbsp lemon juice")
  await tablespoon.getByRole("radio", { name: /Australian/ }).check()
  await expect(tablespoon).toContainText("Reads 2 measurements in 1 recipe.")

  await tablespoon.getByText("Exceptions").click()
  const exceptions = page.getByRole("dialog", { name: "Exceptions" })
  const oliveOil = exceptions.getByRole("checkbox", { name: /Include 1 Tbsp olive oil/ })
  await oliveOil.evaluate((node) => node.setAttribute("data-identity-check", "stable-occurrence"))
  await oliveOil.uncheck()
  await expect(oliveOil).toHaveAttribute("data-identity-check", "stable-occurrence")
  await exceptions.getByRole("button", { name: "Done", exact: true }).click()
  await expect(tablespoon).toContainText("Reads 1 measurement in 1 recipe.")
  await expect(tablespoon).toContainText("1 excluded.")

  await tablespoon.getByRole("button", { name: "Apply" }).click()
  await expect(review).toContainText("2 measurements name sizes that differ by country.")
  await review.getByRole("button", { name: "Undo last answer" }).click()
  await expect(review).toContainText("3 measurements name sizes that differ by country.")

  await decisions.getByRole("radio", { name: "Australian", exact: true }).check()
  await expect(decisions).toContainText("Reads 3 measurements in 1 recipe.")
  await decisions.getByRole("button", { name: "Apply" }).click()
  await expect(review).toContainText("Every measurement is settled.")
  await expect(review.getByRole("button", { name: "Save recipe", exact: true })).toBeEnabled()

  await review.getByRole("button", { name: "Save recipe", exact: true }).click()
  await expect(page).toHaveURL(/\/imports\//)
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()
  const imported = page.getByRole("link", { name: "Regional dressing" })
  const recipePath = await imported.getAttribute("href")
  expect(recipePath).toMatch(/^\/recipes\//)
  await page.goto(`${recipePath}/edit`)
  await page.getByRole("button", { name: "More options for olive oil" }).click()
  await page.getByRole("menuitem", { name: "Details, olive oil", exact: true }).click()
  await page.getByRole("button", { name: "Revert this interpretation" }).click()
  await page.getByRole("button", { name: "Done" }).click()
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page).toHaveURL(new RegExp(`${recipePath}$`))

  const stored = await request.get(`/api/v1${recipePath}`)
  expect(stored.ok()).toBe(true)
  const body = (await stored.json()) as {
    revision: number
    recipe: {
      ingredient_sections: {
        ingredients: {
          name: string
          quantity?: unknown
          source_text?: string
          normalization?: { method: string; review?: string; warnings?: string[] }
        }[]
      }[]
    }
  }
  const reverted = body.recipe.ingredient_sections[0].ingredients[0]
  expect(body.revision).toBe(2)
  expect(reverted.name).toBe("1 Tbsp olive oil")
  expect(reverted.quantity).toBeUndefined()
  expect(reverted.source_text).toBe("1 Tbsp olive oil")
  expect(reverted.normalization?.method).toBe("unresolved")
  expect(reverted.normalization?.review).toBe("confirmed")
})

test("a large upload is imported in passes and the rest is taken up again", async ({ page }) => {
  await page.goto("/")
  await page
    .locator('input[type="file"]')
    .setInputFiles(["e2e/fixtures/passes-first.json", "e2e/fixtures/passes-second.json"])

  const review = page.getByRole("region", { name: "Review import" })
  await expect(review).toContainText("2 recipes will be added to your library.")
  await review.getByRole("button", { name: "Select none" }).click()
  await review.getByRole("checkbox", { name: "Import First pass loaf" }).check()
  await review.getByRole("button", { name: "Import selected", exact: true }).click()

  // The import carries the way back to what it left behind, and following it
  // reopens the same staged review rather than asking for the file again.
  await expect(page).toHaveURL(/\/imports\/.*remaining=/)
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()
  await page.getByRole("link", { name: "Review the rest of this file" }).click()

  // What is left is one recipe, so the surface is the one a single capture
  // gets: the recipe and Save, without the queue's selection machinery.
  const remainder = page.getByRole("region", { name: "Save this recipe" })
  await expect(page).toHaveURL(/\?preview=/)
  await expect(remainder.getByRole("button", { name: "Save recipe", exact: true })).toBeEnabled()
  await expect(remainder).toContainText("Second pass loaf")
  await expect(remainder.getByRole("checkbox", { name: /^Import / })).toHaveCount(0)
  await expect(remainder.getByRole("button", { name: "Select none" })).toHaveCount(0)

  // The link survives a reload, because the review is on the server rather
  // than in the page that made it.
  await page.reload()
  await expect(remainder.getByRole("button", { name: "Save recipe", exact: true })).toBeEnabled()
  await remainder.getByRole("button", { name: "Save recipe", exact: true }).click()
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()
  await expect(page.getByRole("link", { name: "Review the rest of this file" })).toHaveCount(0)
})

test("a single-recipe import keeps its result in focus", async ({ page }) => {
  const title = "Reported spiced buns"
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/spiced-buns.json")
  const review = page.getByRole("region", { name: "Save this recipe" })
  await review.getByRole("button", { name: "Save recipe", exact: true }).click()
  await expect(page).toHaveURL(/\/imports\//)

  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()
  await expect(page.getByRole("link", { name: title })).toBeVisible()
  await expect(page.getByRole("button", { name: "All recipes", exact: true })).toHaveCount(0)
  await expect(page.getByRole("progressbar", { name: "Import progress" })).toHaveCount(0)

  const job = new URL(page.url()).pathname.split("/").pop()
  await expect(page.getByRole("link", { name: "See them in your library" })).toHaveCount(0)
  await page.getByRole("link", { name: title, exact: true }).click()
  await expect(page).toHaveURL(/\/recipes\//)
  await expect(page.getByRole("button", { name: "Cook", exact: true })).toBeVisible()
  await expect(page.getByRole("heading", { name: title, exact: true, level: 1 })).toBeVisible()

  // Import history can still scope the library to this job.
  await page.goto(`/?imported=${job}`)
  await expect(page.getByRole("button", { name: "From one import" })).toBeVisible()
  await expect(page.getByRole("link", { name: new RegExp(title) })).toBeVisible()

  // The scope is a filter like any other, so it can be taken off.
  await page.getByRole("button", { name: "From one import" }).click()
  await expect(page.getByRole("button", { name: "From one import" })).toHaveCount(0)
})

test("progress updates in place without replacing the document or its interactive state", async ({
  page,
}) => {
  const started = new Date(Date.now() - 12_000).toISOString()
  const items = Array.from({ length: 30 }, (_, position) => ({
    position,
    title: `Stable recipe ${position + 1}`,
    filename: "stable-recipes.zip",
    state: "pending",
    review_required: false,
    outcome: "",
    library_entry_id: "",
    recipe_id: `recipe-${position + 1}`,
    revision: 0,
    dropped_assets: 0,
    deferred_assets: 0,
    diagnostics: [],
    started_at: started,
  }))
  let jobReads = 0
  let terminalReads = 0

  await page.addInitScript(() => {
    const loads = Number(sessionStorage.getItem("import-progress-document-loads") ?? "0")
    sessionStorage.setItem("import-progress-document-loads", String(loads + 1))
  })
  await page.route("**/api/v1/import-jobs/stable", async (route) => {
    jobReads += 1
    const finished = jobReads === 1 ? 0 : jobReads === 2 ? 10 : 30
    const state = finished === 30 ? "completed" : "running"
    if (state === "completed") terminalReads += 1
    await route.fulfill({
      json: {
        id: "stable",
        importer: "SREF recipe",
        source: "stable-recipes.zip",
        state,
        on_duplicate: "skip",
        total_items: 30,
        created_at: started,
        started_at: started,
        ...(state === "completed" ? { finished_at: new Date().toISOString() } : {}),
        progress: {
          total: 30,
          pending: 30 - finished,
          running: 0,
          imported: finished,
          skipped: 0,
          failed: 0,
          cancelled: 0,
          review_required: 0,
          finished,
          current_batch: finished === 0 ? 1 : Math.ceil(finished / 10),
          total_batches: 3,
        },
      },
    })
  })
  await page.route("**/api/v1/import-jobs/stable/items?**", async (route) => {
    const finished = jobReads === 1 ? 0 : jobReads === 2 ? 10 : 30
    await route.fulfill({
      json: {
        items: items.map((item, position) => ({
          ...item,
          state: position < finished ? "imported" : "pending",
          outcome: position < finished ? "created" : "",
          revision: position < finished ? 1 : 0,
        })),
        total: 30,
        limit: 100,
        offset: 0,
      },
    })
  })

  // A poll can only fire while its timer is still scheduled, so pending poll
  // delays (2 to 30 seconds), not elapsed wall time, decide when polling has
  // stopped. The two-minute page-activity timer stays scheduled throughout.
  await page.addInitScript(() => {
    const pending = new Map<number, number>()
    const set = window.setTimeout.bind(window)
    const clear = window.clearTimeout.bind(window)
    window.setTimeout = ((handler: TimerHandler, timeout?: number, ...args: unknown[]) => {
      const id = set(() => {
        pending.delete(id)
        if (typeof handler === "function") handler(...args)
      }, timeout)
      pending.set(id, timeout ?? 0)
      return id
    }) as typeof window.setTimeout
    window.clearTimeout = ((id?: number) => {
      if (id !== undefined) pending.delete(id)
      clear(id)
    }) as typeof window.clearTimeout
    ;(window as unknown as { pendingPollTimers: () => number }).pendingPollTimers = () =>
      [...pending.values()].filter((delay) => delay >= 1_500 && delay <= 30_000).length
  })
  await page.goto("/imports/stable")
  await expect(page.getByRole("heading", { name: "Importing 0 of 30 recipes" })).toBeVisible()

  const allRecipes = page.getByRole("button", { name: "All recipes" })
  await page.evaluate(() => {
    ;(window as typeof window & { importProgressMarker?: string }).importProgressMarker =
      "same-document"
    document.querySelector("main")!.setAttribute("data-identity-check", "same-main")
    document
      .querySelector('[aria-label="Filter import results"] button')!
      .setAttribute("data-identity-check", "same-filter")
    const range = document.createRange()
    range.selectNodeContents(document.querySelector("main h1")!)
    const selection = window.getSelection()!
    selection.removeAllRanges()
    selection.addRange(range)
  })
  // Stay away from the document's maximum scroll offset: terminal copy is
  // deliberately shorter, and a browser cannot retain an offset below the new
  // bottom even when the route preserves it exactly.
  await page.getByText("Stable recipe 20").scrollIntoViewIfNeeded()
  await allRecipes.evaluate((node) => (node as HTMLElement).focus({ preventScroll: true }))
  const scrollBefore = await page.evaluate(() => window.scrollY)
  expect(scrollBefore).toBeGreaterThan(0)

  await expect(page.getByRole("heading", { name: "30 recipes imported" })).toBeVisible()
  expect(await page.evaluate(() => sessionStorage.getItem("import-progress-document-loads"))).toBe(
    "1",
  )
  expect(
    await page.evaluate(
      () => (window as typeof window & { importProgressMarker?: string }).importProgressMarker,
    ),
  ).toBe("same-document")
  await expect(page.locator("main")).toHaveAttribute("data-identity-check", "same-main")
  await expect(allRecipes).toHaveAttribute("data-identity-check", "same-filter")
  await expect(allRecipes).toBeFocused()
  expect(await page.evaluate(() => window.scrollY)).toBe(scrollBefore)
  expect(await page.evaluate(() => window.getSelection()?.toString())).toBe("stable-recipes.zip")
  await expect(page.getByRole("button", { name: "All recipes" })).toHaveAttribute(
    "aria-pressed",
    "true",
  )

  await page.waitForFunction(
    () => (window as unknown as { pendingPollTimers: () => number }).pendingPollTimers() === 0,
  )
  expect(terminalReads).toBe(1)
})

test("a long source filename appears once and the import results reflow", async ({ page }) => {
  const source = `${"family-recipes-".repeat(14)}.zip`
  const finished = "2026-09-04T12:01:00Z"
  await page.route("**/api/v1/import-jobs/long-source", (route) =>
    route.fulfill({
      json: {
        id: "long-source",
        importer: "CookbookManager",
        source,
        state: "completed",
        on_duplicate: "skip",
        total_items: 2,
        created_at: "2026-09-04T12:00:00Z",
        finished_at: finished,
        progress: {
          total: 2,
          pending: 0,
          running: 0,
          imported: 2,
          skipped: 0,
          failed: 0,
          cancelled: 0,
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
  await page.route("**/api/v1/import-jobs/long-source/items?**", (route) =>
    route.fulfill({
      json: {
        items: [
          {
            position: 0,
            title: "Long filename supper",
            filename: source,
            state: "imported",
            review_required: false,
            dropped_assets: 0,
            deferred_assets: 0,
            diagnostics: [],
          },
          {
            position: 1,
            title: "Second supper",
            filename: source,
            state: "imported",
            review_required: false,
            dropped_assets: 0,
            deferred_assets: 0,
            diagnostics: [],
          },
        ],
        total: 2,
        limit: 100,
        offset: 0,
      },
    }),
  )

  await page.setViewportSize({ width: 320, height: 800 })
  await page.goto("/imports/long-source")

  await expect(page.getByText(source)).toHaveCount(1)
  await expect(page.getByRole("heading", { level: 1, name: source })).toBeVisible()
  const timing = page.getByRole("button", { name: "Timing", exact: true })
  await expect(timing).toHaveAttribute("aria-expanded", "false")
  await timing.focus()
  await page.keyboard.press("Enter")
  const copy = await page.locator("main").innerText()
  expect(copy).not.toContain("—")
  expect(copy).not.toContain("Every recipe in this import arrived.")
  expect(copy).not.toContain("Batch 1 of 1")
  expect(copy).not.toContain("Processing details")
  expect(copy.match(/Started/g)).toHaveLength(1)
  expect(copy.match(/Finished/g)).toHaveLength(1)
  const overflow = await page.evaluate(() =>
    Array.from(document.querySelectorAll<HTMLElement>("main *"))
      .filter((element) => element.getBoundingClientRect().right > window.innerWidth + 1)
      .map((element) => element.textContent?.trim().slice(0, 80)),
  )
  expect(overflow).toEqual([])
})

test("a chosen file reports which phase it is in, and can be stopped", async ({ page }) => {
  let releasePreview!: () => void
  const held = new Promise<void>((resolve) => {
    releasePreview = resolve
  })
  await page.route("**/api/v1/recipes/import-preview", async (route) => {
    await held
    await route.continue()
  })
  await page.goto("/")

  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/lemon-tart.json")

  // Sending and reading are different kinds of waiting, and the interface says
  // which one it is rather than describing both as "working".
  const progress = page.getByRole("progressbar", { name: /Sending|Reading/ })
  await expect(progress).toBeVisible()
  const panel = page.getByRole("status").filter({ hasText: /Sending 1 file|Reading 1 file/ })
  await expect(panel).toBeVisible()
  // Waiting is only bearable if it can be ended.
  await expect(panel.getByRole("button", { name: "Cancel" })).toBeVisible()

  releasePreview()
  await expect(page.getByRole("region", { name: "Save this recipe" })).toBeVisible()
  await expect(progress).toHaveCount(0)
})

test("a held import can be abandoned from its progress panel", async ({ page }) => {
  await page.route("**/api/v1/recipes/import-preview", () => {
    // Never answered: the reader's only way out is the cancel control.
  })
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/lemon-tart.json")

  const panel = page.getByRole("status").filter({ hasText: /Sending 1 file|Reading 1 file/ })
  await expect(panel).toBeVisible()
  await panel.getByRole("button", { name: "Cancel" }).click()
  await expect(page.getByRole("progressbar", { name: /Sending|Reading/ })).toHaveCount(0)
  await expect(page.getByRole("region", { name: "Save this recipe" })).toHaveCount(0)
})

test("the import menu previews recipe URLs and keeps failures actionable", async ({ page }) => {
  let fail = false
  await page.route("**/api/v1/recipes/import-url/preview", async (route) => {
    if (fail) {
      await route.fulfill({
        status: 422,
        contentType: "application/problem+json",
        body: JSON.stringify({ detail: "No recipe was found on that page." }),
      })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        id: "url-preview",
        expires_at: "2026-09-01T12:00:00Z",
        items: [
          {
            id: "item-1",
            recipe: {
              title: "Web noodle bowl",
              description: "Fetched from the web.",
              yield: "2 bowls",
              ingredient_count: 2,
              step_count: 1,
              ingredients: [{ items: ["noodles", "broth"] }],
              instructions: [{ items: ["Simmer."] }],
            },
            source: {
              format_id: "web-recipe",
              format_name: "Recipe web page",
              url: "https://recipes.example/noodles",
            },
            analysis: { status: "new", matches: [] },
            diagnostics: [],
            assets: { discovered: 0, available: 0, deferred: 0, failed: 0, unavailable: 0 },
            already_here: false,
            can_commit: true,
          },
        ],
      }),
    })
  })
  await page.goto("/")

  await page.getByRole("button", { name: "Add recipes" }).click()
  await page.getByRole("menuitem", { name: "From a website" }).click()
  const dialog = page.getByRole("dialog", { name: "Import a recipe from the web" })
  await dialog.getByRole("textbox", { name: "Recipe URL" }).fill("https://recipes.example/noodles")
  await dialog.getByRole("button", { name: "Preview recipe" }).click()
  await expect(page.getByRole("region", { name: "Save this recipe" })).toContainText(
    "Web noodle bowl",
  )

  // A capture is one focused task, so the library and its import menu come back
  // by leaving this one rather than sitting behind it.
  fail = true
  await page.getByRole("button", { name: "Cancel", exact: true }).click()
  await page.getByRole("button", { name: "Add recipes" }).click()
  await page.getByRole("menuitem", { name: "From a website" }).click()
  await dialog
    .getByRole("textbox", { name: "Recipe URL" })
    .fill("https://recipes.example/not-a-recipe")
  await dialog.getByRole("button", { name: "Preview recipe" }).click()
  await expect(dialog.getByText("No recipe was found on that page.")).toBeVisible()
  await expect(dialog).toBeVisible()
})

test("large preview queues support bulk and shift-range selection", async ({ page }) => {
  const items = Array.from({ length: 7 }, (_, index) => ({
    id: `item-${index + 1}`,
    recipe: {
      title: `Queue recipe ${index + 1}`,
      ingredient_count: index + 1,
      step_count: 1,
      ingredients: [{ items: ["water"] }],
      instructions: [{ items: ["Cook."] }],
    },
    source: {
      format_id: "cookbookmanager",
      format_name: "CookbookManager",
      filename: "recipes.zip",
    },
    analysis: { status: "new", matches: [] },
    diagnostics:
      index === 5
        ? [
            {
              code: "review",
              class: "actionable",
              severity: "warning",
              area: "ingredients",
              count: 1,
              message: "Review one ingredient.",
            },
          ]
        : index === 6
          ? [
              {
                code: "invalid",
                class: "blocking",
                severity: "error",
                area: "content",
                count: 1,
                message: "Recipe is incomplete.",
              },
            ]
          : [],
    assets: {
      discovered: 1,
      available: index < 2 ? 1 : 0,
      deferred: index >= 2 ? 1 : 0,
      failed: 0,
      unavailable: 0,
    },
    already_here: false,
    can_commit: index !== 6,
  }))
  await page.route("**/api/v1/recipes/import-preview", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ id: "large-preview", expires_at: "2026-09-01T12:00:00Z", items }),
    }),
  )
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/lemon-tart.json")

  const review = page.getByRole("region", { name: "Review import" })
  // One recipe asks for a review and one cannot be imported; the remaining
  // five require no attention.
  await expect(review).toContainText("6 recipes will be added to your library.")
  await expect(review).toContainText("1 recipe could not be read.")
  await review.getByRole("button", { name: "Select all" }).click()
  await expect(review.getByText("6 recipes selected")).toBeVisible()
  await review.getByRole("button", { name: "Select none" }).click()
  // Reviewable recipes remain selectable, while blocked recipes do not.
  await choose(review.getByRole("combobox", { name: "Filter import candidates" }), "review")
  await review.getByRole("button", { name: "Select visible" }).click()
  await expect(review.getByText("1 recipe selected")).toBeVisible()
  await choose(review.getByRole("combobox", { name: "Filter import candidates" }), "blocked")
  await review.getByRole("button", { name: "Select visible" }).click()
  await expect(review.getByText("1 recipe selected")).toBeVisible()

  await choose(review.getByRole("combobox", { name: "Filter import candidates" }), "all")
  await review.getByRole("button", { name: "Select none" }).click()
  await review.getByRole("checkbox", { name: "Import Queue recipe 1" }).click()
  await review
    .getByRole("checkbox", { name: "Import Queue recipe 5" })
    .click({ modifiers: ["Shift"] })
  await expect(review.getByText("5 recipes selected")).toBeVisible()
  await expect(review.getByRole("checkbox", { name: "Import Queue recipe 3" })).toBeChecked()
  await expect(review.getByRole("checkbox", { name: "Import Queue recipe 7" })).toBeDisabled()
})
