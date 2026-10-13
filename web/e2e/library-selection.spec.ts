import { type APIRequestContext, type Locator, expect, test } from "@playwright/test"

import { seedBook, seedRecipe } from "./library"

/**
 * Bulk organization through the real API: a plan changes only what it names,
 * and a partly failed save reports which recipes are still waiting.
 */

/**
 * Opens one group's list and finds the value on offer there. A group only
 * grows a search box once the library has more labels than fit, so which of
 * the two ways to reach a value applies depends on how much else the run has
 * seeded.
 */
async function offered(
  dialog: Locator,
  action: string,
  noun: string,
  label: string,
  name: string,
): Promise<Locator> {
  await dialog.getByRole("button", { name: action, exact: true }).click()
  const search = dialog.getByRole("searchbox", { name: `Search ${noun}` })
  if ((await search.count()) > 0) await search.fill(label)
  return dialog.getByRole("button", { name, exact: true })
}

async function organization(request: APIRequestContext, id: string) {
  const response = await request.get(`/api/v1/recipes/${id}/organization`)
  expect(response.ok()).toBeTruthy()
  return (await response.json()) as { tag_ids?: string[]; facet_ids?: string[] }
}

test("recipes chosen in the library are filed in one pass", async ({ page, request }) => {
  const first = await seedRecipe(request, "Bulk filed first supper")
  const second = await seedRecipe(request, "Bulk filed second supper")
  const untouched = await seedRecipe(request, "Bulk untouched supper")
  const book = await seedBook(request, "Bulk weeknight collection", [])
  const suffix = Math.random().toString(36).slice(2, 8)
  const keeping = `Bulk keeping ${suffix}`
  const adding = `Bulk adding ${suffix}`
  for (const name of [keeping, adding]) {
    expect((await request.post("/api/v1/tags", { data: { name } })).ok()).toBeTruthy()
  }

  // One recipe already carries a tag the edit says nothing about, which has to
  // survive: the recipes in a selection never agree about what they hold.
  const tags = (await (await request.get("/api/v1/tags")).json()) as {
    items: { id: string; name: string }[]
  }
  const keepingID = tags.items.find((tag) => tag.name === keeping)!.id
  const existing = (await (
    await request.get(`/api/v1/recipes/${first.id}/organization`)
  ).json()) as { revision: number }
  expect(
    (
      await request.put(`/api/v1/recipes/${first.id}/organization`, {
        headers: { "If-Match": `"${existing.revision}"` },
        data: { tag_ids: [keepingID], facet_ids: [], user_state: { favorite: false }, sources: [] },
      })
    ).ok(),
  ).toBeTruthy()

  await page.goto("/")
  await page.getByRole("button", { name: "Select" }).click()
  await page.getByRole("button", { name: new RegExp(first.title) }).click()
  await page.getByRole("button", { name: new RegExp(second.title) }).click()
  await expect(page.getByText("2 selected")).toBeVisible()

  await page
    .getByRole("region", { name: "Selected recipes" })
    .getByRole("button", { name: "Organize" })
    .click()
  const dialog = page.getByRole("dialog", { name: "Organize 2 recipes" })
  await expect(dialog).toBeVisible()

  // Nothing chosen yet is nothing to apply.
  await expect(dialog.getByRole("button", { name: "Apply to 2 recipes" })).toBeDisabled()
  await expect(dialog.getByText("Nothing chosen yet. Add or take off a label below.")).toBeVisible()

  // The chips are ordinary buttons, so the keyboard reaches them the same way.
  await (
    await offered(dialog, "Add a tag", "tags to add", adding, `Add ${adding} to 2 recipes.`)
  ).focus()
  await page.keyboard.press("Enter")
  await (
    await offered(
      dialog,
      "Add to a book",
      "books to add",
      book.title,
      `Add 2 recipes to ${book.title}.`,
    )
  ).click()

  // The edit can be read back as the changes it will make, without pressing
  // anything to find out what a control currently means.
  const summary = dialog.getByRole("region", { name: "These changes" })
  await expect(summary).toContainText(`Add ${adding} to 2 recipes.`)
  await expect(summary).toContainText(`Add 2 recipes to ${book.title}.`)

  await dialog.getByRole("button", { name: "Apply to 2 recipes" }).click()
  await expect(dialog).not.toBeVisible()

  const addingID = tags.items.find((tag) => tag.name === adding)!.id
  for (const recipe of [first, second]) {
    expect((await organization(request, recipe.id)).tag_ids).toContain(addingID)
  }
  // The tag the edit never named is still there.
  expect((await organization(request, first.id)).tag_ids).toContain(keepingID)
  // A recipe that was not selected is untouched.
  expect((await organization(request, untouched.id)).tag_ids ?? []).toHaveLength(0)

  const members = (await (await request.get(`/api/v1/books/${book.id}`)).json()) as {
    entries?: { library_entry_id: string }[]
  }
  expect((members.entries ?? []).map((entry) => entry.library_entry_id)).toEqual(
    expect.arrayContaining([first.id, second.id]),
  )
})

test("a bulk edit that fails part way through says what is still waiting", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Bulk refused supper")
  const name = `Bulk refused ${Math.random().toString(36).slice(2, 8)}`
  expect((await request.post("/api/v1/tags", { data: { name } })).ok()).toBeTruthy()

  await page.goto("/")
  await page.getByRole("button", { name: "Select" }).click()
  await page.getByRole("button", { name: new RegExp(recipe.title) }).click()
  await page
    .getByRole("region", { name: "Selected recipes" })
    .getByRole("button", { name: "Organize" })
    .click()

  const dialog = page.getByRole("dialog", { name: "Organize 1 recipe" })
  await (
    await offered(dialog, "Add a tag", "tags to add", name, `Add ${name} to 1 recipe.`)
  ).click()

  await page.route("**/organization", async (route) => {
    if (route.request().method() === "PUT") {
      await route.fulfill({ status: 503, json: { title: "Unavailable", status: 503 } })
      return
    }
    await route.continue()
  })
  await dialog.getByRole("button", { name: "Apply to 1 recipe" }).click()
  await expect(dialog.getByRole("alert")).toBeVisible()
  await expect(dialog.getByText("1 of 1 could not be saved. The rest were.")).toBeVisible()

  // The plan survives the failure, so the reader retries rather than rebuilds.
  await page.unroute("**/organization")
  await dialog.getByRole("button", { name: "Try 1 again" }).click()
  await expect(dialog).not.toBeVisible()
  expect((await organization(request, recipe.id)).tag_ids ?? []).toHaveLength(1)
})
