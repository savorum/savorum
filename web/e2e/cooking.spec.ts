import { expect, test } from "@playwright/test"

import { seedRecipe } from "./library"

test("cooking mode carries the batch, resumes checkoffs, and starts clean on request", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const recipe = await seedRecipe(request, "Cooking session supper", {
    ingredient_sections: [
      {
        id: "ingredients",
        ingredients: [
          {
            id: "salt",
            name: "fine salt",
            quantity: { kind: "simple", amount: { value: "1" }, unit: "mass.gram" },
          },
        ],
      },
    ],
    instruction_sections: [
      {
        id: "method",
        steps: [
          {
            id: "mix",
            text: "Mix the ingredients.",
            ingredient_uses: [{ ingredient_ref: "salt" }],
          },
          { id: "cook", text: "Cook until tender." },
        ],
      },
    ],
  })

  await page.goto(`/recipes/${recipe.id}`)
  await page.getByRole("textbox", { name: "How many servings" }).fill("8")
  await page.getByRole("textbox", { name: "How many servings" }).press("Tab")
  await page.getByRole("button", { name: "Cook", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}/cook`))
  await expect(
    page.getByRole("heading", { name: new RegExp(`Cooking · ${recipe.title}`) }),
  ).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)
  // The batch a cook chose in the reader is stated on the guided view itself;
  // the controls that change it are one panel away, with the rest of what a
  // cook settles once before working.
  await expect(page.getByText("Batch ×2", { exact: true })).toBeVisible()
  await page.getByRole("button", { name: "Cooking options", exact: true }).click()
  const options = page.getByRole("dialog")
  await expect(options.getByRole("textbox", { name: "How many servings" })).toHaveValue("8")
  // The panel is rendered a frame before it answers Escape; it has taken
  // focus by the time it does.
  await expect(options.locator(":focus")).toHaveCount(1)
  await page.keyboard.press("Escape")
  await expect(options).toBeHidden()
  await expect(page.getByRole("heading", { name: "Step 1 of 2" })).toBeVisible()
  await expect(page.getByRole("checkbox", { name: "2 g fine salt", exact: true })).toBeVisible()
  const salt = page.getByRole("checkbox", { name: /fine salt/ })
  await salt.check()
  // One control finishes the step and moves on, which is the same act at the
  // stove; the step it leaves behind stays counted as done.
  await page.getByRole("button", { name: "Next step", exact: true }).click()
  await expect(page.getByRole("heading", { name: "Step 2 of 2" })).toBeVisible()
  await expect(page.getByText("1 of 2", { exact: true })).toBeVisible()

  await page.reload()
  await expect(page.getByRole("heading", { name: "Step 2 of 2" })).toBeVisible()
  await expect(page.getByText("1 of 2", { exact: true })).toBeVisible()
  await expect(page.getByRole("checkbox", { name: /fine salt/ })).toBeChecked()
  await page.getByRole("button", { name: "Exit cooking", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}`))
  await page.getByRole("button", { name: "Resume cooking", exact: true }).click()
  await expect(page.getByRole("checkbox", { name: /fine salt/ })).toBeChecked()
  await page.getByRole("button", { name: "Cooking options", exact: true }).click()
  await page.getByRole("button", { name: "Start over", exact: true }).click()
  await expect(page.getByRole("heading", { name: "Step 1 of 2" })).toBeVisible()
  await expect(page.getByText("0 of 2", { exact: true })).toBeVisible()
  await expect(page.getByRole("checkbox", { name: /fine salt/ })).not.toBeChecked()
})

test("a sectioned method keeps its parts and its numbering from the reader into cooking", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Sectioned method supper", {
    times: {
      assertions: [
        { id: "t-prove", kind: "proof", duration: { value: "PT45M" } },
        { id: "t-rest", kind: "rest", duration: { value: "PT10M" } },
        { id: "t-bake", kind: "cook", label: "Bake", duration: { value: "PT25M" } },
      ],
    },
    instruction_sections: [
      { id: "lead", steps: [{ id: "l1", text: "Read the recipe through before starting." }] },
      {
        id: "dough",
        title: "The dough",
        steps: [
          { id: "d1", text: "Mix and knead until smooth." },
          {
            id: "d2",
            text: "Prove until doubled, then rest before shaping.",
            timing_refs: ["t-prove", "t-rest"],
          },
        ],
      },
      {
        id: "baking",
        title: "Baking",
        steps: [{ id: "b1", text: "Bake until deeply coloured.", timing_refs: ["t-bake"] }],
      },
    ],
  })

  // The reader numbers steps inside the part of the method they belong to.
  await page.goto(`/recipes/${recipe.id}`)
  await expect(page.getByRole("heading", { name: "The dough" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Baking" })).toBeVisible()
  // Two timings on one step are two separate controls, each naming its phase.
  const readerRest = page.getByRole("button", { name: /^Rest ·/ })
  await expect(readerRest).toBeVisible()
  await expect(page.getByRole("button", { name: /^Prove ·/ })).toBeVisible()

  await page.getByRole("button", { name: "Cook", exact: true }).click()
  // Cooking works one step at a time, and names the one it is on the way the
  // reader names it: counted inside its own part of the method.
  await expect(page.getByRole("heading", { name: "Step 1", exact: true })).toBeVisible()
  await page.getByRole("button", { name: "All steps", exact: true }).click()
  // The whole method is one control away, with the reader's sections and
  // numbering.
  await expect(page.getByRole("heading", { name: "The dough" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Baking" })).toBeVisible()
  await expect(page.getByRole("button", { name: /^Step 1\b/ })).toHaveCount(3)

  await page.getByRole("button", { name: /^Step 1 Bake until deeply coloured/ }).click()
  await expect(page.getByRole("heading", { name: "Step 1 · Baking" })).toBeVisible()
  await page.getByRole("button", { name: /^Bake ·|^Start 25 min timer$/ }).click()
  const timer = await page.evaluate(
    () =>
      Object.keys(localStorage)
        .filter((key) => key.startsWith("savorum.timers.v2:"))
        .map((key) => JSON.parse(localStorage.getItem(key)!))[0],
  )
  // The timer records where the cook was, said the way the reader says it.
  expect(timer).toMatchObject({ label: "Bake", stepNumber: 1, sectionTitle: "Baking" })
  await expect(page.getByRole("button", { name: /Timers:/ })).toBeVisible()
})

test("two timings on one step run as two independent timers", async ({ page, request }) => {
  const recipe = await seedRecipe(request, "Two timer supper", {
    times: {
      assertions: [
        { id: "t-rest", kind: "rest", duration: { value: "PT30M" } },
        { id: "t-brown", kind: "cook", label: "Brown", duration: { value: "PT20M" } },
      ],
    },
    instruction_sections: [
      {
        id: "method",
        steps: [
          {
            id: "s1",
            text: "Rest the meat while the potatoes brown.",
            timing_refs: ["t-rest", "t-brown"],
          },
        ],
      },
    ],
  })
  await page.goto(`/recipes/${recipe.id}`)
  const rest = page.getByRole("button", { name: /^Rest ·/ })
  const brown = page.getByRole("button", { name: /^Brown ·/ })
  await expect(rest).toHaveText(/30 min/)
  await expect(brown).toHaveText(/20 min/)

  await rest.click()
  // Starting one must not make the other look as though it is already running.
  await expect(rest).toHaveAttribute("data-running", "true")
  await expect(brown).toHaveAttribute("data-running", "false")
  await expect(brown).toHaveText(/20 min/)

  await brown.click()
  await expect(brown).toHaveAttribute("data-running", "true")
  const timers = await page.evaluate(
    () =>
      Object.keys(localStorage)
        .filter((key) => key.startsWith("savorum.timers.v2:"))
        .map((key) => JSON.parse(localStorage.getItem(key)!))
        .sort((a, b) => a.startedAt - b.startedAt) as {
        label: string
        seconds: number
      }[],
  )
  expect(timers.map((timer) => [timer.label, timer.seconds])).toEqual([
    ["Rest", 1800],
    ["Brown", 1200],
  ])
})

test("finishing a cooking session records one history event without a recipe revision", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Finished cooking supper")
  await page.goto(`/recipes/${recipe.id}/cook`)
  await page.getByRole("button", { name: "More cooking actions", exact: true }).click()
  await page.getByRole("menuitem", { name: "Finish cooking", exact: true }).click()
  await page.getByRole("textbox", { name: "Add a note (optional)" }).fill("Less salt next time")
  await page.getByRole("button", { name: "Finish", exact: true }).click()
  await expect(page.getByRole("heading", { name: "Meal finished" })).toBeVisible()
  const history = await (await request.get(`/api/v1/recipes/${recipe.id}/cookings`)).json()
  expect(history.times_cooked).toBe(1)
  expect(history.items).toHaveLength(1)
  expect(history.items[0].note).toBe("Less salt next time")
  const current = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(current.headers().etag).toBe('"1"')
})

test("screen awake is requested only for cooking and released when it ends", async ({
  page,
  request,
}) => {
  await page.addInitScript(() => {
    const calls = { request: 0, release: 0 }
    Object.defineProperty(window, "__wakeCalls", { value: calls, configurable: true })
    Object.defineProperty(navigator, "wakeLock", {
      configurable: true,
      value: {
        request: async () => {
          calls.request += 1
          return {
            released: false,
            release: async () => {
              calls.release += 1
            },
            addEventListener: () => {},
          }
        },
      },
    })
  })
  const recipe = await seedRecipe(request, "Wakeful cooking supper")
  await page.goto(`/recipes/${recipe.id}`)
  await expect
    .poll(() =>
      page.evaluate(
        () => (window as Window & { __wakeCalls?: { request: number } }).__wakeCalls?.request,
      ),
    )
    .toBe(0)
  await page.getByRole("button", { name: "Cook", exact: true }).click()
  await expect
    .poll(() =>
      page.evaluate(
        () => (window as Window & { __wakeCalls?: { request: number } }).__wakeCalls?.request,
      ),
    )
    .toBe(1)
  await page.getByRole("button", { name: "Exit cooking", exact: true }).click()
  await expect
    .poll(() =>
      page.evaluate(
        () => (window as Window & { __wakeCalls?: { release: number } }).__wakeCalls?.release,
      ),
    )
    .toBe(1)
})

test("a microwave step adjusts for the reader's own oven and says nothing when it agrees", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Microwave timing supper", {
    times: {
      assertions: [
        {
          id: "warm",
          kind: "cook",
          label: "Microwave",
          duration: { value: "PT3M" },
          provenance: "supplemental",
          microwave: {
            source_text: "microwave for 3 minutes at 1250 W",
            power: { watts: 1250 },
          },
        },
      ],
    },
    instruction_sections: [
      {
        id: "method",
        steps: [{ id: "warm-step", text: "Microwave the bowl.", timing_refs: ["warm"] }],
      },
    ],
  })

  const setOutput = async (watts: number | null) => {
    const current = await (await request.get("/api/v1/me/preferences")).json()
    const overrides = { ...current.overrides, microwave_output_watts: watts ?? undefined }
    if (watts === null) delete overrides.microwave_output_watts
    const saved = await request.put("/api/v1/me/preferences", {
      data: { revision: current.revision, overrides },
    })
    expect(saved.ok(), await saved.text()).toBe(true)
  }

  // The reader's microwave is the one the recipe was written for, so there is
  // no adjustment to offer and the step is its timer and nothing else.
  await setOutput(1250)
  await page.goto(`/recipes/${recipe.id}`)
  const timers = page.getByRole("group", { name: "Timers" }).first()
  await expect(timers.getByRole("button", { name: /Start 3 min timer/ })).toBeVisible()
  await expect(timers).not.toContainText("microwave:")
  await expect(timers.getByRole("button", { name: "Different microwave" })).toHaveCount(0)
  // An unscaled time is never reported as an estimate.
  await expect(timers).not.toContainText("about 3 min")

  // A weaker microwave needs longer, so the chip times the adjusted duration,
  // one line says what it is, and one control offers the recipe's own time.
  await setOutput(800)
  await page.reload()
  // The chip says the time, so what qualifies it says only whose oven it is
  // for rather than repeating the duration beside itself.
  await expect(timers).toContainText("for your 800 W microwave")
  await expect(timers).not.toContainText("4 min 41 sec for your")
  await expect(timers.getByRole("button", { name: /Start 4 min 41 sec timer/ })).toBeVisible()
  const useRecipe = timers.getByRole("button", { name: "Use the recipe's 3 min" })
  await expect(useRecipe).toBeVisible()
  await useRecipe.click()
  await expect(timers).toContainText("as the recipe wrote it, at 1250 W")
  await expect(timers.getByRole("button", { name: /Start 3 min timer/ })).toBeVisible()
  await timers.getByRole("button", { name: "Use about 4 min 41 sec for 800 W" }).click()

  // One meal cooked in another kitchen's microwave changes this reading only.
  await timers.getByRole("button", { name: "Different microwave" }).click()
  await timers.getByRole("spinbutton", { name: "Microwave output" }).fill("1000")
  await expect(timers).toContainText("for your 1000 W microwave")
  await expect(timers.getByRole("button", { name: /Start 3 min 45 sec timer/ })).toBeVisible()
  const stored = await (await request.get("/api/v1/me/preferences")).json()
  expect(stored.effective.microwave_output_watts).toBe(800)

  // Cooking is the screen a timer is actually started from, so it offers the
  // same timing the reader did rather than the recipe's own.
  await page.goto(`/recipes/${recipe.id}/cook`)
  const cooking = page.getByRole("group", { name: "Timers" }).first()
  await expect(cooking.getByRole("button", { name: /Start 4 min 41 sec timer/ })).toBeVisible()
  await expect(cooking).toContainText("for your 800 W microwave")
  await expect(cooking.getByRole("button", { name: /Start 3 min timer/ })).toHaveCount(0)

  await setOutput(null)
})

test("recipe ingredients can be added on HTTP hosts without randomUUID", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "HTTP shopping supper")
  await page.addInitScript(() => {
    Object.defineProperty(crypto, "randomUUID", { value: undefined, configurable: true })
  })
  await page.goto(`/recipes/${recipe.id}`)
  await page.getByRole("button", { name: "More actions", exact: true }).click()
  await page.getByRole("menuitem", { name: "Add ingredients to list", exact: true }).click()
  await expect(page.getByRole("dialog")).toBeVisible()
  await page.getByRole("button", { name: "Update shopping list", exact: true }).click()
  await expect(page.getByRole("heading", { name: "Shopping list updated" })).toBeVisible()
})

test("resuming cooking restores every dish and can add another recipe", async ({
  page,
  request,
}) => {
  const first = await seedRecipe(request, "Pancakes to resume")
  const second = await seedRecipe(request, "Potatoes to resume")
  const third = await seedRecipe(request, "Eggs to add")
  await page.goto(`/recipes/${first.id}/cook`)
  await page.getByRole("button", { name: "Next step", exact: true }).click()
  await page.goto(`/recipes/${second.id}/cook`)
  await expect(page.getByRole("button", { name: new RegExp(first.title) })).toBeVisible()
  await page.goto(`/recipes/${first.id}`)
  await page.getByRole("button", { name: "Resume cooking", exact: true }).click()
  await expect(page.getByRole("button", { name: new RegExp(first.title) })).toBeVisible()
  await expect(page.getByRole("button", { name: new RegExp(second.title) })).toBeVisible()
  await expect(
    page.getByText("Fold the chicken through and refrigerate for two hours.", { exact: true }),
  ).toBeVisible()
  await page.getByRole("button", { name: "More cooking actions", exact: true }).click()
  await page.getByRole("menuitem", { name: "Add a dish", exact: true }).click()
  await page.getByRole("textbox", { name: "Find a recipe" }).fill(third.title)
  await page.getByRole("button", { name: third.title, exact: true }).click()
  await expect(page.getByRole("button", { name: new RegExp(third.title) })).toBeVisible()
})
