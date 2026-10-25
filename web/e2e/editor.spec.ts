import { expect, test } from "@playwright/test"

import { choose, chooseAction, openSection } from "./controls"
import { seedRecipe } from "./library"

test("equipment editing retains focus and composition and renders the authored requirement", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Equipment editor")
  await page.goto(`/recipes/${recipe.id}/edit`)

  await page.getByRole("button", { name: "Add equipment" }).click()
  const name = page.getByRole("textbox", { name: "Equipment 1 name" })
  await name.pressSequentially("stand mixer")
  await name.evaluate((element) => {
    const input = element as HTMLInputElement
    input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }))
    input.value = "stand mixer 茶"
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
  await expect(name).toBeFocused()
  await expect(name).toHaveValue("stand mixer 茶")

  await page.getByRole("textbox", { name: "Equipment 1 quantity" }).fill("2")
  await page.getByRole("textbox", { name: "Equipment 1 note" }).fill("with bowls")
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))

  const equipment = page.getByRole("region", { name: "Equipment" })
  await expect(equipment).toContainText("2")
  await expect(equipment).toContainText("stand mixer 茶")
  await expect(equipment).toContainText("with bowls")

  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(stored.ok()).toBe(true)
  const body = (await stored.json()) as {
    recipe: {
      equipment: { name: string; note?: string; quantity?: { amount?: { value?: string } } }[]
    }
  }
  expect(body.recipe.equipment).toEqual([
    {
      name: "stand mixer 茶",
      note: "with bowls",
      quantity: { kind: "simple", amount: { value: "2" } },
    },
  ])
})

test("an optional step title keeps focus through typing and composition", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Step title editor")
  await page.goto(`/recipes/${recipe.id}/edit`)

  await page.getByRole("button", { name: "More options for step 1", exact: true }).click()
  await page.getByRole("menuitem", { name: "Add title, step 1", exact: true }).click()
  const title = page.getByRole("textbox", { name: "Title for step 1" })
  await title.pressSequentially("Make the roux")
  await title.evaluate((element) => {
    const input = element as HTMLInputElement
    input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }))
    input.value = "Make the roux 茶"
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
  await expect(title).toBeFocused()
  await expect(title).toHaveValue("Make the roux 茶")

  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  await expect(page.getByRole("heading", { name: "Make the roux 茶", level: 4 })).toBeVisible()

  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(stored.ok()).toBe(true)
  const body = (await stored.json()) as {
    recipe: { instruction_sections: { steps: { title?: string }[] }[] }
  }
  expect(body.recipe.instruction_sections[0].steps[0].title).toBe("Make the roux 茶")
})

test("editing authored metadata keeps list identity, ordering, and source chronology", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Authored metadata editor", {
    categories: ["Cake", "Celebration cake"],
    courses: ["Dessert"],
    cuisines: ["British"],
    cooking_methods: ["Baking"],
    keywords: ["layered"],
    published: "2023-05-04",
    modified: "2025-06-07T08:09:10-06:00",
  })

  await page.goto(`/recipes/${recipe.id}/edit`)
  await openSection(page, /^Categories and keywords/)

  const category = page.getByRole("textbox", { name: "Categories 1" })
  await category.fill("")
  await category.pressSequentially("Layer")
  await category.evaluate((element) => {
    const input = element as HTMLInputElement
    input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }))
    input.value = "Layer cake 茶"
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
  await expect(category).toBeFocused()
  await expect(category).toHaveValue("Layer cake 茶")
  await expect(page.getByRole("textbox", { name: "Categories 2" })).toHaveValue("Celebration cake")

  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  const details = page.getByRole("complementary", { name: "Recipe details" })
  await details.getByRole("button", { name: "From the source page" }).click()
  await expect(page.getByText("Layer cake 茶, Celebration cake")).toBeVisible()
  await expect(details.getByRole("definition")).toHaveText([
    "Layer cake 茶, Celebration cake",
    "Baking",
    "layered",
    "May 4, 2023",
    "Jun 7, 2025",
  ])

  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(stored.ok()).toBe(true)
  const body = (await stored.json()) as {
    recipe: {
      categories: string[]
      courses: string[]
      cuisines: string[]
      cooking_methods: string[]
      keywords: string[]
      published: string
      modified: string
    }
  }
  expect(body.recipe).toEqual(
    expect.objectContaining({
      categories: ["Layer cake 茶", "Celebration cake"],
      courses: ["Dessert"],
      cuisines: ["British"],
      cooking_methods: ["Baking"],
      keywords: ["layered"],
      published: "2023-05-04",
      modified: "2025-06-07T08:09:10-06:00",
    }),
  )
})

test("editing an author keeps focus and preserves structured credit metadata", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Structured author editor", {
    authors: [
      {
        name: "A. Cook",
        kind: "person",
        url: "https://example.test/authors/a-cook",
      },
    ],
  })

  await page.goto(`/recipes/${recipe.id}/edit`)
  await openSection(page, /^Source and attribution/)

  const author = page.getByRole("textbox", { name: "Author 1" })
  await author.fill("")
  await author.pressSequentially("Alex Cook")
  await author.evaluate((element) => {
    const input = element as HTMLInputElement
    input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }))
    input.value = "Alex Cook 茶"
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
  await expect(author).toBeFocused()
  await expect(author).toHaveValue("Alex Cook 茶")

  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))

  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(stored.ok()).toBe(true)
  const body = (await stored.json()) as {
    recipe: { authors: { name: string; kind?: string; url?: string }[] }
  }
  expect(body.recipe.authors).toEqual([
    {
      name: "Alex Cook 茶",
      kind: "person",
      url: "https://example.test/authors/a-cook",
    },
  ])
})

test("editing a shared step timing keeps its identity and complete typed value", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Shared timing editor", {
    times: {
      assertions: [
        {
          id: "shared-rest",
          kind: "rest",
          duration: { value: "PT5M" },
          provenance: "supplemental",
        },
      ],
    },
    instruction_sections: [
      {
        id: "method",
        steps: [
          { id: "s1", text: "Let the dough relax.", timing_refs: ["shared-rest"] },
          { id: "s2", text: "Shape it when ready.", timing_refs: ["shared-rest"] },
        ],
      },
    ],
  })

  await page.goto(`/recipes/${recipe.id}/edit`)
  await chooseAction(page, "More options for step 1", "Timing, step 1")

  const duration = page.getByRole("textbox", { name: "Timing duration" })
  await expect(duration).toBeFocused()
  await duration.fill("")
  await duration.pressSequentially("12 min")
  await expect(duration).toBeFocused()
  await expect(duration).toHaveValue("12 min")

  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))

  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(stored.ok()).toBe(true)
  const body = (await stored.json()) as {
    recipe: {
      times: { assertions: { id: string; duration: { value?: string } }[] }
      instruction_sections: { steps: { timing_refs?: string[] }[] }[]
    }
  }
  expect(body.recipe.times.assertions).toEqual([
    expect.objectContaining({ id: "shared-rest", duration: { value: "PT12M" } }),
  ])
  expect(body.recipe.instruction_sections[0].steps[0].timing_refs).toEqual(["shared-rest"])
  expect(body.recipe.instruction_sections[0].steps[1].timing_refs).toEqual(["shared-rest"])
})

test("editing a compound quantity alternative keeps its identity and complete typed value", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Compound alternative editor", {
    ingredient_sections: [
      {
        id: "ingredients",
        ingredients: [
          {
            id: "oil",
            name: "oil",
            quantity: {
              kind: "alternatives",
              options: [
                {
                  kind: "sum",
                  terms: [
                    { kind: "simple", amount: { value: "2" }, unit: "volume.tablespoon.us" },
                    { kind: "simple", amount: { value: "2" }, unit: "volume.teaspoon.us" },
                  ],
                },
                {
                  kind: "simple",
                  amount: { value: "40" },
                  unit: "volume.milliliter",
                },
              ],
            },
          },
        ],
      },
    ],
  })

  await page.goto(`/recipes/${recipe.id}/edit`)
  await page.getByRole("button", { name: "More options for oil" }).click()
  await page.getByRole("menuitem", { name: "Details, oil", exact: true }).click()

  const dialog = page.getByRole("dialog")
  await expect
    .poll(() => dialog.evaluate((element) => element.contains(document.activeElement)))
    .toBe(true)
  const amount = dialog.getByRole("textbox", { name: "Alternative 1 amount 1" })
  await expect(amount).toHaveValue("2")
  await amount.fill("")
  await expect(amount).toHaveValue("")
  await amount.pressSequentially("12")
  await expect(amount).toBeFocused()
  await expect(amount).toHaveValue("12")

  await page.getByRole("button", { name: "Done" }).click()
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))

  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(stored.ok()).toBe(true)
  const body = (await stored.json()) as {
    recipe: {
      ingredient_sections: {
        ingredients: {
          quantity: {
            kind: string
            options: {
              kind: string
              amount?: { value?: string }
              unit?: string
              terms?: { kind: string; amount?: { value?: string }; unit?: string }[]
            }[]
          }
        }[]
      }[]
    }
  }
  expect(body.recipe.ingredient_sections[0].ingredients[0].quantity).toEqual({
    kind: "alternatives",
    options: [
      {
        kind: "sum",
        terms: [
          { kind: "simple", amount: { value: "12" }, unit: "volume.tablespoon.us" },
          { kind: "simple", amount: { value: "2" }, unit: "volume.teaspoon.us" },
        ],
      },
      {
        kind: "simple",
        amount: { value: "40" },
        unit: "volume.milliliter",
      },
    ],
  })
})

test("editing a divided ingredient use keeps focus and stores its structured quantity", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Divided ingredient editor", {
    ingredient_sections: [
      {
        id: "ingredients",
        ingredients: [
          {
            id: "sugar",
            name: "sugar",
            note: "divided",
            quantity: {
              kind: "simple",
              amount: { value: "2" },
              unit: "volume.cup.us.customary",
            },
          },
        ],
      },
    ],
    instruction_sections: [
      {
        id: "method",
        steps: [{ id: "mix", text: "Mix half a cup of the sugar." }],
      },
    ],
  })

  await page.goto(`/recipes/${recipe.id}/edit`)
  await page.getByRole("button", { name: "More options for step 1", exact: true }).click()
  await page
    .getByRole("menuitem", { name: "Ingredients, temperature, and photos, step 1", exact: true })
    .click()
  // The choice reads as the editor now holds the ingredient, amount and all.
  await page.getByRole("checkbox", { name: "Use 2 cups sugar, divided" }).check()

  const amount = page.getByRole("textbox", { name: "Amount of sugar, divided used" })
  await amount.pressSequentially("1/")
  await amount.evaluate((element) => {
    const input = element as HTMLInputElement
    input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }))
    input.value = "1/2"
    input.dispatchEvent(
      new InputEvent("input", {
        bubbles: true,
        data: "2",
        inputType: "insertCompositionText",
        isComposing: true,
      }),
    )
    input.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: "2" }))
  })
  await expect(amount).toBeFocused()
  await expect(amount).toHaveValue("1/2")
  await choose(
    page.getByRole("combobox", { name: "Measure for sugar, divided used" }),
    "volume.cup.us.customary",
  )

  await page.getByRole("button", { name: "Done" }).click()
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  // The reader does not repeat under a step the ingredients it uses; the list
  // is already beside the method. What the step uses is carried into cooking.
  await expect(page.getByText("Uses sugar", { exact: true })).toHaveCount(0)
  await page.getByRole("button", { name: "Cook", exact: true }).click()
  await expect(page.getByRole("checkbox", { name: /sugar, divided/ })).toBeVisible()
  await page.getByRole("button", { name: "Exit cooking", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))

  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(stored.ok()).toBe(true)
  const body = (await stored.json()) as {
    recipe: {
      instruction_sections: {
        steps: {
          ingredient_uses?: {
            ingredient_ref: string
            quantity?: { kind: string; amount?: { value?: string }; unit?: string }
          }[]
        }[]
      }[]
    }
  }
  expect(body.recipe.instruction_sections[0].steps[0].ingredient_uses).toEqual([
    {
      ingredient_ref: "sugar",
      quantity: {
        kind: "simple",
        amount: { value: "1/2" },
        unit: "volume.cup.us.customary",
      },
    },
  ])
})

test("editing multiple temperature purposes keeps focus and preserves each purpose", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Temperature purpose editor", {
    instruction_sections: [
      {
        id: "method",
        steps: [
          {
            id: "heat",
            text: "Heat carefully.",
            temperatures: [
              {
                amount: { value: "350" },
                unit: "temperature.fahrenheit",
              },
              {
                amount: { value: "240" },
                unit: "temperature.fahrenheit",
              },
            ],
          },
        ],
      },
    ],
  })

  await page.goto(`/recipes/${recipe.id}/edit`)
  await page.getByRole("button", { name: "More options for step 1", exact: true }).click()
  await page
    .getByRole("menuitem", { name: "Ingredients, temperature, and photos, step 1", exact: true })
    .click()
  await choose(page.getByRole("combobox", { name: "Applies to temperature 1" }), "oil")
  await choose(page.getByRole("combobox", { name: "Applies to temperature 2" }), "other")

  const label = page.getByRole("textbox", { name: "Other purpose for temperature 2" })
  await label.pressSequentially("sugar ")
  await label.evaluate((element) => {
    const input = element as HTMLInputElement
    input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }))
    input.value = "sugar syrup"
    input.dispatchEvent(
      new InputEvent("input", {
        bubbles: true,
        data: "syrup",
        inputType: "insertCompositionText",
        isComposing: true,
      }),
    )
    input.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: "syrup" }))
  })
  await expect(label).toBeFocused()
  await expect(label).toHaveValue("sugar syrup")

  await page.getByRole("button", { name: "Done" }).click()
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))

  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(stored.ok()).toBe(true)
  const body = (await stored.json()) as {
    recipe: {
      instruction_sections: {
        steps: {
          temperatures?: { purpose?: { kind: string; label?: string } }[]
        }[]
      }[]
    }
  }
  expect(body.recipe.instruction_sections[0].steps[0].temperatures).toEqual([
    expect.objectContaining({ purpose: { kind: "oil" } }),
    expect.objectContaining({ purpose: { kind: "other", label: "sugar syrup" } }),
  ])
})

test("recipe photo order, primary selection, and step reuse stay independent", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Explicit image relationships")

  await page.goto(`/recipes/${recipe.id}/edit`)
  await openSection(page, /^Photos$/)
  await page.locator('input[type="file"][accept="image/*"]').evaluate((input, pixelBase64) => {
    const bytes = Uint8Array.from(atob(pixelBase64), (character) => character.charCodeAt(0))
    const distinctBytes = new Uint8Array(bytes.length + 1)
    distinctBytes.set(bytes)

    const transfer = new DataTransfer()
    transfer.items.add(new File([bytes], "first.png", { type: "image/png" }))
    transfer.items.add(new File([distinctBytes], "second.png", { type: "image/png" }))
    Object.defineProperty(input, "files", { value: transfer.files })
    input.dispatchEvent(new Event("change", { bubbles: true }))
  }, "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Z2S8AAAAASUVORK5CYII=")

  const descriptions = page.getByRole("textbox", { name: "Describe this photo" })
  await expect(descriptions).toHaveCount(2)
  await descriptions.nth(0).fill("First angle")
  await descriptions.nth(1).fill("Second angle")

  // Reordering the recipe relationship does not move or replace either asset
  // editor, and the original primary remains an explicit independent choice.
  await page.getByRole("button", { name: "Move recipe photo earlier" }).nth(1).click()
  await expect(descriptions.nth(0)).toHaveValue("First angle")
  await expect(descriptions.nth(1)).toHaveValue("Second angle")

  await page.getByRole("button", { name: "More options for step 1", exact: true }).click()
  await page
    .getByRole("menuitem", { name: "Ingredients, temperature, and photos, step 1", exact: true })
    .click()
  await page.getByRole("button", { name: "Second angle" }).click()
  await page.getByRole("button", { name: "Done" }).click()
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))

  await expect(page.getByRole("img", { name: "First angle" })).toBeVisible()
  await expect(
    page.getByRole("region", { name: "Recipe photos" }).getByRole("img", { name: "Second angle" }),
  ).toBeVisible()
  await expect(page.getByRole("img", { name: "Second angle" })).toHaveCount(2)

  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(stored.ok()).toBe(true)
  const body = (await stored.json()) as {
    recipe: {
      assets: { id: string; role?: string; alt?: string }[]
      image_refs: string[]
      primary_image: string
      instruction_sections: { steps: { image_refs?: string[] }[] }[]
    }
  }
  const first = body.recipe.assets.find((asset) => asset.alt === "First angle")!
  const second = body.recipe.assets.find((asset) => asset.alt === "Second angle")!
  expect(body.recipe.assets.map((asset) => asset.role)).toEqual(["image", "image"])
  expect(body.recipe.image_refs).toEqual([second.id, first.id])
  expect(body.recipe.primary_image).toBe(first.id)
  expect(body.recipe.instruction_sections[0].steps[0].image_refs).toEqual([second.id])
})

test("authored optionality and variations survive reading, editing, and saving", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Chowder with a variation")
  const loaded = await request.get(`/api/v1/recipes/${recipe.id}`)
  const body = await loaded.json()
  const document = body.recipe
  document.variants = [
    { title: "Richer", text: "Add 100 ml cream with the stock." },
    { text: "Follow the chocolate variation above but use orange zest." },
  ]
  document.ingredient_sections[0].ingredients[0].optional = true
  const saved = await request.put(`/api/v1/recipes/${recipe.id}`, {
    headers: { "If-Match": loaded.headers().etag },
    data: document,
  })
  expect(saved.ok(), await saved.text()).toBe(true)

  // The reader shows a variation as a variation, not as a general note.
  await page.goto(`/recipes/${recipe.id}`)
  const variations = page.getByRole("complementary", { name: "Variations" })
  await expect(variations).toContainText("Richer")
  await expect(variations).toContainText("Add 100 ml cream with the stock.")
  await expect(variations).toContainText("use orange zest")
  await expect(page.getByText("optional", { exact: true }).first()).toBeVisible()

  // Editing something unrelated must not quietly discard either of them.
  await page.goto(`/recipes/${recipe.id}/edit`)
  // Notes and variations is a section of the editor, not something found at
  // the bottom of a panel of bibliographic fields.
  await expect(page.getByRole("textbox", { name: "Variation 1", exact: true })).toHaveValue(
    "Add 100 ml cream with the stock.",
  )
  await expect(page.getByRole("textbox", { name: "Variation 1 name", exact: true })).toHaveValue(
    "Richer",
  )
  const title = page.getByRole("textbox", { name: "Title" })
  await title.fill("Chowder with a variation, edited")
  await page.getByRole("button", { name: "Save" }).click()
  await expect(
    page.getByRole("heading", { name: "Chowder with a variation, edited" }),
  ).toBeVisible()

  const reread = await request.get(`/api/v1/recipes/${recipe.id}`)
  const after = (await reread.json()).recipe
  expect(after.variants).toHaveLength(2)
  expect(after.variants[0].title).toBe("Richer")
  expect(after.ingredient_sections[0].ingredients[0].optional).toBe(true)
})

for (const kind of ["ingredients", "instructions", "stub"]) {
  test(`save and read a ${kind} recipe without fabricated content`, async ({ page, request }) => {
    const record = await seedRecipe(request, `Partial ${kind}`)
    await page.goto(`/recipes/${record.id}/edit`)
    if (kind !== "ingredients") {
      await page.getByRole("button", { name: "Remove group", exact: true }).click()
      await page.getByRole("button", { name: "Confirm removal", exact: true }).click()
    }
    if (kind !== "instructions") {
      await page.getByRole("button", { name: "Add a heading Instructions", exact: true }).click()
      await page.getByRole("button", { name: "Remove stage", exact: true }).click()
      await page.getByRole("button", { name: "Confirm removal", exact: true }).click()
    }
    await page.getByRole("button", { name: "Save changes" }).click()
    await expect(page).toHaveURL(new RegExp(`/recipes/${record.id}$`))
    await expect(page.getByRole("heading", { name: "Ingredients", exact: true })).toHaveCount(
      kind === "ingredients" ? 1 : 0,
    )
    await expect(page.getByRole("heading", { name: "Instructions", exact: true })).toHaveCount(
      kind === "instructions" ? 1 : 0,
    )
    const response = await request.get(`/api/v1/recipes/${record.id}`)
    const body = await response.json()
    expect(body.recipe.ingredient_sections?.length ?? 0).toBe(kind === "ingredients" ? 1 : 0)
    expect(body.recipe.instruction_sections?.length ?? 0).toBe(kind === "instructions" ? 1 : 0)
  })
}

test("a measurement the importer resolved on its own can be put back to its words", async ({
  page,
  request,
}) => {
  // A recipe imported under unit registry 0.1.0, where a bare `teaspoon`
  // became the metric spoon with no review warning.
  const recipe = await seedRecipe(request, "Legacy teaspoon", {
    ingredient_sections: [
      {
        id: "spices",
        title: "Spices",
        ingredients: [
          {
            id: "cinnamon",
            name: "ground cinnamon",
            note: "or a cassia stick",
            optional: true,
            source_text: "1 teaspoon ground cinnamon",
            quantity: {
              kind: "simple",
              amount: { value: "1" },
              unit: "volume.teaspoon.metric.culinary",
            },
            normalization: { method: "extracted" },
          },
        ],
      },
    ],
  })

  await page.goto(`/recipes/${recipe.id}/edit`)
  await page.getByRole("button", { name: "More options for ground cinnamon" }).click()
  await page.getByRole("menuitem", { name: "Details, ground cinnamon", exact: true }).click()

  const revert = page.getByRole("button", { name: "Revert this interpretation" })
  await expect(revert).toBeVisible()
  await revert.click()
  await page.getByRole("button", { name: "Done" }).click()
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))

  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(stored.ok()).toBe(true)

  const body = (await stored.json()) as {
    recipe: {
      ingredient_sections: {
        ingredients: {
          name: string
          note?: string
          optional?: boolean
          quantity?: unknown
          source_text?: string
          normalization?: { method: string; review?: string }
        }[]
      }[]
    }
  }
  const reverted = body.recipe.ingredient_sections[0].ingredients[0]

  // The interpretation is gone and the authored line is back for review.
  expect(reverted.quantity).toBeUndefined()
  expect(reverted.name).toBe("1 teaspoon ground cinnamon")
  expect(reverted.source_text).toBe("1 teaspoon ground cinnamon")
  expect(reverted.normalization?.method).toBe("unresolved")
  expect(reverted.normalization?.review).toBe("confirmed")

  // Metadata the interpretation had nothing to do with survives.
  expect(reverted.optional).toBe(true)
  expect(reverted.note).toBe("or a cassia stick")
})

test("steps are dragged into order, and their details name the step they belong to", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Reorderable method", {
    ingredient_sections: [
      {
        id: "ingredients",
        ingredients: [
          {
            id: "butter",
            name: "unsalted butter",
            note: "softened",
            source_text: "115 g (1 stick) unsalted butter, softened",
            quantity: { kind: "simple", amount: { value: "115" }, unit: "mass.gram" },
          },
        ],
      },
    ],
    instruction_sections: [
      {
        id: "method",
        steps: [
          { id: "s1", text: "Cream the butter.", ingredient_uses: [{ ingredient_ref: "butter" }] },
          { id: "s2", text: "Fold in the flour." },
          { id: "s3", text: "Bake until risen." },
        ],
      },
    ],
  })

  await page.goto(`/recipes/${recipe.id}/edit`)
  // The details of a step say which step they belong to and what it says, so
  // choosing the ingredients it uses is not done from memory.
  await page.getByRole("button", { name: "More options for step 3", exact: true }).click()
  await page
    .getByRole("menuitem", { name: "Ingredients, temperature, and photos, step 3", exact: true })
    .click()
  const details = page.getByRole("dialog")
  await expect(details.getByRole("heading", { name: "Step 3", exact: true })).toBeVisible()
  await expect(details.getByText("Bake until risen.", { exact: true })).toBeVisible()
  // The ingredient reads as the editor holds it, not as the source line it was
  // imported from.
  await expect(
    details.getByRole("checkbox", { name: /115 g unsalted butter, softened/ }),
  ).toBeVisible()
  await expect(details.getByText("115 g (1 stick) unsalted butter, softened")).toHaveCount(0)
  await page.keyboard.press("Escape")

  // Dragging a step by its handle puts it where it was dropped.
  const steps = page.getByRole("textbox", { name: /^Step \d+$/ })
  await expect(steps.nth(0)).toHaveValue("Cream the butter.")
  // The gesture itself belongs to the browser, and the headless shell these
  // tests run in has no native drag and drop, so the drag is delivered as the
  // events a real one sends. What is under test is the reorder that follows.
  await page.evaluate(() => {
    const handle = document.querySelector('[data-editor-id="s3"] [data-drag-handle]')
    const target = document.querySelector('[data-editor-id="s1"]')
    if (!handle || !target) throw new Error("the step handle or its target is missing")
    const dataTransfer = new DataTransfer()
    const drag = (node: Element, type: string) =>
      node.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer }))
    drag(handle, "dragstart")
    drag(target, "dragover")
    drag(target, "drop")
    drag(handle, "dragend")
  })
  await expect(steps.nth(0)).toHaveValue("Bake until risen.")
  await expect(steps.nth(1)).toHaveValue("Cream the butter.")
  await expect(steps.nth(2)).toHaveValue("Fold in the flour.")

  await page.getByRole("button", { name: "Save changes", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  const stored = await (await request.get(`/api/v1/recipes/${recipe.id}`)).json()
  expect(
    stored.recipe.instruction_sections[0].steps.map((step: { text: string }) => step.text),
  ).toEqual(["Bake until risen.", "Cream the butter.", "Fold in the flour."])

  // The reader does not repeat the ingredients a step uses under the step: the
  // list is already beside the method.
  await expect(page.getByText("Uses:")).toHaveCount(0)
})

test("an ingredient is dragged within its group and into another", async ({ page, request }) => {
  const recipe = await seedRecipe(request, "Draggable ingredients", {
    ingredient_sections: [
      {
        id: "dough",
        title: "Dough",
        ingredients: [
          { id: "flour", name: "flour" },
          { id: "water", name: "water" },
        ],
      },
      { id: "topping", title: "Topping", ingredients: [{ id: "seeds", name: "seeds" }] },
    ],
  })
  await page.goto(`/recipes/${recipe.id}/edit`)
  const names = page.getByRole("textbox", { name: "Ingredient", exact: true })
  await expect(names).toHaveCount(3)
  // The headless shell has no native drag and drop, so the gesture is delivered
  // as the events a real one sends. What is under test is the reorder.
  const drag = (from: string, onto: string) =>
    page.evaluate(
      ([from, onto]) => {
        const rowOf = (name: string) =>
          Array.from(document.querySelectorAll<HTMLInputElement>('input[aria-label="Ingredient"]'))
            .find((input) => input.value === name)
            ?.closest("[data-reorder-row]")
        const source = rowOf(from)
        const target = rowOf(onto)
        const handle = source?.querySelector("[data-drag-handle]")
        if (!source || !target || !handle) throw new Error("the row or its handle is missing")
        const dataTransfer = new DataTransfer()
        const send = (node: Element, type: string) =>
          node.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer }))
        send(handle, "dragstart")
        send(target, "dragover")
        send(target, "drop")
        send(handle, "dragend")
      },
      [from, onto],
    )

  await drag("flour", "water")
  await expect(names.nth(0)).toHaveValue("water")
  await expect(names.nth(1)).toHaveValue("flour")

  await drag("flour", "seeds")
  await expect(names.nth(0)).toHaveValue("water")
  await expect(names.nth(1)).toHaveValue("flour")
  await expect(names.nth(2)).toHaveValue("seeds")
  await expect(page.getByRole("group").filter({ has: page.getByText("Topping") })).toHaveCount(0)

  await page.getByRole("button", { name: "Save changes", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  const stored = await (await request.get(`/api/v1/recipes/${recipe.id}`)).json()
  expect(
    stored.recipe.ingredient_sections.map((section: { ingredients: { name: string }[] }) =>
      section.ingredients.map((ingredient) => ingredient.name),
    ),
  ).toEqual([["water"], ["flour", "seeds"]])
})
