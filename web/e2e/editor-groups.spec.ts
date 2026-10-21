import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "@playwright/test"

import { chooseAction } from "./controls"
import { seedRecipe } from "./library"

const sections = [
  {
    id: "prepare",
    title: "Prepare",
    steps: [
      { id: "mix", text: "Mix the pastry." },
      {
        id: "chill",
        title: "Rest the dough",
        text: "Chill the dough.",
        temperatures: [{ text: "Refrigerator temperature" }],
      },
    ],
  },
  { id: "rest", title: "Rest", steps: [{ id: "pause", text: "Let the pastry rest." }] },
  { id: "bake", title: "Bake", steps: [{ id: "oven", text: "Bake until golden." }] },
]

test("ingredient move actions cross groups and preserve details, focus, and editing", async ({
  page,
  request,
}) => {
  const water = {
    id: "water",
    name: "Water",
    note: "For the dough",
    optional: true,
    quantity: { kind: "simple", amount: { value: "1" }, unit: "volume.cup.us.customary" },
  }
  const recipe = await seedRecipe(request, "Move ingredients between groups", {
    ingredient_sections: [
      { id: "dough", title: "Dough", ingredients: [{ id: "flour", name: "Flour" }, water] },
      { id: "resting", title: "Rest", ingredients: [{ id: "sugar", name: "Sugar" }] },
      { id: "finish", title: "Finish", ingredients: [{ id: "salt", name: "Salt" }] },
    ],
  })
  await page.goto(`/recipes/${recipe.id}/edit`)
  const rest = page.locator('[data-editor-id="resting"]')
  const waterRow = page.locator('[data-editor-id="water"]')
  await chooseAction(page, "More options for Sugar", "Move down, Sugar")
  await chooseAction(page, "More options for Water", "Move down, Water")
  await expect(rest.locator('[data-editor-id="water"]')).toBeVisible()
  await expect(waterRow.getByRole("textbox", { name: "Ingredient", exact: true })).toBeFocused()
  await expect(page.locator('form > [aria-live="polite"]')).toContainText(
    "Moved Water to position 1.",
  )
  await chooseAction(page, "More options for Water", "Move up, Water")
  await chooseAction(page, "More options for Water", "Move up, Water")
  await chooseAction(page, "More options for Water", "Move down, Water")
  await chooseAction(page, "More options for Water", "Move down, Water")
  await chooseAction(page, "More options for Water", "Move down, Water")
  await chooseAction(page, "More options for Water", "Move up, Water")
  await expect(rest.locator('[data-editor-id="water"]')).toBeVisible()
  await page.getByRole("button", { name: "Save changes", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  const saved = (await (await request.get(`/api/v1/recipes/${recipe.id}`)).json()).recipe
  expect(saved.ingredient_sections[1].ingredients).toEqual([water])
  expect(
    saved.ingredient_sections.map((section: { ingredients: { id: string }[] }) =>
      section.ingredients.map((ingredient) => ingredient.id),
    ),
  ).toEqual([["flour"], ["water"], ["sugar", "salt"]])

  await page.goto(`/recipes/${recipe.id}/edit`)
  await chooseAction(page, "More options for Water", "Move up, Water")
  const input = waterRow.getByRole("textbox", { name: "Ingredient", exact: true })
  await input.evaluate((element) => ((element as HTMLInputElement).dataset.identity = "retained"))
  await input.fill("")
  await input.pressSequentially("Warm water")
  await expect(input).toBeFocused()
  await expect(input).toHaveValue("Warm water")
  await input.evaluate((element) => {
    const field = element as HTMLInputElement
    field.setSelectionRange(5, 5)
    field.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true }))
    field.setRangeText("茶", 5, 5, "end")
    field.dispatchEvent(
      new InputEvent("input", {
        bubbles: true,
        data: "茶",
        inputType: "insertCompositionText",
        isComposing: true,
      }),
    )
    field.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: "茶" }))
  })
  await expect(input).toBeFocused()
  await expect(input).toHaveAttribute("data-identity", "retained")
  await expect(input).toHaveValue("Warm 茶water")
  expect(await input.evaluate((element: HTMLInputElement) => element.selectionStart)).toBe(6)
})

test("an ingredient choice moves between groups with its alternatives intact", async ({
  page,
  request,
}) => {
  const choice = {
    id: "fat",
    relation: "or",
    note: "For the pastry",
    alternatives: [
      { id: "butter", name: "Butter" },
      { id: "oil", name: "Oil" },
    ],
  }
  const recipe = await seedRecipe(request, "Move an ingredient choice", {
    ingredient_sections: [
      { id: "first", title: "Pastry", ingredients: [choice] },
      { id: "second", title: "Filling", ingredients: [{ id: "sugar", name: "Sugar" }] },
    ],
  })
  await page.goto(`/recipes/${recipe.id}/edit`)
  const fat = page.locator('[data-editor-id="fat"]')
  await expect(fat.getByRole("button", { name: "Move choice up", exact: true })).toBeDisabled()
  await fat.getByRole("button", { name: "Move choice down", exact: true }).click()
  await expect(page.locator('[data-editor-id="second"] [data-editor-id="fat"]')).toBeVisible()
  await expect(fat.getByRole("combobox", { name: "Choice wording", exact: true })).toBeFocused()
  await fat.getByRole("button", { name: "Move choice up", exact: true }).click()
  await expect(page.locator('[data-editor-id="first"] [data-editor-id="fat"]')).toBeVisible()
  await page.getByRole("button", { name: "Save changes", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  const saved = (await (await request.get(`/api/v1/recipes/${recipe.id}`)).json()).recipe
  expect(saved.ingredient_sections[0].ingredients).toEqual([choice])
})

test("step move buttons cross stage boundaries and preserve content, focus, and editing", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Move steps between stages", {
    instruction_sections: sections,
  })
  await page.goto(`/recipes/${recipe.id}/edit`)
  const prepare = page.locator('[data-editor-id="prepare"]')
  const rest = page.locator('[data-editor-id="rest"]')
  const bake = page.locator('[data-editor-id="bake"]')
  const chill = page.locator('[data-editor-id="chill"]')

  await expect(prepare.getByRole("button", { name: "Move step 1 up", exact: true })).toBeDisabled()
  await expect(bake.getByRole("button", { name: "Move step 1 down", exact: true })).toBeDisabled()
  await rest.getByRole("button", { name: "Move step 1 down", exact: true }).press("Enter")
  await prepare.getByRole("button", { name: "Move step 2 down", exact: true }).press("Enter")
  await expect(rest.locator('[data-editor-id="chill"]')).toBeVisible()
  await expect(chill.getByRole("textbox", { name: "Step 1", exact: true })).toBeFocused()
  await expect(page.locator('form > [aria-live="polite"]')).toContainText(
    "Moved step to position 1.",
  )

  await rest.getByRole("button", { name: "Move step 1 down", exact: true }).press("Space")
  await expect(bake.locator('[data-editor-id="chill"]')).toBeVisible()
  await expect(bake.getByRole("textbox", { name: "Step 1", exact: true })).toHaveValue(
    "Chill the dough.",
  )
  await bake.getByRole("button", { name: "Move step 1 up", exact: true }).press("Enter")
  await rest.getByRole("button", { name: "Move step 1 up", exact: true }).press("Enter")
  await expect(prepare.getByRole("textbox", { name: "Step 2", exact: true })).toBeFocused()

  await prepare.getByRole("button", { name: "Move step 2 up", exact: true }).press("Enter")
  await expect(prepare.getByRole("textbox", { name: "Step 1", exact: true })).toHaveValue(
    "Chill the dough.",
  )
  await prepare.getByRole("button", { name: "Move step 1 down", exact: true }).press("Enter")
  await prepare.getByRole("button", { name: "Move step 2 down", exact: true }).press("Enter")

  const text = chill.getByRole("textbox", { name: "Step 1", exact: true })
  await text.press("End")
  await text.pressSequentially(" Keep covered.")
  await text.evaluate((element) => {
    const input = element as HTMLTextAreaElement
    input.dataset.identity = "retained"
    input.setSelectionRange(6, 6)
    input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true }))
    input.setRangeText("茶", 6, 6, "end")
    input.dispatchEvent(
      new InputEvent("input", {
        bubbles: true,
        data: "茶",
        inputType: "insertCompositionText",
        isComposing: true,
      }),
    )
    input.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: "茶" }))
  })
  await expect(text).toBeFocused()
  await expect(text).toHaveAttribute("data-identity", "retained")
  await expect(text).toHaveValue("Chill 茶the dough. Keep covered.")
  expect(await text.evaluate((input: HTMLTextAreaElement) => input.selectionStart)).toBe(7)

  await page.getByRole("button", { name: "Save changes", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  const saved = (await (await request.get(`/api/v1/recipes/${recipe.id}`)).json()).recipe
  expect(saved.instruction_sections[0].steps.map((step: { id: string }) => step.id)).toEqual([
    "mix",
  ])
  expect(saved.instruction_sections[1].steps).toEqual([
    { ...sections[0].steps[1], text: "Chill 茶the dough. Keep covered." },
  ])
})

test("steps drag across stages, into an empty stage, and after its last step", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Drag steps between stages", {
    instruction_sections: sections,
  })
  await page.goto(`/recipes/${recipe.id}/edit`)
  const chill = page.locator('[data-editor-id="chill"]')
  const rest = page.locator('[data-editor-id="rest"]')
  const bake = page.locator('[data-editor-id="bake"]')
  await rest.getByRole("button", { name: "Move step 1 down", exact: true }).click()
  await chill.locator("[data-drag-handle]").dragTo(bake.locator('[data-editor-id="oven"]'))
  await expect(bake.getByRole("textbox", { name: "Step 2", exact: true })).toHaveValue(
    "Chill the dough.",
  )
  await expect(chill.getByRole("textbox", { name: "Step 2", exact: true })).toBeFocused()
  await chill
    .locator("[data-drag-handle]")
    .dragTo(rest.getByRole("textbox", { name: "Stage name" }))
  await expect(rest.locator('[data-editor-id="chill"]')).toBeVisible()
  await chill
    .locator("[data-drag-handle]")
    .dragTo(bake.getByRole("button", { name: "Step", exact: true }))
  await expect(bake.getByRole("textbox", { name: "Step 3", exact: true })).toHaveValue(
    "Chill the dough.",
  )
  await expect(chill.getByRole("textbox", { name: "Step 3", exact: true })).toBeFocused()
})

for (const kind of ["ingredient", "step"] as const) {
  test(`${kind} groups require confirmation and support cancellation and keyboard`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    const recipe = await seedRecipe(request, `Confirm ${kind} group removal`, {
      instruction_sections: sections,
    })
    await page.goto(`/recipes/${recipe.id}/edit`)
    const group = page.locator(
      `[data-editor-id="${kind === "ingredient" ? "marinade" : "prepare"}"]`,
    )
    const remove = group.getByRole("button", {
      name: kind === "ingredient" ? "Remove group" : "Remove stage",
      exact: true,
    })
    const confirm = group.getByRole("button", { name: "Confirm removal", exact: true })
    const initial = await remove.boundingBox()
    await remove.click()
    await expect(confirm).toHaveAccessibleDescription(
      kind === "ingredient"
        ? "Remove this group and all its ingredients?"
        : "Remove this stage and all its steps?",
    )
    const confirming = await confirm.boundingBox()
    expect(initial).not.toBeNull()
    expect(confirming).not.toBeNull()
    expect(confirming!.x + confirming!.width).toBeCloseTo(initial!.x + initial!.width, 0)
    const cancelBox = await group.getByRole("button", { name: "Cancel", exact: true }).boundingBox()
    expect(cancelBox).not.toBeNull()
    expect(Math.abs(cancelBox!.y - confirming!.y)).toBeLessThanOrEqual(4)
    await expect(confirm).toBeFocused()
    await expect(group.getByRole("textbox").first()).toBeVisible()
    const accessibility = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
      .analyze()
    expect(accessibility.violations).toEqual([])
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true)

    await confirm.press("Escape")
    await expect(remove).toBeFocused()
    await expect(confirm).toHaveCount(0)
    await expect(page.locator('form > [aria-live="polite"]')).toBeEmpty()
    await remove.press("Enter")
    await expect(page.locator('form > [aria-live="polite"]')).toContainText("Remove this")
    await group.getByRole("button", { name: "Cancel", exact: true }).click()
    await expect(remove).toBeFocused()
    await page.keyboard.down("Enter")
    await expect(confirm).toBeFocused()
    await page.keyboard.down("Enter")
    await expect(confirm).toBeFocused()
    await page.keyboard.up("Enter")
    await page.getByRole("textbox", { name: "Title", exact: true }).focus()
    await expect(confirm).toHaveCount(0)

    await remove.press("Space")
    await confirm.press("Space")
    await expect(group).toHaveCount(0)
    if (kind === "ingredient")
      await expect(page.getByRole("button", { name: "Ingredient", exact: true })).toBeFocused()
    else
      await expect(
        page.locator('[data-editor-id="rest"]').getByRole("textbox", { name: "Stage name" }),
      ).toBeFocused()
  })
}

test("a pointer double click confirms group removal without moving the target", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Double click group removal", {
    instruction_sections: [sections[0], { id: "bake", steps: sections[2].steps }],
  })
  await page.goto(`/recipes/${recipe.id}/edit`)
  const prepare = page.locator('[data-editor-id="prepare"]')
  await prepare.getByRole("button", { name: "Remove stage", exact: true }).dblclick()
  await expect(prepare).toHaveCount(0)
  await expect(
    page.getByRole("button", { name: "Add a heading Instructions", exact: true }),
  ).toBeFocused()
})

for (const kind of ["ingredient", "step"] as const) {
  test(`an unused ${kind} group removes immediately even with a heading`, async ({
    page,
    request,
  }) => {
    const recipe = await seedRecipe(request, `Remove empty ${kind} group`)
    await page.goto(`/recipes/${recipe.id}/edit`)
    await page.locator(kind === "ingredient" ? "#add-group" : "#add-stage").click()
    const heading = page
      .getByRole("textbox", {
        name: kind === "ingredient" ? "Group name" : "Stage name",
        exact: true,
      })
      .last()
    await heading.pressSequentially("Unused group")
    await expect(heading).toBeFocused()
    await expect(heading).toHaveValue("Unused group")
    await heading.evaluate((element) => {
      const input = element as HTMLInputElement
      input.dataset.identity = "retained"
      input.setSelectionRange(6, 6)
      input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true }))
      input.setRangeText("茶", 6, 6, "end")
      input.dispatchEvent(
        new InputEvent("input", {
          bubbles: true,
          data: "茶",
          inputType: "insertCompositionText",
          isComposing: true,
        }),
      )
      input.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: "茶" }))
    })
    await expect(heading).toBeFocused()
    await expect(heading).toHaveValue("Unused茶 group")
    await expect(heading).toHaveAttribute("data-identity", "retained")
    expect(await heading.evaluate((element: HTMLInputElement) => element.selectionStart)).toBe(7)
    const groupID = await heading.locator("../..").getAttribute("data-editor-id")
    const group = page.locator(`[data-editor-id="${groupID}"]`)
    await group
      .getByRole("textbox", {
        name: kind === "ingredient" ? "Ingredient" : "Step 1",
        exact: true,
      })
      .fill("  ")
    await group
      .getByRole("button", {
        name: kind === "ingredient" ? "Remove group" : "Remove stage",
        exact: true,
      })
      .click()
    await expect(group).toHaveCount(0)
    await expect(page.getByRole("button", { name: "Confirm removal", exact: true })).toHaveCount(0)
  })
}

test("a stage emptied by moving its last step removes immediately", async ({ page, request }) => {
  const recipe = await seedRecipe(request, "Remove stage after moving its step", {
    instruction_sections: sections,
  })
  await page.goto(`/recipes/${recipe.id}/edit`)
  const rest = page.locator('[data-editor-id="rest"]')
  const bake = page.locator('[data-editor-id="bake"]')
  await rest.getByRole("button", { name: "Move step 1 down", exact: true }).click()
  await expect(bake.getByRole("textbox", { name: "Step 1", exact: true })).toBeFocused()
  await rest.getByRole("button", { name: "Remove stage", exact: true }).click()
  await expect(rest).toHaveCount(0)
  await expect(bake.getByRole("textbox", { name: "Stage name", exact: true })).toBeFocused()
  await expect(bake.getByRole("textbox", { name: "Step 1", exact: true })).toHaveValue(
    "Let the pastry rest.",
  )
})

test("an ingredient group emptied by moving its last row removes immediately", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Remove group after moving its ingredient", {
    ingredient_sections: [
      { id: "first", title: "First", ingredients: [{ id: "milk", name: "Milk" }] },
      { id: "second", title: "Second", ingredients: [{ id: "flour", name: "Flour" }] },
    ],
  })
  await page.goto(`/recipes/${recipe.id}/edit`)
  const first = page.locator('[data-editor-id="first"]')
  const second = page.locator('[data-editor-id="second"]')
  await page
    .locator('[data-editor-id="milk"] [data-drag-handle]')
    .dragTo(page.locator('[data-editor-id="flour"]'))
  await expect(
    second.getByRole("textbox", { name: "Ingredient", exact: true }).first(),
  ).toHaveValue("Milk")
  await first.getByRole("button", { name: "Remove group", exact: true }).click()
  await expect(first).toHaveCount(0)
  await expect(second.getByRole("textbox", { name: "Group name", exact: true })).toBeFocused()
})

test.describe("touch group removal", () => {
  test.use({ hasTouch: true, isMobile: true, viewport: { width: 390, height: 844 } })

  test("two separate taps confirm removal", async ({ page, request }) => {
    const recipe = await seedRecipe(request, "Touch group removal", {
      instruction_sections: sections,
    })
    await page.goto(`/recipes/${recipe.id}/edit`)
    const prepare = page.locator('[data-editor-id="prepare"]')
    await prepare.getByRole("button", { name: "Remove stage", exact: true }).tap()
    await expect(
      prepare.getByRole("button", { name: "Confirm removal", exact: true }),
    ).toHaveAccessibleDescription("Remove this stage and all its steps?")
    await prepare.getByRole("button", { name: "Confirm removal", exact: true }).tap()
    await expect(prepare).toHaveCount(0)
  })
})
