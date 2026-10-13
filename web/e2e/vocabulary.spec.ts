import { type APIRequestContext, expect, test } from "@playwright/test"

import { chooseNamed } from "./controls"
import { seedRecipe } from "./library"

/**
 * Renaming, combining, and deleting labels, each stating how many recipes it
 * affects. Deleting a label recipes still carry asks first.
 */

async function createTag(request: APIRequestContext, name: string): Promise<string> {
  const response = await request.post("/api/v1/tags", { data: { name } })
  expect(response.ok()).toBeTruthy()
  return ((await response.json()) as { id: string }).id
}

async function fileUnder(request: APIRequestContext, recipeID: string, tagIDs: string[]) {
  const current = (await (
    await request.get(`/api/v1/recipes/${recipeID}/organization`)
  ).json()) as { revision: number }
  const response = await request.put(`/api/v1/recipes/${recipeID}/organization`, {
    headers: { "If-Match": `"${current.revision}"` },
    data: { tag_ids: tagIDs, facet_ids: [], user_state: { favorite: false }, sources: [] },
  })
  expect(response.ok()).toBeTruthy()
}

async function tagsOn(request: APIRequestContext, recipeID: string): Promise<string[]> {
  const response = await request.get(`/api/v1/recipes/${recipeID}/organization`)
  expect(response.ok()).toBeTruthy()
  return ((await response.json()) as { tag_ids?: string[] }).tag_ids ?? []
}

test("editing a tag says what it is on, and combining is its own decision", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Vocabulary weeknight supper")
  const suffix = Math.random().toString(36).slice(2, 8)
  const retiring = `Vocabulary retiring ${suffix}`
  const keeping = `Vocabulary keeping ${suffix}`
  const retiringID = await createTag(request, retiring)
  await createTag(request, keeping)
  await fileUnder(request, recipe.id, [retiringID])

  await page.goto("/tags")
  await page.getByRole("button", { name: retiring, exact: true }).click()
  const editor = page.getByRole("dialog", { name: "Edit tag", exact: true })
  await expect(editor).toBeVisible()
  await expect(editor.getByText("Used on 1 recipe.", { exact: true })).toBeVisible()

  // Deleting a tag recipes carry asks first, and Back returns to the editor.
  await editor.getByRole("button", { name: "Delete", exact: true }).click()
  const confirm = page.getByRole("dialog", { name: `Delete ${retiring}?`, exact: true })
  await expect(confirm.getByText(/This takes it off 1 recipe\./)).toBeVisible()
  await confirm.getByRole("button", { name: "Back", exact: true }).click()

  // Renaming never passes a merge selector.
  await expect(editor.getByRole("combobox")).toHaveCount(0)
  await editor.getByRole("textbox", { name: "Name", exact: true }).fill("Vocabulary renamed")

  await editor.getByRole("button", { name: "Combine with another tag", exact: true }).click()
  const combine = page.getByRole("dialog", { name: "Combine with another tag", exact: true })
  await expect(
    combine.getByText(new RegExp(`Recipes with ${retiring} get it instead`)),
  ).toBeVisible()
  await expect(combine.getByRole("button", { name: "Combine", exact: true })).toBeDisabled()
  await chooseNamed(combine.getByRole("combobox", { name: "Keep this tag", exact: true }), keeping)
  await expect(
    combine.getByText(
      `1 recipe switches from ${retiring} to ${keeping}, and ${retiring} goes away.`,
    ),
  ).toBeVisible()

  // Back returns to the editor exactly as it was left.
  await combine.getByRole("button", { name: "Back", exact: true }).click()
  await expect(editor.getByRole("textbox", { name: "Name", exact: true })).toHaveValue(
    "Vocabulary renamed",
  )

  await editor.getByRole("button", { name: "Combine with another tag", exact: true }).click()
  await chooseNamed(combine.getByRole("combobox", { name: "Keep this tag", exact: true }), keeping)
  await combine.getByRole("button", { name: "Combine", exact: true }).click()

  await expect(page.getByRole("button", { name: retiring, exact: true })).toHaveCount(0)
  await expect(page.getByRole("button", { name: keeping, exact: true })).toBeVisible()
  const carried = await tagsOn(request, recipe.id)
  expect(carried).not.toContain(retiringID)
  expect(carried).toHaveLength(1)

  // A tag no recipe uses is deleted without a second step.
  const unused = `Vocabulary unused ${suffix}`
  await createTag(request, unused)
  await page.reload()
  await page.getByRole("button", { name: unused, exact: true }).click()
  await expect(editor.getByText("Not on any recipes yet.", { exact: true })).toBeVisible()
  await editor.getByRole("button", { name: "Delete", exact: true }).click()
  await expect(page.getByRole("button", { name: unused, exact: true })).toHaveCount(0)
})

test("deleting a tag recipes carry confirms, then takes it off them", async ({ page, request }) => {
  const recipe = await seedRecipe(request, "Vocabulary deleted tag supper")
  const name = `Vocabulary deleting ${Math.random().toString(36).slice(2, 8)}`
  const tagID = await createTag(request, name)
  await fileUnder(request, recipe.id, [tagID])

  await page.goto("/tags")
  await page.getByRole("button", { name, exact: true }).click()
  const editor = page.getByRole("dialog", { name: "Edit tag", exact: true })
  await expect(editor.getByText("Used on 1 recipe.", { exact: true })).toBeVisible()
  await editor.getByRole("button", { name: "Delete", exact: true }).click()

  const confirm = page.getByRole("dialog", { name: `Delete ${name}?`, exact: true })
  await expect(confirm.getByText(/The recipe itself stays in your library/)).toBeVisible()
  await confirm.getByRole("button", { name: "Delete", exact: true }).click()

  await expect(page.getByRole("button", { name, exact: true })).toHaveCount(0)
  expect(await tagsOn(request, recipe.id)).not.toContain(tagID)
})
