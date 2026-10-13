import { expect, test, type Locator } from "@playwright/test"

import { choose, chooser, openSection } from "./controls"
import { seedRecipe } from "./library"

async function compose(field: Locator, value: string) {
  await field.fill("")
  await field.evaluate((node) => node.setAttribute("data-entry-identity", "same"))
  await field.pressSequentially(value)
  await field.evaluate((node) => (node as HTMLInputElement).setSelectionRange(0, 0))
  await field.pressSequentially("Fresh ")
  await expect(field).toBeFocused()
  await field.evaluate((node) => {
    const input = node as HTMLInputElement
    const at = input.selectionStart ?? 0
    input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true }))
    input.value = input.value.slice(0, at) + "茶" + input.value.slice(at)
    input.setSelectionRange(at + 1, at + 1)
    input.dispatchEvent(new InputEvent("input", { bubbles: true, data: "茶", isComposing: true }))
    input.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: "茶" }))
  })
  await expect(field).toHaveAttribute("data-entry-identity", "same")
  await expect(field).toBeFocused()
  await expect(field).toHaveValue(`Fresh 茶${value}`)
  expect(await field.evaluate((node) => (node as HTMLInputElement).selectionStart)).toBe(7)
}

test("ordinary entry keeps the recipe primary, protects navigation, and settles what it kept", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 375, height: 720 })
  await page.goto("/recipes/new")
  await expect(page.getByRole("textbox", { name: "Group name", exact: true })).toBeHidden()
  await expect(page.getByRole("textbox", { name: "Stage name", exact: true })).toBeHidden()
  await compose(page.getByRole("textbox", { name: "Title", exact: true }), "Soup")
  const ingredient = page.getByRole("textbox", { name: "Ingredient", exact: true })
  await compose(ingredient, "carrots")
  await ingredient.press("Enter")
  await expect(page.getByRole("textbox", { name: "Ingredient", exact: true }).nth(1)).toBeFocused()
  await ingredient.nth(1).pressSequentially("water")
  await page.getByRole("textbox", { name: "Step 1", exact: true }).fill("Simmer until tender.")
  await page.getByRole("textbox", { name: "Step 1", exact: true }).press("Control+Enter")
  await expect(page.getByRole("textbox", { name: "Step 2", exact: true })).toBeFocused()
  await page.getByRole("textbox", { name: "Step 2", exact: true }).fill("Serve warm.")
  await openSection(page, /^Yield and timing/)
  await compose(page.getByRole("textbox", { name: "How many", exact: true }), "one pot")
  await page.getByRole("textbox", { name: "Of what", exact: true }).focus()
  const beforeUnload = page.waitForEvent("dialog")
  const reload = page.reload({ timeout: 2_000 }).catch(() => undefined)
  const warning = await beforeUnload
  expect(warning.type()).toBe("beforeunload")
  await warning.dismiss()
  await reload
  await expect(page.getByRole("textbox", { name: "Title", exact: true })).toHaveValue(
    "Fresh 茶Soup",
  )
  await page.getByRole("button", { name: "Cancel", exact: true }).click()
  const leave = page.getByRole("dialog", { name: "Leave without saving?" })
  await expect(leave).toBeVisible()
  await leave.getByRole("button", { name: "Keep editing" }).click()
  await expect(page.getByRole("textbox", { name: "Title", exact: true })).toHaveValue(
    "Fresh 茶Soup",
  )
  await page.getByRole("button", { name: "Save recipe", exact: true }).click()
  await expect(page.getByRole("link", { name: "Edit", exact: true })).toBeVisible()
  await expect(page).toHaveURL(/\/recipes\/r[a-z0-9]+$/)
  const id = page.url().split("/").at(-1)
  const saved = (await (await request.get(`/api/v1/recipes/${id}`)).json()).recipe
  expect(saved.yields[0].text).toBe("Fresh 茶one pot")
  expect(saved.ingredient_sections[0].ingredients).toHaveLength(2)
  await page.goto("/recipes/new")
  await expect(page.getByRole("textbox", { name: "Title", exact: true })).toHaveValue("")
  await page.getByRole("textbox", { name: "Title", exact: true }).fill("Discard this")
  await page.getByRole("link", { name: "Recipes", exact: true }).click()
  await page.getByRole("button", { name: "Discard changes", exact: true }).click()
  await expect(page).toHaveURL(/\/$/)
  await page.goto("/recipes/new")
  await expect(page.getByRole("textbox", { name: "Title", exact: true })).toHaveValue("")
})

test("yield blur and save keep the original unit and amount metadata", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Measured yield", {
    yields: [
      {
        quantity: {
          kind: "simple",
          amount: { value: "4", approximate: true },
          unit: "count.serving",
        },
        label: "servings",
        text: "About four generous bowls",
      },
    ],
  })
  const before = (await (await request.get(`/api/v1/recipes/${recipe.id}`)).json()).recipe
  await page.goto(`/recipes/${recipe.id}/edit`)
  await openSection(page, /^Yield and timing/)
  await page.getByRole("textbox", { name: "How many", exact: true }).focus()
  await page.getByRole("textbox", { name: "How many", exact: true }).press("Tab")
  await page.getByRole("button", { name: "Save changes", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  expect((await (await request.get(`/api/v1/recipes/${recipe.id}`)).json()).recipe.yields).toEqual(
    before.yields,
  )
  await page.goto(`/recipes/${recipe.id}/edit`)
  await choose(chooser(page, "Measurements"), "metric")
  await page.getByRole("button", { name: "Cancel", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
})

test("imported recipe retains comprehensive values on an unchanged save", async ({
  page,
  request,
}) => {
  const shape = "alternatives" as const
  const simple = (value: string, unit = "count.item") => ({
    kind: "simple",
    amount: { value },
    unit,
  })
  const quantity = {
    kind: "alternatives",
    options: [simple("2"), { kind: "sum", terms: [simple("1"), simple("1/2")] }],
  }
  const source = {
    sref: { version: "0.4.1", unit_registry: "0.2.0" },
    id: `entry-${shape}`,
    title: `Imported ${shape} supper`,
    source: {
      name: "Family notebook",
      url: `https://example.org/supper-${shape}`,
      application: "example.notebook",
      source_id: `supper-${shape}`,
      retrieved_at: "2023-01-02T03:04:05Z",
      attribution: "Family collection",
    },
    authors: [{ name: "A. Cook", kind: "person" }],
    published: "2020-01-02",
    modified: "2022-02-03",
    source_locale: "en-CA",
    language: "en-CA",
    yields: [{ quantity, label: "dishes", text: "Enough for the table" }],
    future_recipe: { empty: [], blank: "", missing: null },
    "x-org.example.review": { empty: [], blank: "", missing: null },
    times: {
      total: { min: "PT1H", max: "PT2H", source_text: "one to two hours" },
      assertions: [
        {
          id: "wait",
          kind: "rest",
          attention: "passive",
          duration: { min: "PT10M", max: "PT15M" },
          note: "While preparing the sauce",
        },
      ],
    },
    ingredient_sections: [
      {
        id: "base",
        ingredients: [
          { id: "flour", name: "flour", quantity, source_text: "The flour as written" },
        ],
      },
      {
        id: "sauce",
        title: "Sauce",
        ingredients: [
          {
            id: "choice",
            relation: "or",
            alternatives: [
              { id: "milk", name: "milk" },
              { id: "water", name: "water" },
            ],
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
            text: "Mix the flour.",
            ingredient_uses: [{ ingredient_ref: "flour", quantity: simple("1/2") }],
            timing_refs: ["wait"],
          },
        ],
      },
      {
        id: "finish",
        title: "Finish",
        steps: Array.from({ length: 35 }, (_, i) => ({
          id: `finish-${i}`,
          text: `Continue gently, stage ${i + 1}.`,
          ...(i === 0 ? { timing_refs: ["wait"] } : {}),
        })),
      },
    ],
  }
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles({
    name: `${shape}.recipe.json`,
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(source)),
  })
  const review = page.getByRole("region", { name: "Save this recipe" })
  const save = review.getByRole("button", { name: "Save recipe", exact: true })
  await expect(save).toBeVisible()
  const decide = review.getByRole("checkbox", { name: `Import ${source.title}`, exact: true })
  if ((await decide.count()) > 0) await decide.check()
  await save.click()
  await page.getByRole("link", { name: source.title, exact: true }).click()
  const id = page.url().split("/").at(-1)
  const before = (await (await request.get(`/api/v1/recipes/${id}`)).json()).recipe
  expect(before.sref_opaque_raw).toBeTruthy()
  await page.getByRole("link", { name: "Edit", exact: true }).click()
  await page.setViewportSize({ width: 320, height: 700 })
  await page.getByRole("button", { name: "More options for flour", exact: true }).click()
  await page.getByRole("menuitem", { name: "Details, flour", exact: true }).click()
  await expect(
    page.getByRole("textbox", { name: "As the source wrote it", exact: true }),
  ).toHaveValue("The flour as written")
  await page.getByRole("button", { name: "Done", exact: true }).click()
  await openSection(page, /^Yield and timing/)
  await page.getByRole("button", { name: "Yield details", exact: true }).click()
  await expect(page.getByRole("textbox", { name: "Yield as written", exact: true })).toHaveValue(
    "Enough for the table",
  )
  await page.getByRole("button", { name: "Done", exact: true }).click()
  await page.getByText("Source and attribution", { exact: true }).click()
  await expect(
    page.getByRole("textbox", { name: "Recipe's credited source", exact: true }),
  ).toHaveValue("Family notebook")
  await page.getByRole("textbox", { name: "Recipe's credited source", exact: true }).focus()
  await page.keyboard.press("Tab")
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole("button", { name: "Save changes", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`))
  const after = (await (await request.get(`/api/v1/recipes/${id}`)).json()).recipe
  expect(after).toEqual(before)
})

test("timing details retain composition, ranges and shared step relationships", async ({
  page,
  request,
}) => {
  const record = await seedRecipe(request, "Timing details supper", {
    times: {
      prep: { text: "overnight" },
      total: { min: "PT1H", max: "PT2H", source_text: "One to two hours" },
      assertions: [
        {
          id: "rest",
          kind: "rest",
          duration: { min: "PT10M", max: "PT15M" },
          note: "While making sauce",
          attention: "passive",
        },
      ],
    },
    instruction_sections: [
      {
        id: "method",
        steps: [
          { id: "mix", text: "Mix.", timing_refs: ["rest"] },
          { id: "finish", text: "Serve.", timing_refs: ["rest"] },
        ],
      },
    ],
  })
  await page.goto(`/recipes/${record.id}/edit`)
  await openSection(page, /^Yield and timing/)
  const preparation = page.getByRole("textbox", { name: "Preparation time", exact: true })
  await preparation.focus()
  await preparation.press("Tab")
  await page.getByRole("button", { name: "Range and source wording", exact: true }).nth(2).click()
  const maximum = page.getByRole("textbox", { name: "Up to", exact: true })
  await maximum.fill("3 hr")
  await compose(page.getByRole("textbox", { name: "Time as written", exact: true }), "three hours")
  await page.getByRole("button", { name: "Done", exact: true }).click()
  await page.getByRole("button", { name: "More options for step 1", exact: true }).click()
  await page.getByRole("menuitem", { name: "Timing, step 1", exact: true }).click()
  await page.getByText("Timing details", { exact: true }).click()
  await compose(page.getByRole("textbox", { name: "Timing name", exact: true }), "Rest")
  await compose(
    page.getByRole("textbox", { name: "Timing note", exact: true }),
    "While making sauce",
  )
  await choose(chooser(page, "During this time"), "active")
  await page.getByRole("button", { name: "Done", exact: true }).click()
  await page.getByRole("button", { name: "Save changes", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${record.id}$`))
  const saved = (await (await request.get(`/api/v1/recipes/${record.id}`)).json()).recipe
  expect(saved.times.prep).toEqual({ text: "overnight" })
  expect(saved.times.total.max).toBe("PT3H")
  expect(saved.times.assertions[0]).toMatchObject({
    id: "rest",
    attention: "active",
    note: "Fresh 茶While making sauce",
    label: "Fresh 茶Rest",
    duration: { min: "PT10M", max: "PT15M" },
  })
  expect(
    saved.instruction_sections[0].steps.map((step: { timing_refs: string[] }) => step.timing_refs),
  ).toEqual([["rest"], ["rest"]])
})

test("source records and package ranges remain editable with stable inputs", async ({
  page,
  request,
}) => {
  const record = await seedRecipe(request, "Source and package supper", {
    source: {
      name: "Notebook",
      application: "example.notebook",
      source_id: "supper-1",
      retrieved_at: "2020-01-02T03:04:05Z",
      attribution: "Original credit",
    },
  })
  await page.goto(`/recipes/${record.id}/edit`)
  await openSection(page, /^Source and attribution/)
  await page.getByRole("button", { name: /^Source record/ }).press("Enter")
  await expect(page.getByRole("dialog", { name: "Source record", exact: true })).toBeVisible()
  await compose(page.getByRole("textbox", { name: "Source application", exact: true }), "Notebook")
  await compose(
    page.getByRole("textbox", { name: "Recipe identifier in source", exact: true }),
    "supper",
  )
  const retrieved = page.getByRole("textbox", { name: "Retrieved at", exact: true })
  await retrieved.fill("2024-01-02T03:04:05Z")
  await compose(page.getByRole("textbox", { name: "Attribution", exact: true }), "Family credit")
  await page.getByRole("button", { name: "Done", exact: true }).click()
  await page
    .getByRole("button", { name: "More options for whole-milk yoghurt", exact: true })
    .click()
  await page.getByRole("menuitem", { name: "Details, whole-milk yoghurt", exact: true }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByText("Package size", { exact: true }).click()
  const amount = dialog.getByRole("textbox", { name: "Package size", exact: true })
  await amount.fill("2-3")
  await amount.press("Tab")
  await choose(
    dialog.getByRole("combobox", { name: "Package size measure", exact: true }),
    "mass.gram",
  )
  await dialog.getByRole("button", { name: "Done", exact: true }).click()
  await page.getByRole("button", { name: "Save changes", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${record.id}$`))
  const saved = (await (await request.get(`/api/v1/recipes/${record.id}`)).json()).recipe
  expect(saved.source).toMatchObject({
    application: "Fresh 茶Notebook",
    source_id: "Fresh 茶supper",
    retrieved_at: "2024-01-02T03:04:05Z",
    attribution: "Fresh 茶Family credit",
  })
  expect(saved.ingredient_sections[0].ingredients[0].package_size.amount).toEqual({
    min: "2",
    max: "3",
  })
})
