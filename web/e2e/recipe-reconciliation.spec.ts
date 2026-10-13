import { expect, test } from "@playwright/test"

import { choose, chooser } from "./controls"
import { seedRecipe } from "./library"

test("two selected recipes are compared where they were found, then combined", async ({
  page,
  request,
}) => {
  const left = await seedRecipe(request, "Merge first recipe", {
    notes: ["Left keeps its own note."],
  })
  const right = await seedRecipe(request, "Merge second recipe", {
    notes: ["Left keeps its own note."],
  })

  // Comparing starts from the recipes, not from a menu and an empty page that
  // asks for them to be found a second time.
  await page.goto("/")
  await page.getByRole("button", { name: "Select" }).click()
  await page.getByRole("button", { name: new RegExp(left.title) }).click()
  await page.getByRole("button", { name: new RegExp(right.title) }).click()
  await expect(page.getByText("2 selected")).toBeVisible()
  await page
    .getByRole("region", { name: "Selected recipes" })
    .getByRole("button", { name: "Compare" })
    .click()

  await expect(page.getByRole("heading", { name: "Compare two recipes" })).toBeVisible()
  await expect(page.getByRole("heading", { name: left.title, exact: true })).toBeVisible()
  await expect(page.getByRole("heading", { name: right.title, exact: true })).toBeVisible()
  // What the two disagree about is marked, and what they agree about says so
  // rather than being read twice.
  const titles = page.getByRole("region", { name: "Title", exact: true })
  await expect(titles).toContainText("Different")
  await expect(page.getByRole("region", { name: "Notes", exact: true })).toContainText(
    "The same in both",
  )
  // Nothing here is a stored field name, in what is shown or in what is read out.
  const comparison = await page.locator("#main-content").innerText()
  expect(comparison).not.toMatch(/ingredient_sections|instruction_sections|source_locale/)
  const accessible = await page.locator("#main-content").evaluate((node) => node.outerHTML)
  expect(accessible).not.toMatch(/aria-label="[^"]*_sections/)

  // Cancelling a comparison comes back to the recipes it started from.
  await page.getByRole("button", { name: "Back to selection", exact: true }).click()
  await expect(page.getByText("2 selected")).toBeVisible()
  await page
    .getByRole("region", { name: "Selected recipes" })
    .getByRole("button", { name: "Compare" })
    .click()

  // Combining is a decision of its own, taken after the reading.
  await expect(page.getByRole("button", { name: "Combine recipes", exact: true })).toHaveCount(0)
  await page.getByRole("button", { name: "Combine these recipes", exact: true }).click()
  await expect(page.getByRole("heading", { name: "Combine into one recipe" })).toBeVisible()
  await expect(page.getByRole("button", { name: "Combine recipes", exact: true })).toBeDisabled()
  await page
    .getByRole("group", { name: "Which recipe do you keep?", exact: true })
    .getByRole("radio", { name: right.title, exact: true })
    .check()
  // The result is stated before it happens, and it names both recipes.
  await expect(page.getByText(`${left.title} moves to Trash`)).toBeVisible()
  await page
    .getByRole("group", { name: "Choose the Title to keep", exact: true })
    .getByRole("radio", { name: left.title, exact: true })
    .check()
  await expect(page.getByText(`Title: from ${left.title}`)).toBeVisible()

  await page.getByRole("button", { name: "Combine recipes", exact: true }).click()
  await expect(page).toHaveURL(`/recipes/${right.id}`)
  const kept = await request.get(`/api/v1/recipes/${right.id}`)
  expect(kept.headers().etag).toBe('"2"')
  expect((await kept.json()).recipe.title).toBe(left.title)
  const trash = await request.get(`/api/v1/trash/recipes/${left.id}`)
  expect(trash.ok()).toBe(true)
})

test("the comparison page reached directly still asks which two recipes", async ({
  page,
  request,
}) => {
  const left = await seedRecipe(request, "Direct compare first")
  const right = await seedRecipe(request, "Direct compare second")
  await page.goto("/recipes/merge")
  const search = page.getByRole("textbox", { name: "Find recipes" })
  await search.fill("Direct compare")
  await page.getByRole("button", { name: "Search recipes", exact: true }).click()
  await choose(chooser(page, "First recipe"), left.id)
  await choose(chooser(page, "Second recipe"), right.id)
  await page.getByRole("button", { name: "Compare recipes", exact: true }).click()
  await expect(page.getByRole("heading", { name: "Compare two recipes" })).toBeVisible()
  // There is no selection behind this one, so there is nothing to go back to.
  await expect(page.getByRole("button", { name: "Back to selection" })).toHaveCount(0)
  await page.getByRole("button", { name: "Choose recipes", exact: true }).click()
  await expect(page.getByRole("combobox", { name: "First recipe", exact: true })).toBeVisible()
})

test("source refresh shows actual conflicts and keeps independent local edits", async ({
  page,
  request,
}) => {
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/refresh-source.json")
  await page
    .getByRole("region", { name: "Save this recipe" })
    .getByRole("button", { name: "Save recipe", exact: true })
    .click()
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()
  const path = await page
    .getByRole("link", { name: "Source refresh saffron rice", exact: true })
    .getAttribute("href")
  const initial = await request.get(`/api/v1${path}`)
  const entry = await initial.json()
  const remote = structuredClone(entry.recipe)
  remote.title = "Source saffron rice update"
  remote.notes = ["Rest for five minutes."]
  const local = {
    ...entry.recipe,
    title: "My saffron rice",
    description: "My local cooking note.",
  }
  const update = await request.put(`/api/v1${path}`, {
    headers: { "If-Match": initial.headers().etag },
    data: local,
  })
  expect(update.ok()).toBe(true)
  const preview = await request.post(`/api/v1${path}/source-refresh/preview`, {
    data: { expected_revision: 2, remote },
  })
  expect(preview.ok(), await preview.text()).toBe(true)
  const plan = await preview.json()
  await page.route("**/source-refresh/preview-url", async (route) => {
    expect(route.request().postDataJSON().url).toBe("https://recipes.example/saffron-rice")
    await route.fulfill({ json: plan })
  })
  await page.goto(`${path}/refresh`)
  // A recipe action that cannot say which recipe it is about leaves no way
  // back to it, and a heading about a known source has to name that source.
  await expect(page.getByRole("link", { name: "Back to My saffron rice" })).toHaveAttribute(
    "href",
    path!,
  )
  await expect(
    page.getByRole("heading", { name: "Check the source of My saffron rice" }),
  ).toBeVisible()
  await expect(page.getByText("recipes.example as its source")).toBeVisible()
  const url = page.getByRole("textbox", { name: "Recipe source URL" })
  await expect(url).toHaveValue("https://recipes.example/saffron-rice")
  await url.focus()
  await url.press("End")
  await url.pressSequentially("/test")
  await expect(url).toBeFocused()
  await expect(url).toHaveValue("https://recipes.example/saffron-rice/test")
  await url.evaluate((element: HTMLInputElement) => {
    element.dataset.editIdentity = "retained"
    element.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true }))
    element.value += "茶"
    element.dispatchEvent(new InputEvent("input", { bubbles: true, data: "茶", isComposing: true }))
    element.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: "茶" }))
  })
  await expect(url).toHaveValue("https://recipes.example/saffron-rice/test茶")
  await expect(url).toBeFocused()
  await expect(url).toHaveAttribute("data-edit-identity", "retained")
  await url.fill("https://recipes.example/saffron-rice")
  await page.getByRole("button", { name: "Check source", exact: true }).click()
  await expect(
    page.getByRole("button", { name: "Apply reviewed updates", exact: true }),
  ).toBeDisabled()
  // Each side is named and read as the recipe reads it, not as the value that
  // happens to be stored.
  const titleChoice = page.getByRole("group", { name: "Choose the Title to keep", exact: true })
  const disputed = page.locator("div").filter({ has: titleChoice }).last()
  await expect(disputed.getByText("My edit", { exact: true })).toBeVisible()
  await expect(disputed.getByRole("listitem").filter({ hasText: "My saffron rice" })).toBeVisible()
  await expect(disputed.getByText("The source", { exact: true })).toBeVisible()
  await expect(
    disputed.getByRole("listitem").filter({ hasText: "Source saffron rice update" }),
  ).toBeVisible()
  // The group names the part of the recipe in dispute, not the field it is
  // stored in.
  await titleChoice.getByRole("radio", { name: "Use source", exact: true }).check()
  await page.getByRole("button", { name: "Apply reviewed updates", exact: true }).click()
  await expect(page).toHaveURL(path!)
  const result = await request.get(`/api/v1${path}`)
  expect(result.headers().etag).toBe('"3"')
  expect((await result.json()).recipe).toMatchObject({
    title: remote.title,
    description: local.description,
    notes: remote.notes,
  })
})

test("a recipe with no recorded source is asked for one rather than rechecked", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Handwritten family supper")

  await page.goto(`/recipes/${recipe.id}`)
  await page.getByRole("button", { name: "More actions" }).click()
  // Nothing can be refreshed from an address the recipe does not have, and a
  // menu offering to check one implies Savorum already knows where it is.
  await page.getByRole("menuitem", { name: "Add a source link" }).click()
  await expect(
    page.getByRole("heading", { name: `Add a source link for ${recipe.title}` }),
  ).toBeVisible()
  await expect(page.getByText("records no source address")).toBeVisible()
  await page.getByRole("link", { name: `Back to ${recipe.title}` }).click()
  await expect(page.getByRole("heading", { name: recipe.title })).toBeVisible()
})
