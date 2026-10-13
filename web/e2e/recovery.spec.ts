import { expect, test } from "@playwright/test"

import { seedRecipe } from "./library"

const draftKeys = () =>
  Object.keys(localStorage).filter((key) => key.startsWith("savorum.recipe-draft"))

test("an interrupted entry session is offered back, and saving settles it", async ({
  page,
  request,
}) => {
  await page.goto("/recipes/new")
  await page.getByRole("textbox", { name: "Title", exact: true }).fill("Interrupted supper")
  await page.getByRole("textbox", { name: "Ingredient", exact: true }).fill("smoked paprika")
  await page.getByRole("textbox", { name: "Step 1", exact: true }).fill("Toast the spices.")
  await expect.poll(async () => (await page.evaluate(draftKeys)).length).toBeGreaterThan(0)
  // The entry is scoped to who is working and where, not left under a bare key.
  expect(await page.evaluate(draftKeys)).toEqual(["savorum.recipe-draft.v1:local:default:new"])

  page.once("dialog", (dialog) => void dialog.accept())
  await page.reload()
  const offer = page.getByRole("status").filter({ hasText: "Unsaved work from" })
  await expect(offer).toBeVisible()
  await expect(page.getByRole("textbox", { name: "Title", exact: true })).toHaveValue("")
  await offer.getByRole("button", { name: "Restore it", exact: true }).click()
  await expect(page.getByRole("textbox", { name: "Title", exact: true })).toHaveValue(
    "Interrupted supper",
  )
  await expect(page.getByRole("textbox", { name: "Title", exact: true })).toBeFocused()
  await expect(page.getByRole("textbox", { name: "Ingredient", exact: true })).toHaveValue(
    "smoked paprika",
  )

  // Restored work is unsaved work: leaving still asks.
  await page.getByRole("link", { name: "Recipes", exact: true }).click()
  const leaving = page.getByRole("dialog", { name: "Leave without saving?" })
  await expect(leaving).toBeVisible()
  await leaving.getByRole("button", { name: "Keep editing", exact: true }).click()

  await page.getByRole("button", { name: "Save recipe", exact: true }).click()
  await expect(page).toHaveURL(/\/recipes\/r[a-z0-9]+$/)
  const id = page.url().split("/").at(-1)
  expect((await (await request.get(`/api/v1/recipes/${id}`)).json()).recipe.title).toBe(
    "Interrupted supper",
  )
  expect(await page.evaluate(draftKeys)).toEqual([])
  await page.goto("/recipes/new")
  await expect(page.getByText("Unsaved work from")).toHaveCount(0)
})

for (const viewport of [
  { name: "desktop", width: 1280, height: 800 },
  { name: "mobile", width: 390, height: 844 },
]) {
  test(`restoring over current edits requires confirmation on ${viewport.name}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.goto("/recipes/new")
    const title = page.getByRole("textbox", { name: "Title", exact: true })
    const ingredient = page.getByRole("textbox", { name: "Ingredient", exact: true })
    const step = page.getByRole("textbox", { name: "Step 1", exact: true })
    await title.fill("Earlier supper")
    await ingredient.fill("smoked paprika")
    await step.fill("Toast the spices.")
    await expect.poll(async () => (await page.evaluate(draftKeys)).length).toBeGreaterThan(0)
    page.once("dialog", (dialog) => void dialog.accept())
    await page.reload()

    const offer = page.getByRole("status").filter({ hasText: "Unsaved work from" })
    const restore = offer.getByRole("button", { name: "Restore it", exact: true })
    await expect(offer).toBeVisible()
    const [draftKey] = await page.evaluate(draftKeys)
    const stored = await page.evaluate((key) => localStorage.getItem(key), draftKey)
    await title.fill("Current supper")
    await ingredient.fill("fresh ginger")
    await step.fill("Chop the ginger.")
    await restore.click()
    const confirmation = page.getByRole("dialog", { name: "Replace your current edits?" })
    await expect(confirmation).toBeVisible()
    await expect(confirmation).toContainText("Your current edits will be lost.")
    const keepEditing = confirmation.getByRole("button", { name: "Keep editing", exact: true })
    await expect(keepEditing).toBeFocused()
    const screenshot = testInfo.outputPath(`${viewport.name}-restore-confirmation.png`)
    await page.screenshot({ path: screenshot })
    await testInfo.attach(`${viewport.name}-restore-confirmation`, {
      path: screenshot,
      contentType: "image/png",
    })
    await keepEditing.click()
    await expect(confirmation).toHaveCount(0)
    await expect(restore).toBeFocused()
    await expect(title).toHaveValue("Current supper")
    await expect(ingredient).toHaveValue("fresh ginger")
    await expect(step).toHaveValue("Chop the ginger.")
    await expect(offer).toBeVisible()
    expect(await page.evaluate((key) => localStorage.getItem(key), draftKey)).toBe(stored)

    await restore.click()
    await expect(confirmation).toBeVisible()
    await page.keyboard.press("Escape")
    await expect(confirmation).toHaveCount(0)
    await expect(restore).toBeFocused()
    await expect(title).toHaveValue("Current supper")

    await restore.click()
    await confirmation.getByRole("button", { name: "Replace and restore", exact: true }).click()
    await expect(confirmation).toHaveCount(0)
    await expect(offer).toHaveCount(0)
    await expect(title).toHaveValue("Earlier supper")
    await expect(title).toBeFocused()
    await expect(ingredient).toHaveValue("smoked paprika")
    await expect(step).toHaveValue("Toast the spices.")
    await page.getByRole("button", { name: "Cancel", exact: true }).click()
    const leaving = page.getByRole("dialog", { name: "Leave without saving?" })
    await expect(leaving).toBeVisible()
    await leaving.getByRole("button", { name: "Keep editing", exact: true }).click()
    await expect(title).toHaveValue("Earlier supper")
  })
}

test("a save the server refuses keeps the entry, submits once, and recovers", async ({
  page,
  request,
}) => {
  let attempts = 0
  let failing = true
  await page.route("**/api/v1/recipes", async (route) => {
    if (route.request().method() !== "POST") return route.fallback()
    attempts += 1
    if (!failing) return route.fallback()
    await new Promise((resolve) => setTimeout(resolve, 1_200))
    await route.fulfill({
      status: 503,
      contentType: "application/problem+json",
      body: JSON.stringify({ title: "Unavailable", detail: "The library is not reachable." }),
    })
  })
  await page.goto("/recipes/new")
  await page.getByRole("textbox", { name: "Title", exact: true }).fill("Refused supper")
  await page.getByRole("textbox", { name: "Ingredient", exact: true }).fill("dried chilli")
  await page.getByRole("textbox", { name: "Step 1", exact: true }).fill("Simmer slowly.")

  const save = page.getByRole("button", { name: "Save recipe", exact: true })
  await save.click()
  // A second press while the first is in flight must not create a second recipe.
  await save.click({ force: true })
  await expect(page.getByRole("alert").first()).toBeVisible()
  expect(attempts).toBe(1)
  await expect(page.getByRole("textbox", { name: "Title", exact: true })).toHaveValue(
    "Refused supper",
  )
  await expect.poll(async () => (await page.evaluate(draftKeys)).length).toBeGreaterThan(0)

  failing = false
  await save.click()
  await expect(page).toHaveURL(/\/recipes\/r[a-z0-9]+$/)
  const id = page.url().split("/").at(-1)
  expect((await (await request.get(`/api/v1/recipes/${id}`)).json()).recipe.title).toBe(
    "Refused supper",
  )
  const listed = await (
    await request.get(`/api/v1/recipes?q=${encodeURIComponent("Refused supper")}`)
  ).json()
  expect(
    (listed.items as { title: string }[]).filter((item) => item.title === "Refused supper"),
  ).toHaveLength(1)
})

test("leaving deliberately settles the entry rather than offering it back", async ({ page }) => {
  await page.goto("/recipes/new")
  await page.getByRole("textbox", { name: "Title", exact: true }).fill("Abandoned supper")
  await expect.poll(async () => (await page.evaluate(draftKeys)).length).toBeGreaterThan(0)
  await page.getByRole("link", { name: "Recipes", exact: true }).click()
  await page.getByRole("button", { name: "Discard changes", exact: true }).click()
  await expect(page).toHaveURL(/\/$/)
  expect(await page.evaluate(draftKeys)).toEqual([])
  await page.goto("/recipes/new")
  await expect(page.getByText("Unsaved work from")).toHaveCount(0)
})

test("a search and a filter that match nothing offer the whole library back", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Recovery searchable supper")
  const tagResponse = await request.post("/api/v1/tags", { data: { name: "Recovery unused" } })
  expect(tagResponse.ok()).toBe(true)

  await page.goto("/?q=zzzznothingmatchesthis")
  await expect(page.getByText("Nothing matches that search.")).toBeVisible()
  await page.getByRole("button", { name: "Show all recipes", exact: true }).click()
  await expect(page.getByRole("link", { name: new RegExp(recipe.title) })).toBeVisible()

  // A filter that matches nothing is a narrowed library, not an empty one.
  await page.getByRole("button", { name: /Filters/ }).click()
  await page.getByRole("button", { name: "Choose a tag", exact: true }).click()
  const tagSearch = page.getByRole("searchbox", { name: "Search tags", exact: true })
  if (await tagSearch.count()) await tagSearch.fill("Recovery unused")
  await page.getByRole("button", { name: "Narrow to Recovery unused", exact: true }).click()
  await expect(page.getByText("Nothing matches that search.")).toBeVisible()
  await expect(page.getByRole("heading", { name: "Your library is empty." })).toHaveCount(0)
  await page.getByRole("button", { name: "Show all recipes", exact: true }).click()
  await expect(page.getByRole("link", { name: new RegExp(recipe.title) })).toBeVisible()
})

test("trashing a recipe offers the way back, and says so when the way back is refused", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Recovery trashed supper")
  await page.goto(`/recipes/${recipe.id}`)
  await page.getByRole("button", { name: "More actions", exact: true }).click()
  await page.getByRole("menuitem", { name: "Move to trash", exact: true }).click()
  await page.getByRole("dialog").getByRole("button", { name: "Move to trash", exact: true }).click()
  await expect(page).toHaveURL("/")

  const notice = page.getByRole("status").filter({ hasText: recipe.title })
  await expect(notice).toContainText("was moved to trash")

  // A restore the server refuses is reported, and the offer stays.
  await page.route("**/restore", (route) =>
    route.fulfill({
      status: 412,
      contentType: "application/problem+json",
      body: JSON.stringify({
        code: "recipe-revision-conflict",
        detail: "The recipe changed after it was read.",
      }),
    }),
  )
  await notice.getByRole("button", { name: "Undo", exact: true }).click()
  await expect(page.getByRole("alert").first()).toBeVisible()
  await expect(notice).toBeVisible()

  await page.unroute("**/restore")
  await notice.getByRole("button", { name: "Undo", exact: true }).click()
  await expect(notice).toHaveCount(0)
  await expect(page.getByRole("link", { name: new RegExp(recipe.title) })).toBeVisible()
  const restored = await (await request.get(`/api/v1/recipes/${recipe.id}`)).json()
  expect(restored.deleted_at ?? null).toBeNull()

  // The undo offer travels in history state and must not outlive the action
  // it belonged to across a page reload.
  await page.reload()
  await expect(page.getByRole("status").filter({ hasText: recipe.title })).toHaveCount(0)
  await expect(page.getByRole("link", { name: new RegExp(recipe.title) })).toBeVisible()
})

test("earlier versions say what each change touched", async ({ page, request }) => {
  const recipe = await seedRecipe(request, "History summary supper")

  // Two ordinary edits in different parts of the recipe. A list of dates that
  // all read "Edited" can only be told apart by opening each one in turn.
  const first = await request.get(`/api/v1/recipes/${recipe.id}`)
  const document = (await first.json()).recipe
  const withNote = await request.put(`/api/v1/recipes/${recipe.id}`, {
    headers: { "If-Match": first.headers().etag },
    data: { ...document, notes: [...(document.notes ?? []), "Rest it for five minutes."] },
  })
  expect(withNote.ok(), await withNote.text()).toBe(true)
  const renamed = await request.put(`/api/v1/recipes/${recipe.id}`, {
    headers: { "If-Match": withNote.headers().etag },
    data: {
      ...(await withNote.json()).recipe,
      title: "History summary supper, renamed",
      description: "A shorter summary.",
    },
  })
  expect(renamed.ok(), await renamed.text()).toBe(true)

  await page.goto(`/recipes/${recipe.id}/history`)
  const versions = page.getByRole("listitem")
  await expect(versions.nth(0)).toContainText("Title and Summary")
  await expect(versions.nth(1)).toContainText("Notes")
  // The first version lists no changes, and its cause is said in words.
  await expect(versions.nth(2)).not.toContainText("Notes")
  await expect(versions.nth(2)).toContainText("Saved")
  // History never names a stored field.
  expect(await page.locator("#main-content").innerText()).not.toMatch(
    /ingredient_sections|instruction_sections/,
  )
})
