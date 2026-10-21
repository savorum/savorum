import AxeBuilder from "@axe-core/playwright"
import { type Locator, type Page, expect, test } from "@playwright/test"

import { choose, openSection } from "./controls"
import { seedBook, seedRecipe } from "./library"

async function expectAccessible(page: Page) {
  await page.evaluate(async () => {
    const animations = document
      .getAnimations()
      .filter((animation) => animation.effect?.getTiming().iterations !== Infinity)
    await Promise.all(animations.map((animation) => animation.finished.catch(() => {})))
  })
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze()
  expect(results.violations).toEqual([])
}

async function expectStableControlledEditing(input: Locator, typed: string, composed: string) {
  await input.fill("")
  await input.focus()
  await input.pressSequentially(`${typed}${composed}`)
  await expectStableControlledValue(input, `${typed}${composed}`)
}

async function expectStableControlledValue(input: Locator, value: string) {
  await expect(input).toBeFocused()
  await expect(input).toHaveValue(value)
}

test("representative library, reader, editor, book, and trash views pass axe", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Accessible aubergine supper")
  const book = await seedBook(request, "Accessible suppers", [recipe.id])

  await page.goto("/")
  await expectAccessible(page)

  await page.goto(`/recipes/${recipe.id}`)
  await expectAccessible(page)

  await page.goto(`/recipes/${recipe.id}/edit`)
  await expectAccessible(page)

  await page.goto(`/books/${book.id}`)
  await expectAccessible(page)

  const loaded = await request.get(`/api/v1/recipes/${recipe.id}`)
  const trashed = await request.delete(`/api/v1/recipes/${recipe.id}`, {
    headers: { "If-Match": loaded.headers().etag },
  })
  expect(trashed.ok()).toBe(true)
  await page.goto("/trash")
  await expectAccessible(page)
})

/**
 * Duplicate ids are invalid markup, and a label[for] or aria-labelledby then
 * resolves against whichever element the browser reaches first.
 *
 * Asserted directly because axe's `duplicate-id` rule no longer runs by default
 * and `duplicate-id-aria` ignores ids referenced by a plain label.
 */
async function expectNoDuplicateIDs(page: Page, surface: string) {
  const duplicates = await page.evaluate(() => {
    const counts = new Map<string, number>()
    for (const node of Array.from(document.querySelectorAll("[id]"))) {
      counts.set(node.id, (counts.get(node.id) ?? 0) + 1)
    }
    return [...counts.entries()]
      .filter(([, total]) => total > 1)
      .map(([id, total]) => {
        const shared = Array.from(document.querySelectorAll(`[id="${CSS.escape(id)}"]`))
        return `${id} on ${total}: ${shared.map((e) => e.getAttribute("aria-label") ?? e.tagName).join(", ")}`
      })
  })
  expect(duplicates, `${surface} shares element ids`).toEqual([])
}

test("no surface gives two elements the same id", async ({ page, request }) => {
  // Lists of repeated controls are where this goes wrong, so every list on the
  // editor is given more than one entry.
  const recipe = await seedRecipe(request, "Shared identifier sweep", {
    notes: ["first note", "second note"],
    keywords: ["one", "two"],
    categories: ["Cake", "Celebration cake"],
    authors: [{ name: "One", url: "https://example.test/one" }, { name: "Two" }],
  })
  const book = await seedBook(request, "Sweep book", [recipe.id])
  for (const [surface, url] of [
    ["library", "/"],
    ["reader", `/recipes/${recipe.id}`],
    ["editor", `/recipes/${recipe.id}/edit`],
    ["book", `/books/${book.id}`],
    ["books", "/books"],
    ["trash", "/trash"],
    ["account", "/account"],
    ["preferences", "/preferences"],
  ] as [string, string][]) {
    await page.goto(url)
    await page.waitForSelector("#main-content")
    await expectNoDuplicateIDs(page, surface)
  }

  // Source and attribution holds most of the repeated controls and is
  // collapsed until asked for.
  await page.goto(`/recipes/${recipe.id}/edit`)
  await expect(page.getByRole("textbox", { name: "Notes 2" })).toBeVisible()
  await openSection(page, /^Source and attribution/)
  await openSection(page, /^Categories and keywords/)
  await expectNoDuplicateIDs(page, "editor with its source and label panels open")
})

test("the import progress page passes axe", async ({ page }) => {
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/accessible-import.json")
  const review = page.getByRole("region", { name: "Save this recipe" })
  await expect(review).toBeVisible()
  await expectAccessible(page)

  // The measurement questions and everything they hide behind a disclosure.
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/measurement-review.yml")
  const decisions = page.getByRole("region", { name: "Measurements to check" })
  await expect(decisions).toBeVisible()
  await decisions.getByText("Answer the 2 measures separately").click()
  await decisions.getByRole("radio", { name: "Australian", exact: true }).check()
  await expect(decisions.getByRole("button", { name: "Apply" })).toBeVisible()
  await decisions.getByText("Exceptions").first().click()
  const exceptionsDialog = page.getByRole("dialog", { name: "Exceptions" })
  await expect(exceptionsDialog).toBeVisible()
  await expectAccessible(page)
  await exceptionsDialog.getByRole("button", { name: "Done", exact: true }).click()

  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/accessible-import.json")
  await expect(review).toBeVisible()

  await review.getByRole("button", { name: "Save recipe", exact: true }).click()
  await expect(page).toHaveURL(/\/imports\//)
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()
  await expectAccessible(page)
})

test("skip navigation and client routing place focus at meaningful content", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Keyboard route supper")
  await page.goto("/")

  await page.keyboard.press("Tab")
  const skip = page.getByRole("link", { name: "Skip to main content" })
  await expect(skip).toBeFocused()
  await page.keyboard.press("Enter")
  await expect(page.locator("#main-content")).toBeFocused()

  await page.getByRole("link", { name: new RegExp(recipe.title) }).click()
  await expect(page.getByRole("heading", { name: recipe.title })).toBeFocused()
})

test("controlled list inputs retain focus and composition while editing", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Stable accessible editor", {
    yields: [
      {
        quantity: { kind: "simple", amount: { value: "4" }, unit: "count.serving" },
        label: "servings",
        text: "Serves four",
      },
    ],
    ingredient_sections: [
      {
        id: "base",
        ingredients: [
          { id: "sugar", name: "sugar" },
          { id: "flour", name: "flour" },
        ],
      },
    ],
    instruction_sections: [
      {
        id: "method",
        steps: [
          { id: "mix", text: "Mix." },
          { id: "bake", text: "Bake." },
        ],
      },
    ],
  })
  await page.goto(`/recipes/${recipe.id}/edit`)

  await page.getByRole("button", { name: "Add a heading Ingredients", exact: true }).click()
  await expectStableControlledEditing(
    page.getByRole("textbox", { name: "Group name", exact: true }),
    "Base ",
    "茶",
  )
  await page.getByRole("button", { name: "Add a heading Instructions", exact: true }).click()
  await expectStableControlledEditing(
    page.getByRole("textbox", { name: "Stage name", exact: true }),
    "Method ",
    "茶",
  )

  await expectStableControlledEditing(
    page.getByRole("textbox", { name: "Summary", exact: true }),
    "Warm tea",
    " 茶",
  )
  // Yield and timing is collapsed by default.
  await openSection(page, /^Yield and timing/)
  await expectStableControlledEditing(
    page.getByRole("textbox", { name: "Of what", exact: true }),
    "Cups",
    " 茶",
  )
  await page.getByRole("button", { name: "Yield details", exact: true }).click()
  await expectStableControlledEditing(
    page.getByRole("textbox", { name: "Yield as written", exact: true }),
    "One pot",
    " 茶",
  )
  // The exact amount opens in a dialog, which holds the rest of the page
  // inert until it is shut.
  await page.getByRole("button", { name: "Done", exact: true }).click()

  const sugar = page.getByRole("textbox", { name: "Ingredient" }).first()
  await sugar.fill("")
  await sugar.pressSequentially("brown sugar")
  await expect(sugar).toBeFocused()
  await expect(sugar).toHaveValue("brown sugar")

  await sugar.evaluate((element) => {
    const input = element as HTMLInputElement
    input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }))
    input.value = "brown sugar 茶"
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
  await expect(sugar).toBeFocused()
  await expect(sugar).toHaveValue("brown sugar 茶")

  const firstStep = page.getByRole("textbox", { name: "Step 1" })
  await firstStep.fill("")
  await firstStep.pressSequentially("Whisk gently")
  await firstStep.evaluate((element) => {
    const input = element as HTMLTextAreaElement
    input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }))
    input.value = "Whisk gently 茶"
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
  await expect(firstStep).toBeFocused()
  await expect(firstStep).toHaveValue("Whisk gently 茶")

  await page.getByRole("button", { name: "More options for brown sugar 茶", exact: true }).click()
  await page.getByRole("menuitem", { name: "Move down, brown sugar 茶", exact: true }).click()
  await expect(page.getByRole("textbox", { name: "Ingredient", exact: true }).nth(1)).toBeFocused()
  await expect(page.getByRole("textbox", { name: "Ingredient" }).nth(1)).toHaveValue(
    "brown sugar 茶",
  )

  await page.getByRole("button", { name: "More options for brown sugar 茶", exact: true }).click()
  await page.getByRole("menuitem", { name: "Remove, brown sugar 茶", exact: true }).click()
  await expect(page.getByRole("textbox", { name: "Ingredient", exact: true })).toBeFocused()
  await expect(page.getByRole("textbox", { name: "Ingredient" })).toHaveValue("flour")

  await page.getByRole("button", { name: "Move step 1 down", exact: true }).click()
  await expect(page.getByRole("textbox", { name: "Step 2" })).toBeFocused()
  await expect(page.getByRole("textbox", { name: "Step 2" })).toHaveValue("Whisk gently 茶")
})

test("storage guidance stays compact and expands from the keyboard on narrow books", async ({
  page,
  request,
}) => {
  const book = await seedBook(request, "Storage guidance book", [])
  await page.setViewportSize({ width: 320, height: 844 })
  for (const state of ["warning", "limited"] as const) {
    await page.route("**/api/v1/storage-status", (route) =>
      route.fulfill({ json: { state, hosted: false } }),
    )
    await page.goto(`/books/${book.id}`)
    const summary = page.getByRole("button", {
      name:
        state === "warning" ? "Storage is nearing its limit" : "Storage has reached a safety limit",
    })
    await expect(summary).toBeVisible()
    await expect(summary).toHaveCSS("padding-top", "0px")
    await expect(summary.locator("..")).toHaveCSS("border-top-width", "0px")
    await expect(summary.locator("svg")).toHaveAttribute("fill", "none")
    const heading = await page.getByRole("heading", { name: book.title, exact: true }).boundingBox()
    expect(heading?.y).toBeLessThan(250)
    await summary.focus()
    await summary.press("Enter")
    await expect(
      page.getByText("Contact your server administrator for help with storage."),
    ).toBeVisible()
    await summary.press("Enter")
    await expect(
      page.getByText("Contact your server administrator for help with storage."),
    ).toBeHidden()
    await expect(summary).toBeFocused()
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320)
    await page.unroute("**/api/v1/storage-status")
  }
})

test("book editing retains controlled fields and focus across reorder and removal", async ({
  page,
  request,
}) => {
  const first = await seedRecipe(request, "First accessible book recipe")
  const second = await seedRecipe(request, "Second accessible book recipe")
  const book = await seedBook(request, "Keyboard book", [first.id, second.id])
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto(`/books/${book.id}`)
  await expect(page.getByRole("heading", { name: "Keyboard book", exact: true })).toBeInViewport()
  const heading = await page
    .getByRole("heading", { name: "Keyboard book", exact: true })
    .boundingBox()
  expect(heading?.y).toBeLessThan(250)

  // A book opens as something to read, so arranging it is entered deliberately.
  await page.getByRole("button", { name: "Organize book", exact: true }).click()
  await page.getByRole("button", { name: "Book details", exact: true }).click()
  for (const label of ["Title", "Subtitle", "About this book"]) {
    await expectStableControlledEditing(
      page.getByRole("textbox", { name: label, exact: true }),
      "Weekend",
      " 茶",
    )
  }
  await page.getByRole("button", { name: "Done editing", exact: true }).click()
  await page.getByRole("button", { name: "Chapter", exact: true }).click()
  const chapter = page.getByRole("textbox", { name: "Chapter name" })
  await expectStableControlledEditing(chapter, "Weekend", " 茶")
  await expect(page.getByRole("combobox", { name: `Chapter for ${first.title}` })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320)

  await page.getByRole("button", { name: `Move ${first.title} down` }).click()
  await expect(page.getByRole("link", { name: first.title })).toBeFocused()

  await page.getByRole("button", { name: `Remove ${first.title} from book` }).click()
  await expect(page.getByRole("link", { name: second.title })).toBeFocused()
})

test("resource-backed recipe searches retain focus, caret, composition, and DOM identity", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 320, height: 844 })
  const libraryRecipe = await seedRecipe(request, "Persistent tea recipe")
  const bookRecipe = await seedRecipe(request, "Searchable accessible book recipe")
  const book = await seedBook(request, "Searchable keyboard book", [])

  await page.goto("/")
  await expect(page.getByRole("link", { name: new RegExp(libraryRecipe.title) })).toBeVisible()
  const librarySearch = page.getByRole("combobox", { name: "Search recipes" })
  await expectStableControlledEditing(librarySearch, "Persistent", " tea")
  await expect.poll(() => new URL(page.url()).searchParams.get("q")).toBe("Persistent tea")
  await expect(page.getByRole("link", { name: new RegExp(libraryRecipe.title) })).toBeVisible()
  await expectStableControlledValue(librarySearch, "Persistent tea")

  await page.goto(`/books/${book.id}`)
  await page.getByRole("button", { name: "Add recipes", exact: true }).click()

  const bookSearch = page.getByRole("searchbox", { name: "Search recipes to add" })
  const candidate = page.getByRole("checkbox", { name: new RegExp(bookRecipe.title) })
  await expect(candidate).toHaveCount(1)
  const refreshed = page.waitForResponse((response) => {
    const url = new URL(response.url())
    return (
      url.pathname === "/api/v1/recipes/suggestions" &&
      url.searchParams.get("q") === "Searchable accessible"
    )
  })
  await expectStableControlledEditing(bookSearch, "Searchable", " accessible")
  await refreshed
  await expect(candidate).toHaveCount(1)
  await expectStableControlledValue(bookSearch, "Searchable accessible")
})

test("the add menu offers writing and import from the keyboard", async ({ page }) => {
  await page.goto("/")
  const trigger = page.getByRole("button", { name: "Add recipes" })
  await trigger.focus()
  await page.keyboard.press("Enter")
  await expect(page.getByRole("menuitem", { name: "New recipe", exact: true })).toHaveAttribute(
    "data-highlighted",
    "",
  )
  await page.keyboard.press("ArrowDown")
  const importItem = page.getByRole("menuitem", { name: "From a file" })
  await expect(importItem).toHaveAttribute("data-highlighted", "")
  const chooser = page.waitForEvent("filechooser")
  await page.keyboard.press("Enter")
  await chooser
})

test("library, reader and editor views reflow at a 320 CSS pixel viewport", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Narrow accessible supper")
  await page.setViewportSize({ width: 320, height: 800 })

  for (const path of [
    "/",
    "/books",
    `/recipes/${recipe.id}`,
    `/recipes/${recipe.id}/edit`,
    `/recipes/${recipe.id}/cook`,
    "/plan",
    "/shopping",
  ]) {
    await page.goto(path)
    await expect(page.locator("main")).toBeVisible()
    const overflow = await page.evaluate(() =>
      Array.from(document.querySelectorAll<HTMLElement>("body *"))
        .filter((element) => {
          const rect = element.getBoundingClientRect()
          return rect.right > window.innerWidth + 1 || rect.left < -1
        })
        .slice(0, 12)
        .map((element) => ({
          tag: element.tagName,
          className: element.className,
          text: element.textContent?.trim().slice(0, 80),
        })),
    )
    expect(overflow, `${path} has horizontal overflow`).toEqual([])
    if (path.endsWith("/edit")) {
      const step = await page.getByRole("textbox", { name: "Step 1", exact: true }).boundingBox()
      expect(step?.width).toBeGreaterThan(220)
    }
  }
})

test("invalid forms and dialogs have deterministic focus behavior", async ({ page }) => {
  await page.goto("/recipes/new")
  const title = page.getByRole("textbox", { name: "Title" })
  await title.fill("")
  await page.getByRole("button", { name: "Save recipe" }).click()
  await expect(title).toBeFocused()
  expect(await title.evaluate((element) => (element as HTMLInputElement).validity.valid)).toBe(
    false,
  )

  const ingredient = page.getByRole("textbox", { name: "Ingredient" }).first()
  const more = page.getByRole("button", { name: "More options for ingredient 1" })
  await more.focus()
  await page.keyboard.press("Enter")
  await page.getByRole("menuitem", { name: "Details, ingredient 1", exact: true }).click()
  await expect(page.getByRole("dialog")).toBeVisible()
  await page.keyboard.press("Escape")
  await expect(more).toBeFocused()
  await expect(ingredient).toBeVisible()
})

test("library search naming and URL import fields retain typing, composition and DOM identity", async ({
  page,
}) => {
  await page.goto("/?q=soup")
  await page.getByRole("button", { name: "Filters", exact: true }).click()
  await page.getByRole("button", { name: "Save this search" }).click()
  const name = page.getByRole("textbox", { name: "Name for this search" })
  await expectStableControlledEditing(name, "Weeknight", " 味噌")
  await page.getByRole("button", { name: "Save", exact: true }).click()
  await expect(page).toHaveURL(/view=/)
  await expect(page.getByRole("combobox", { name: "Saved search" })).toHaveCount(0)
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)

  await page.getByRole("button", { name: "Add recipes" }).click()
  await page.getByRole("menuitem", { name: "From a website" }).click()
  const url = page.getByRole("textbox", { name: "Recipe URL" })
  await url.evaluate((element) => {
    ;(window as Window & { controlledInput?: Element }).controlledInput = element
  })
  await url.pressSequentially("https://example.com/rece")
  // URL inputs do not expose selectionStart; inserting before the final letter
  // verifies the caret through the resulting value instead.
  await url.press("ArrowLeft")
  await url.pressSequentially("ip")
  await expect(url).toHaveValue("https://example.com/recipe")
  await url.press("End")
  await url.evaluate((element) => {
    const control = element as HTMLInputElement
    control.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true }))
    control.value += "茶"
    control.dispatchEvent(new InputEvent("input", { bubbles: true, data: "茶", isComposing: true }))
    control.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: "茶" }))
  })
  await expect(url).toHaveValue("https://example.com/recipe茶")
  await expect(url).toBeFocused()
  expect(
    await url.evaluate(
      (element) => (window as Window & { controlledInput?: Element }).controlledInput === element,
    ),
  ).toBe(true)
})

test("extracted source fields preserve editing identity across source-row updates", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Source editing")
  await page.goto(`/recipes/${recipe.id}`)
  await page.getByRole("button", { name: "Organize", exact: true }).click()
  const dialog = page.getByRole("dialog", { name: "Organize recipe" })
  await dialog.getByText("Notes and sources", { exact: true }).click()
  await expectStableControlledEditing(
    dialog.getByRole("textbox", { name: "Personal note", exact: true }),
    "Family favourite",
    " 茶",
  )
  await dialog.getByText("Sources", { exact: true }).click()
  await dialog.getByRole("button", { name: "Add a source", exact: true }).click()
  await expectStableControlledEditing(
    dialog.getByRole("textbox", { name: "Source", exact: true }),
    "https://example.com/",
    "茶",
  )
  await choose(dialog.getByRole("combobox", { name: "Kind of source" }), "other")
  await expectStableControlledEditing(
    dialog.getByRole("textbox", { name: "Source", exact: true }),
    "Family notes",
    " 茶",
  )
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click()
})

test("label and saved-search editors retain multi-character composition and caret position", async ({
  page,
}) => {
  await page.goto("/tags")
  const tags = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "Tags", exact: true }) })
  await tags.getByRole("button", { name: "Add", exact: true }).click()
  const name = tags.getByRole("textbox", { name: "New tag", exact: true })
  await expectStableControlledEditing(name, "Weeknight", " 茶")
  await name.press("Enter")
  await tags.getByRole("button", { name: "Weeknight 茶", exact: true }).click()
  const editor = page.getByRole("dialog", { name: "Edit tag", exact: true })
  await expectStableControlledEditing(
    editor.getByRole("textbox", { name: "Name", exact: true }),
    "Quick suppers",
    " 茶",
  )
  await editor.getByRole("button", { name: "Cancel", exact: true }).click()
  await page.goto("/saved-searches")
  await page.getByRole("button", { name: "New search", exact: true }).click()
  const search = page.getByRole("dialog", { name: "New saved search", exact: true })
  await expectStableControlledEditing(
    search.getByRole("textbox", { name: "Name", exact: true }),
    "Suppers",
    " 茶",
  )
  await expectStableControlledEditing(
    search.getByRole("textbox", { name: "Words to match", exact: true }),
    "Noodles",
    " 茶",
  )
  await search.getByRole("button", { name: "Cancel", exact: true }).click()
})

test("editor insertion and section movement support continuous keyboard entry", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Keyboard entry", {
    ingredient_sections: [
      { id: "first", title: "Dough", ingredients: [{ id: "flour", name: "flour" }] },
      { id: "second", title: "Topping", ingredients: [{ id: "sugar", name: "sugar" }] },
    ],
    instruction_sections: [
      { id: "mix", title: "Mix", steps: [{ id: "stir", text: "Stir." }] },
      { id: "bake", title: "Bake", steps: [{ id: "heat", text: "Heat." }] },
    ],
  })
  await page.goto(`/recipes/${recipe.id}/edit`)
  const ingredient = page.getByRole("textbox", { name: "Ingredient", exact: true }).first()
  await ingredient.focus()
  await ingredient.press("Control+Enter")
  await expect(page.getByRole("textbox", { name: "Ingredient", exact: true }).nth(1)).toBeFocused()
  const inserted = page.getByRole("textbox", { name: "Ingredient", exact: true }).nth(1)
  await expectStableControlledEditing(inserted, "Warm water", " 茶")
  await page.getByRole("button", { name: "Move group 1 down", exact: true }).focus()
  await page.keyboard.press("Enter")
  await expect(page.getByRole("textbox", { name: "Group name", exact: true }).nth(1)).toBeFocused()
  await expect(
    page.locator('[aria-live="polite"]').filter({ hasText: "Moved group to position 2." }),
  ).toHaveCount(1)
  await page.getByRole("textbox", { name: "Step 1", exact: true }).first().focus()
  await page.keyboard.press("Control+Enter")
  await expect(page.getByRole("textbox", { name: "Step 2", exact: true })).toBeFocused()
  await expectStableControlledEditing(
    page.getByRole("textbox", { name: "Step 2", exact: true }),
    "Rest gently",
    " 茶",
  )
  await page.getByRole("button", { name: "Move stage 1 down", exact: true }).focus()
  await page.keyboard.press("Enter")
  await expect(page.getByRole("textbox", { name: "Stage name", exact: true }).nth(1)).toBeFocused()
  await page.getByRole("button", { name: "Remove stage", exact: true }).nth(1).focus()
  await page.keyboard.press("Enter")
  await expect(page.getByRole("button", { name: "Confirm removal", exact: true })).toBeFocused()
  await page.keyboard.press("Enter")
  await expect(page.getByRole("textbox", { name: "Stage name", exact: true })).toBeFocused()
})

test("account, preferences, organization, history, and cooking surfaces pass axe", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Accessible workflow sweep")
  for (const path of [
    "/account",
    "/account/security",
    "/account/tokens",
    "/admin",
    "/libraries/default/access",
    "/preferences",
    "/tags",
    "/saved-searches",
    "/imports",
    `/recipes/${recipe.id}/history`,
    `/recipes/${recipe.id}/cook`,
  ]) {
    await page.goto(path)
    await expect(page.locator("main")).toBeVisible()
    await expectAccessible(page)
  }
})

test("timer completion announces once without countdown chatter", async ({ page, request }) => {
  await page.clock.install()
  const account = await (await request.get("/api/v1/account")).json()
  await page.addInitScript(
    ({ user, libraryID }) => {
      const now = Date.now()
      const key = `savorum.timers.v2:${JSON.stringify([location.origin, user, libraryID])}:accessible-timer`
      localStorage.setItem(
        key,
        JSON.stringify({
          id: "accessible-timer",
          label: "Tea",
          seconds: 3,
          startedAt: now,
          endsAt: now + 3000,
          status: "running",
        }),
      )
      localStorage.setItem(
        "savorum.timer-alerts.v1",
        JSON.stringify({ sound: false, notifications: false }),
      )
    },
    { user: account.user?.id ?? "local", libraryID: account.active_library_id },
  )
  await page.goto("/")
  const announcement = page.getByRole("status").filter({ hasText: "Tea timer finished." })
  await page.clock.fastForward(4000)
  await expect(announcement).toHaveCount(1)
  await announcement.evaluate((element) => {
    ;(window as Window & { timerAnnouncementMutations?: number }).timerAnnouncementMutations = 0
    new MutationObserver(() => {
      const tracked = window as Window & { timerAnnouncementMutations?: number }
      tracked.timerAnnouncementMutations = (tracked.timerAnnouncementMutations ?? 0) + 1
    }).observe(element, { childList: true, subtree: true, characterData: true })
  })
  await page.clock.fastForward(10000)
  await expect(announcement).toHaveText("Tea timer finished.")
  expect(
    await page.evaluate(
      () => (window as Window & { timerAnnouncementMutations?: number }).timerAnnouncementMutations,
    ),
  ).toBe(0)
})

test("ingredient alternatives retain moved identity and announce branch movement", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Accessible alternatives", {
    ingredient_sections: [
      {
        id: "ingredients",
        ingredients: [
          {
            id: "choice",
            relation: "or",
            alternatives: [
              { id: "milk", name: "milk" },
              { id: "oat", name: "oat milk" },
            ],
          },
          { id: "sugar", name: "sugar" },
        ],
      },
    ],
  })
  await page.goto(`/recipes/${recipe.id}/edit`)
  await page.getByRole("button", { name: "More options for milk", exact: true }).focus()
  await page.keyboard.press("ArrowDown")
  await expect(page.getByRole("menuitem", { name: "Details, milk", exact: true })).toHaveAttribute(
    "data-highlighted",
    "",
  )
  await page.keyboard.press("ArrowDown")
  await expect(
    page.getByRole("menuitem", { name: "Move down, milk", exact: true }),
  ).toHaveAttribute("data-highlighted", "")
  await page.keyboard.press("Enter")
  const moved = page.locator('[data-editor-id="milk"] [data-focus="primary"]')
  await expect(moved).toBeFocused()
  await expect(
    page
      .locator('[data-editor-id="milk"]')
      .getByRole("textbox", { name: "Ingredient", exact: true }),
  ).toHaveValue("milk")
  await expect(
    page.locator('[aria-live="polite"]').filter({ hasText: "Moved alternative to position 2." }),
  ).toHaveCount(1)
  await page.getByRole("button", { name: "Move choice down", exact: true }).focus()
  await page.keyboard.press("Enter")
  await expect(page.getByRole("combobox", { name: "Choice wording", exact: true })).toBeFocused()
  await page.getByRole("button", { name: "Add alternative", exact: true }).focus()
  await page.keyboard.press("Enter")
  await expect(page.getByRole("textbox", { name: "Ingredient", exact: true }).last()).toBeFocused()
  const added = page.getByRole("textbox", { name: "Ingredient", exact: true }).last()
  await expectStableControlledEditing(added, "Soy milk", " 茶")
})

test("shared filter and yield fields preserve continuous entry", async ({ page, request }) => {
  const recipe = await seedRecipe(request, "Shared input supper")
  for (let index = 0; index < 13; index++) {
    const response = await request.post("/api/v1/tags", { data: { name: `Entry check ${index}` } })
    expect(response.ok()).toBe(true)
  }
  await page.goto("/")
  await page.getByRole("button", { name: /Filters/ }).click()
  await page.getByRole("button", { name: "Choose a tag", exact: true }).click()
  await expectStableControlledEditing(
    page.getByRole("searchbox", { name: "Search tags", exact: true }),
    "Entry",
    " 茶",
  )
  await page.getByRole("button", { name: "Select", exact: true }).click()
  await page.getByRole("button", { name: new RegExp(recipe.title) }).click()
  await page
    .getByRole("region", { name: "Selected recipes" })
    .getByRole("button", { name: "Organize" })
    .click()
  const dialog = page.getByRole("dialog", { name: "Organize 1 recipe" })
  await dialog.getByRole("button", { name: "Add a tag", exact: true }).click()
  await expectStableControlledEditing(
    dialog.getByRole("searchbox", { name: "Search tags to add", exact: true }),
    "Entry",
    " 茶",
  )
  await page.goto(`/recipes/${recipe.id}`)
  const amount = page.getByRole("textbox", { name: /How many/ })
  await expectStableControlledEditing(amount, "12", "5")
})

test("planning, shopping, capture, print, and administration surfaces pass axe", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Accessible planned supper")
  const book = await seedBook(request, "Accessible planning", [recipe.id])
  const today = new Date()
  const date = [
    today.getFullYear(),
    String(today.getMonth() + 1).padStart(2, "0"),
    String(today.getDate()).padStart(2, "0"),
  ].join("-")
  const meal = await request.post("/api/v1/meals", {
    data: {
      date,
      meal_type: "dinner",
      dishes: [
        { entry_id: recipe.id, display_name: recipe.title, scale: "2" },
        { display_name: "Green salad", plain_items: [{ name: "Mixed leaves", quantity: "1 bag" }] },
      ],
    },
  })
  expect(meal.ok()).toBe(true)
  const suffix = Math.random().toString(36).slice(2, 10)
  const added = await request.post("/api/v1/shopping/lists/groceries/changes", {
    data: {
      id: `e2e-add-${suffix}`,
      kind: "add",
      item_id: `e2e-milk-${suffix}`,
      base_revision: 0,
      name: "Milk",
    },
  })
  expect(added.ok()).toBe(true)
  const list = (await added.json()) as { items: { id: string; revision: number }[] }
  const milk = list.items.find((item) => item.id === `e2e-milk-${suffix}`)
  const bought = await request.post("/api/v1/shopping/lists/groceries/changes", {
    data: {
      id: `e2e-buy-${suffix}`,
      kind: "edit",
      item_id: `e2e-milk-${suffix}`,
      base_revision: milk?.revision ?? 0,
      bought: true,
    },
  })
  expect(bought.ok()).toBe(true)

  for (const path of [
    "/plan",
    "/shopping",
    "/capture",
    "/books",
    `/books/${book.id}/history`,
    `/books/${book.id}/print`,
    `/recipes/${recipe.id}/print`,
    "/admin/audit",
    "/admin/libraries",
    "/admin/providers",
  ]) {
    await page.goto(path)
    await expect(page.locator("main")).toBeVisible()
    await expectAccessible(page)
  }
})
