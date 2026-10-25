import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "@playwright/test"

import { choose, chooser, section, settleAnimations } from "./controls"
import { seedBook, seedRecipe } from "./library"

test("recipe photos support the first viewport and galleries follow the method", async ({
  page,
  request,
}) => {
  await page.goto("/")
  const images = []
  for (const [id, color] of [
    ["cover", "#c86b45"],
    ["gallery", "#657a4a"],
  ]) {
    const blob = await page.evaluate(async (color) => {
      const canvas = document.createElement("canvas")
      canvas.width = 1000
      canvas.height = 750
      const context = canvas.getContext("2d")!
      context.fillStyle = color
      context.fillRect(0, 0, canvas.width, canvas.height)
      const image = await new Promise<Blob>((resolve) =>
        canvas.toBlob((blob) => resolve(blob!), "image/png"),
      )
      const bytes = await image.arrayBuffer()
      const response = await fetch("/api/v1/assets", {
        method: "POST",
        headers: { "Content-Type": "application/octet-stream", "X-Asset-Media-Type": "image/png" },
        body: bytes,
      })
      if (!response.ok) throw new Error(await response.text())
      return response.json()
    }, color)
    images.push({
      id,
      path: `assets/${id}.png`,
      role: "image",
      media_type: "image/png",
      sha256: blob.sha256,
      size: blob.size,
      alt: id,
    })
  }
  const recipe = await seedRecipe(request, "Contract illustrated supper", {
    assets: images,
    image_refs: ["cover", "gallery"],
    primary_image: "cover",
  })
  await page.goto(`/recipes/${recipe.id}`)
  await expect(page.getByRole("heading", { name: "Ingredients", exact: true })).toBeInViewport()
  await expect(page.getByRole("heading", { name: "Instructions", exact: true })).toBeInViewport()
  await expect(page.getByRole("img", { name: "cover", exact: true })).toHaveJSProperty(
    "naturalWidth",
    1000,
  )
  const gallery = await page.getByRole("region", { name: "Recipe photos" }).boundingBox()
  const body = await page.locator(".recipe-page__body").boundingBox()
  expect(gallery!.y).toBeGreaterThanOrEqual(body!.y + body!.height)
  await page.goto("/?q=Contract%20illustrated")
  const cardImage = page.getByRole("link", { name: new RegExp(recipe.title) }).locator("img")
  await expect(cardImage).toHaveAttribute("alt", "")
  await expect(cardImage).toHaveAttribute("src", /\/derivatives\/card$/)
  await expect(cardImage).toHaveJSProperty("naturalWidth", 640)
})

test("recipe-first library, reader, and editor preserve the complete core workflow", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Contract supper")
  await page.goto("/")
  await expect(page.getByRole("combobox", { name: "Search recipes" })).toBeVisible()
  await expect(page.getByRole("button", { name: /Filters/ })).toBeVisible()
  await page.getByRole("combobox", { name: "Search recipes" }).fill(recipe.title)
  const card = page.getByRole("link", { name: new RegExp(recipe.title) })
  await expect(card.getByRole("heading", { level: 2 })).toBeVisible()
  await card.click()
  await expect(page.getByRole("heading", { name: "Ingredients", exact: true })).toBeInViewport()
  await expect(page.getByRole("heading", { name: "Instructions", exact: true })).toBeInViewport()
  await expect(page.getByText("whole-milk yoghurt", { exact: true })).toBeVisible()
  await expect(
    page.getByText("Whisk the yoghurt with a generous pinch of salt.", { exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole("button", { name: /Sign in|Select library|Manage users/ }),
  ).toHaveCount(0)

  await page.getByRole("link", { name: "Edit", exact: true }).click()
  const bibliography = section(page, /^Source and attribution/)
  await expect(bibliography).toHaveAttribute("aria-expanded", "false")
  await expect(page.getByRole("textbox", { name: "Ingredient", exact: true }).first()).toBeVisible()
  await page.getByRole("button", { name: "Ingredient", exact: true }).click()
  await expect(page.getByRole("textbox", { name: "Ingredient", exact: true }).last()).toBeFocused()
  await page
    .getByRole("textbox", { name: "Ingredient", exact: true })
    .last()
    .pressSequentially("sea salt")
  await page.getByRole("button", { name: "More options for sea salt", exact: true }).click()
  await page.getByRole("menuitem", { name: "Move up, sea salt", exact: true }).click()
  await page
    .getByRole("button", { name: "More options for boneless chicken thighs", exact: true })
    .click()
  await page.getByRole("menuitem", { name: "Remove, boneless chicken thighs", exact: true }).click()
  await page.getByRole("button", { name: "Step", exact: true }).click()
  await page.getByRole("textbox", { name: "Step 3", exact: true }).pressSequentially("Serve warm.")
  await page.getByRole("button", { name: "Move step 3 up", exact: true }).click()
  await page.getByRole("button", { name: "More options for step 3", exact: true }).click()
  await page.getByRole("menuitem", { name: "Remove, step 3", exact: true }).click()
  await page.getByRole("button", { name: "Save changes", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  const stored = await (await request.get(`/api/v1/recipes/${recipe.id}`)).json()
  expect(
    stored.recipe.ingredient_sections[0].ingredients.map((row: { name: string }) => row.name),
  ).toEqual(["whole-milk yoghurt", "sea salt"])
  expect(
    stored.recipe.instruction_sections[0].steps.map((row: { text: string }) => row.text),
  ).toEqual(["Whisk the yoghurt with a generous pinch of salt.", "Serve warm."])
})

test("tags and books filter the library and a trashed recipe can be restored", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Contract tagged supper")
  const unrelated = await seedRecipe(request, "Contract unrelated supper")
  const book = await seedBook(request, "Contract weeknights", [])
  const tagResponse = await request.post("/api/v1/tags", { data: { name: "Contract quick" } })
  expect(tagResponse.ok()).toBe(true)
  const tag = await tagResponse.json()
  await page.goto(`/recipes/${recipe.id}`)
  await page.getByRole("button", { name: "Organize", exact: true }).click()
  const dialog = page.getByRole("dialog", { name: "Organize recipe" })
  await dialog.getByRole("button", { name: "Add a tag", exact: true }).click()
  await dialog.getByRole("button", { name: "Add the tag Contract quick", exact: true }).click()
  await expect(
    dialog.getByRole("button", { name: "Remove the tag Contract quick", exact: true }),
  ).toBeVisible()
  await dialog.getByRole("button", { name: "Add to a book", exact: true }).click()
  const bookSearch = dialog.getByRole("searchbox", { name: "Search books", exact: true })
  await bookSearch.pressSequentially(book.title)
  await expect(bookSearch).toBeFocused()
  await expect(bookSearch).toHaveValue(book.title)
  await dialog
    .getByRole("button", { name: `Put this recipe in ${book.title}`, exact: true })
    .click()
  await expect(
    dialog.getByRole("button", { name: `Take this recipe out of ${book.title}`, exact: true }),
  ).toBeVisible()
  await dialog.getByRole("button", { name: "Save changes" }).click()
  await expect(page.getByRole("dialog")).not.toBeVisible()
  await page.reload()
  const filing = page.getByRole("link", { name: "Contract quick", exact: true })
  await expect(filing).toHaveAttribute("href", new RegExp(encodeURIComponent(tag.id)))
  await filing.click()
  await page.getByRole("button", { name: /Filters/ }).click()
  await page.getByRole("button", { name: "Choose a tag", exact: true }).click()
  await expect(page.getByRole("checkbox", { name: "Contract quick", exact: true })).toBeChecked()
  await page.keyboard.press("Escape")
  await page.goto("/?q=Contract")
  await page.getByRole("button", { name: /Filters/ }).click()
  await page.getByRole("button", { name: "Choose a tag", exact: true }).click()
  await page.getByRole("checkbox", { name: "Contract quick", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(encodeURIComponent(tag.id)))
  await page.keyboard.press("Escape")
  await expect(page.getByRole("link", { name: new RegExp(recipe.title) })).toBeVisible()
  await expect(page.getByRole("link", { name: new RegExp(unrelated.title) })).toHaveCount(0)
  await page.getByRole("link", { name: new RegExp(recipe.title) }).click()
  await page.getByRole("button", { name: "More actions", exact: true }).click()
  await page.getByRole("menuitem", { name: "Move to trash", exact: true }).click()
  await page.getByRole("dialog").getByRole("button", { name: "Move to trash", exact: true }).click()
  await expect(page).toHaveURL("/")
  await page.goto("/trash")
  const row = page.getByRole("listitem").filter({ hasText: recipe.title })
  await row.getByRole("button", { name: "Restore", exact: true }).click()
  await expect(row).toHaveCount(0)
  await page.goto(`/books/${book.id}`)
  await expect(page.getByRole("link", { name: new RegExp(recipe.title) })).toBeVisible()
})

test("a changed batch says which amounts it could not recalculate", async ({ page, request }) => {
  // A source that wrote its own equivalents into the line: the butter has a
  // quantity and an equivalent in its note, and the sugar has neither a
  // quantity nor anything a reader could multiply.
  const recipe = await seedRecipe(request, "Contract partly scalable supper", {
    ingredient_sections: [
      {
        id: "main",
        ingredients: [
          {
            id: "butter",
            name: "unsalted butter",
            note: "softened, 227 grams",
            quantity: { kind: "simple", amount: { value: "1" }, unit: "volume.cup.us.customary" },
          },
          { id: "sugar", name: "0.33 cup/66 grams granulated sugar" },
          { id: "seasoning", name: "salt and pepper" },
        ],
      },
    ],
  })
  await page.goto(`/recipes/${recipe.id}`)
  const ingredients = page.locator(".recipe-page__ingredients")
  await expect(ingredients).toContainText("1 cup")
  await expect(ingredients).not.toContainText("as written")

  await page.getByRole("textbox", { name: "How many servings" }).fill("8")
  await page.getByRole("textbox", { name: "How many servings" }).press("Tab")
  await expect(ingredients).toContainText("2 cups")
  await expect(
    ingredients.getByRole("button", {
      name: "This amount could not be recalculated for the batch, so it is the recipe's own.",
    }),
  ).toBeVisible()
  await expect(
    ingredients.getByRole("button", {
      name: "This ingredient's wording still states the recipe's own numbers.",
    }),
  ).toBeVisible()
  const check = page.getByRole("button", { name: "Measurements to check (2)", exact: true })
  await expect(check).toBeVisible()
  await check.click()
  await expect(page.getByRole("dialog")).toContainText("This amount could not be recalculated")
  await page.keyboard.press("Escape")
  // The row a reader hears is the one on screen, not the one the source wrote.
  const butter = ingredients.getByRole("listitem").filter({ hasText: "unsalted butter" })
  expect(await butter.getAttribute("title")).toBeNull()
  // A line stating no amount has no amount to keep, and is left alone.
  const seasoning = ingredients.getByRole("listitem").filter({ hasText: "salt and pepper" })
  await expect(seasoning).not.toContainText("as written")
})

test("scaling, measurement choices, and cooking timers perform their visible actions", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Contract timed supper", {
    times: {
      assertions: [
        {
          id: "simmer",
          kind: "cook",
          label: "Simmer",
          duration: { value: "PT5M" },
          attention: "passive",
        },
      ],
    },
    instruction_sections: [
      {
        id: "method",
        steps: [{ id: "simmer-step", text: "Simmer gently.", timing_refs: ["simmer"] }],
      },
    ],
  })
  await page.goto(`/recipes/${recipe.id}`)
  const ingredients = page.locator(".recipe-page__ingredients")
  const before = await ingredients.innerText()
  await page.getByRole("textbox", { name: "How many servings" }).fill("8")
  await page.getByRole("textbox", { name: "How many servings" }).press("Tab")
  await expect(ingredients).not.toHaveText(before)
  const scaled = await ingredients.innerText()
  await choose(chooser(page, "Measurements"), "metric")
  await expect(ingredients).not.toHaveText(scaled)
  // The control names the phase it belongs to, so the step's writing does not
  // have to repeat what the button already says.
  await page.getByRole("button", { name: "Simmer · Start 5 min timer", exact: true }).click()
  await expect(page.getByRole("button", { name: /^Timers:/ })).toContainText(/4:|5:/)
  await page.goto("/")
  await page.getByRole("button", { name: /^Timers:/ }).click()
  await page.getByRole("link", { name: `Step 1 · ${recipe.title}`, exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}#`))
  await page.getByRole("button", { name: /^Timers:/ }).click()
  await page.getByRole("button", { name: "Dismiss the Simmer timer", exact: true }).click()
  await expect(page.getByRole("button", { name: /^Timers:/ })).toHaveCount(0)
  const stored = await request.get(`/api/v1/recipes/${recipe.id}`)
  expect(stored.headers().etag).toBe('"1"')
  expect(
    (await stored.json()).recipe.ingredient_sections[0].ingredients[0].quantity.amount.value,
  ).toBe("1/2")
})

test("shared exact alternative quantities are presented once in the amount column", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Contract citrus choice", {
    ingredient_sections: [
      {
        id: "ingredients",
        ingredients: [
          {
            id: "citrus",
            relation: "or",
            alternatives: [
              {
                id: "lime",
                name: "lime, juiced",
                quantity: { kind: "simple", amount: { value: "1" }, unit: "count.item" },
              },
              {
                id: "lemon",
                name: "lemon, juiced",
                quantity: { kind: "simple", amount: { value: "1" }, unit: "count.item" },
              },
            ],
          },
        ],
      },
    ],
  })

  await page.goto(`/recipes/${recipe.id}`)
  const citrus = page.locator("#ingredient-citrus")
  await expect(citrus).toContainText("1 lime, juiced or lemon, juiced")
  await expect(citrus.locator(".recipe-page__amount")).toHaveText("1")
})

test("preferred ingredients use a quieter alternative in reader and cooking views", async ({
  page,
  request,
}, testInfo) => {
  const recipe = await seedRecipe(request, "Preferred ingredient presentation", {
    ingredient_sections: [
      {
        id: "ingredients",
        ingredients: [
          {
            id: "fat",
            relation: "preferred",
            alternatives: [
              {
                id: "butter",
                name: "unsalted butter",
                quantity: {
                  kind: "simple",
                  amount: { value: "1" },
                  unit: "volume.cup.us.customary",
                },
              },
              {
                id: "oil",
                name: "mild olive oil",
                quantity: {
                  kind: "simple",
                  amount: { value: "3/4" },
                  unit: "volume.cup.us.customary",
                },
              },
            ],
          },
          {
            id: "topping",
            relation: "preferred",
            alternatives: [
              {
                id: "yoghurt",
                name: "plain yoghurt",
                quantity: { kind: "simple", amount: { value: "100" }, unit: "mass.gram" },
              },
              {
                id: "coconut",
                name: "coconut yoghurt",
                quantity: { kind: "simple", amount: { value: "100" }, unit: "mass.gram" },
              },
            ],
          },
        ],
      },
    ],
  })
  await page.goto(`/recipes/${recipe.id}`)
  const row = page.locator("#ingredient-fat")
  await expect(row.getByText("or", { exact: true })).toBeVisible()
  await expect(row.getByText("Prefer", { exact: true })).toHaveCSS("width", "1px")
  await expect(row).not.toContainText("alternatively")
  const amounts = row.locator(".recipe-page__amount")
  const primary = await amounts.first().evaluate((element) => getComputedStyle(element).color)
  const alternative = await amounts.last().evaluate((element) => getComputedStyle(element).color)
  expect(primary).not.toBe(alternative)
  const shared = page.locator("#ingredient-topping")
  await expect(shared.locator(".recipe-page__amount")).toHaveCount(1)
  expect(
    await shared
      .getByText("coconut yoghurt", { exact: true })
      .evaluate((element) => getComputedStyle(element).color),
  ).toBe(alternative)
  for (const colorScheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme })
    await settleAnimations(page)
    expect(
      (await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa"]).analyze())
        .violations,
    ).toEqual([])
    await page.screenshot({ path: testInfo.outputPath(`preferred-reader-${colorScheme}.png`) })
  }
  await page.getByRole("button", { name: "Cook", exact: true }).click()
  await expect(page.getByText("mild olive oil", { exact: false })).toBeVisible()
  await expect(page.getByText("alternatively", { exact: true })).toHaveCount(0)
})

test("the search box offers the library's own words and narrows by the one chosen", async ({
  page,
  request,
}) => {
  // Typing a category name offers that category as a filter.
  const suffix = Math.random().toString(36).slice(2, 8)
  const value = `Snapped dessert ${suffix}`
  const created = await request.post("/api/v1/facets", { data: { kind: "course", value } })
  expect(created.ok()).toBeTruthy()
  const facet = (await created.json()) as { id: string }
  const matching = await seedRecipe(request, `Snapped supper ${suffix}`)
  const other = await seedRecipe(request, `Unsnapped supper ${suffix}`)
  const organization = (await (
    await request.get(`/api/v1/recipes/${matching.id}/organization`)
  ).json()) as { revision: number }
  expect(
    (
      await request.put(`/api/v1/recipes/${matching.id}/organization`, {
        headers: { "If-Match": `"${organization.revision}"` },
        data: { tag_ids: [], facet_ids: [facet.id], user_state: { favorite: false }, sources: [] },
      })
    ).ok(),
  ).toBeTruthy()

  await page.goto("/")
  const search = page.getByRole("combobox", { name: "Search recipes" })
  await search.fill(value)
  const suggestions = page.getByRole("listbox", { name: "Narrow the library" })
  await expect(suggestions.getByRole("option", { name: new RegExp(value) })).toBeVisible()

  // Escape puts the list away and leaves the words alone to be searched for,
  // because the suggestion is an offer rather than an interception.
  await search.press("Escape")
  await expect(suggestions).not.toBeVisible()
  await expect(search).toHaveValue(value)

  // The keyboard reaches the list, and the choice becomes a filter rather than
  // words to search the prose for.
  await search.press("ArrowDown")
  await expect(suggestions.getByRole("option", { selected: true })).toBeVisible()
  await search.press("Enter")
  await expect(suggestions).not.toBeVisible()
  await expect(search).toHaveValue("")
  await expect(page.getByRole("button", { name: new RegExp(value) })).toBeVisible()
  await expect(page.getByRole("link", { name: new RegExp(matching.title) })).toBeVisible()
  await expect(page.getByRole("link", { name: new RegExp(other.title) })).toHaveCount(0)
})

test("a book opens as something to read, with arranging behind a deliberate step", async ({
  page,
  request,
}) => {
  const first = await seedRecipe(request, "Read book first supper")
  const second = await seedRecipe(request, "Read book second supper")
  const book = await seedBook(request, "Readable weeknight book", [first.id, second.id])

  await page.goto(`/books/${book.id}`)
  await expect(page.getByRole("heading", { name: book.title, exact: true })).toBeVisible()
  await expect(page.getByRole("link", { name: first.title })).toBeVisible()
  await expect(page.getByRole("link", { name: second.title })).toBeVisible()

  // Reading is reading: nothing here rearranges the book by accident.
  await expect(page.getByRole("combobox", { name: `Chapter for ${first.title}` })).toHaveCount(0)
  await expect(page.getByRole("button", { name: `Move ${first.title} down` })).toHaveCount(0)
  await expect(page.getByRole("link", { name: "Print book" })).toBeVisible()
  await expect(page.getByText("2 recipes", { exact: true })).toBeVisible()

  await page.getByRole("button", { name: "Organize book", exact: true }).click()
  await expect(page.getByRole("combobox", { name: `Chapter for ${first.title}` })).toHaveCount(0)
  await page.getByRole("button", { name: `Move ${first.title} down` }).click()
  await page.getByRole("button", { name: "Save and close" }).click()

  // The new order is what the reader is returned to, and it survives a reload.
  await expect(page.getByRole("button", { name: `Move ${first.title} down` })).toHaveCount(0)
  // Hold the reload's data to exercise the loading state.
  let releaseBook!: () => void
  const heldBook = new Promise<void>((resolve) => {
    releaseBook = resolve
  })
  await page.route(`**/api/v1/books/${book.id}`, async (route) => {
    await heldBook
    await route.continue()
  })
  const requested = page.waitForRequest(
    (request) => new URL(request.url()).pathname === `/api/v1/books/${book.id}`,
  )
  await page.reload()
  await requested
  const titles = page.getByRole("listitem").getByRole("link")
  await expect(titles).toHaveCount(0)
  releaseBook()
  await expect(titles).toHaveText([second.title, first.title])
})

test("ingredient units change this reading, follow the batch, and leave saved quantities intact", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Reader unit choices")
  await page.goto(`/recipes/${recipe.id}`)
  await choose(chooser(page, "Measurements"), "original")
  const row = page.locator("#ingredient-yoghurt")
  const amount = row.getByRole("button", { name: /^whole-milk yoghurt: .* · Unit$/ })
  await expect(amount).toContainText("½ cup")
  await amount.click()
  await expect(page.getByRole("menuitemradio", { name: "g", exact: true })).toHaveCount(0)
  await page.getByRole("menuitemradio", { name: /^ml$/i }).click()
  await expect(amount).toHaveText(/≈ 118\.29 ml/i)
  await expect(amount).toBeFocused()
  const servings = page.getByRole("textbox", { name: "How many servings" })
  await servings.fill("8")
  await servings.press("Tab")
  await expect(amount).toHaveText(/≈ 236\.59 ml/i)
  await amount.click()
  await page.getByRole("menuitemradio", { name: "As written", exact: true }).click()
  await expect(amount).toContainText("1 cup")
  await amount.click()
  await page.getByRole("menuitemradio", { name: /^ml$/i }).click()
  await choose(chooser(page, "Measurements"), "metric")
  await amount.click()
  await expect(page.getByRole("menuitemradio", { name: "Metric", exact: true })).toHaveAttribute(
    "aria-checked",
    "true",
  )
  await page.keyboard.press("Escape")
  const saved = await (await request.get(`/api/v1/recipes/${recipe.id}`)).json()
  expect(saved.recipe.ingredient_sections[0].ingredients[0].quantity).toEqual({
    kind: "simple",
    amount: { value: "1/2" },
    unit: "volume.cup.us.customary",
  })
})

test("each publication source recipe links within its own row", async ({ page, request }) => {
  const first = await seedRecipe(request, "First sourced supper")
  const second = await seedRecipe(request, "Second sourced supper")
  const response = await request.post("/api/v1/books", {
    data: { kind: "publication", title: "Source cookbook", entries: [], sections: [] },
  })
  expect(response.ok()).toBe(true)
  const book = await response.json()
  for (const entry of [first, second]) {
    const organization = await (
      await request.get(`/api/v1/recipes/${entry.id}/organization`)
    ).json()
    const result = await request.put(`/api/v1/recipes/${entry.id}/organization`, {
      headers: { "If-Match": `"${organization.revision}"` },
      data: {
        tag_ids: [],
        facet_ids: [],
        user_state: { favorite: false },
        sources: [{ kind: "publication", publication_book_id: book.id }],
      },
    })
    expect(result.ok()).toBe(true)
  }
  await page.goto(`/books/${book.id}`)
  await page.getByRole("link", { name: first.title, exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${first.id}$`))
  await page.goto(`/books/${book.id}`)
  await page.getByRole("link", { name: second.title, exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${second.id}$`))
})
