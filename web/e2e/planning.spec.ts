import { expect, test } from "@playwright/test"

import { seedRecipe } from "./library"

function localDate(offset = 0): string {
  const date = new Date()
  date.setDate(date.getDate() + offset)
  return [
    date.getFullYear(),
    String(date.getMonth() + 1).padStart(2, "0"),
    String(date.getDate()).padStart(2, "0"),
  ].join("-")
}

test("the plan keeps its view in the URL and marks recipes that are gone", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Planned then trashed stew")
  const date = localDate()
  const created = await request.post("/api/v1/meals", {
    data: {
      date,
      meal_type: "dinner",
      dishes: [{ entry_id: recipe.id, display_name: recipe.title, scale: "2" }],
    },
  })
  expect(created.ok()).toBe(true)

  await page.goto(`/plan?view=month&date=${date}`)
  await expect(page.getByRole("button", { name: "Month", pressed: true })).toBeVisible()
  await expect(
    page.getByRole("table").getByRole("button", { name: `Dinner: ${recipe.title} (2 batches)` }),
  ).toBeVisible()

  await page
    .getByRole("table")
    .getByRole("button", { name: `Dinner: ${recipe.title} (2 batches)` })
    .click()
  const preview = page.getByRole("dialog")
  await expect(preview.getByText(`${recipe.title} (2 batches)`, { exact: true })).toBeVisible()
  await preview.getByRole("button", { name: "Done", exact: true }).click()
  await expect(preview).toBeHidden()

  await page.getByRole("button", { name: "Saved meals", exact: true }).click()
  await expect(page.getByRole("table")).toBeVisible()
  await expect(page).toHaveURL(/view=month/)
  await page.getByRole("button", { name: "Unscheduled", exact: true }).click()

  await page.getByRole("button", { name: "Agenda" }).click()
  await expect(page).toHaveURL(/view=agenda/)
  await expect(page.getByRole("link", { name: `Open ${recipe.title}` })).toBeVisible()
  await page.goBack()
  await expect(page).toHaveURL(/view=month/)

  const loaded = await request.get(`/api/v1/recipes/${recipe.id}`)
  const trashed = await request.delete(`/api/v1/recipes/${recipe.id}`, {
    headers: { "If-Match": loaded.headers().etag! },
  })
  expect(trashed.ok()).toBe(true)
  await page.goto(`/plan?view=agenda&date=${date}`)
  await expect(page.getByText(`${recipe.title} is in the trash.`)).toBeVisible()
  await expect(page.getByRole("link", { name: `Open ${recipe.title}` })).toHaveCount(0)
})

test("a calendar subscription serves the plan until it is revoked", async ({ page, request }) => {
  const date = localDate(1)
  const created = await request.post("/api/v1/meals", {
    data: { date, meal_type: "lunch", dishes: [{ display_name: "Subscription soup" }] },
  })
  expect(created.ok()).toBe(true)

  await page.goto(`/plan?date=${date}`)
  await page.getByRole("button", { name: "Calendar" }).click()
  await page.getByRole("menuitem", { name: "Calendar subscriptions" }).click()
  const dialog = page.getByRole("dialog", { name: "Calendar subscriptions" })
  await dialog.getByRole("textbox", { name: "Subscription name" }).fill("Kitchen wall")
  await dialog.getByRole("button", { name: "Create subscription" }).click()
  const address = await dialog.getByRole("textbox", { name: "Kitchen wall" }).inputValue()

  const feed = await request.get(new URL(address).pathname)
  expect(feed.ok()).toBe(true)
  expect(await feed.text()).toContain("SUMMARY:Lunch: Subscription soup")

  await dialog.getByRole("button", { name: "Revoke" }).click()
  await page
    .getByRole("dialog", { name: "Revoke this subscription?" })
    .getByRole("button", { name: "Revoke" })
    .click()
  await expect(dialog.getByText("No subscriptions yet.")).toBeVisible()
  expect((await request.get(new URL(address).pathname)).status()).toBe(404)
})

test("shopping items group by section, combine, and record who changed them", async ({
  page,
  request,
}) => {
  const suffix = Math.random().toString(36).slice(2, 10)
  const list = `e2e-${suffix}`
  const createdList = await request.post("/api/v1/shopping/lists", {
    data: { id: list, name: `Market ${suffix}` },
  })
  expect(createdList.ok()).toBe(true)
  for (const [index, [name, category]] of [
    ["Diced onion", "Produce"],
    ["Sliced onion", "Produce"],
    ["Milk", "Dairy"],
  ].entries()) {
    const added = await request.post(`/api/v1/shopping/lists/${list}/changes`, {
      data: {
        id: `add-${suffix}-${index}`,
        kind: "add",
        item_id: `item-${index}`,
        base_revision: 0,
        name,
      },
    })
    expect(added.ok()).toBe(true)
    const body = (await added.json()) as { revision: number }
    const categorised = await request.post(`/api/v1/shopping/lists/${list}/changes`, {
      data: {
        id: `cat-${suffix}-${index}`,
        kind: "edit",
        item_id: `item-${index}`,
        base_revision: body.revision,
        category,
      },
    })
    expect(categorised.ok()).toBe(true)
  }

  await page.goto("/shopping")
  await page.getByRole("heading", { level: 1 }).getByRole("button").click()
  await page.getByRole("menuitemradio", { name: `Market ${suffix}` }).click()
  await expect(page.getByRole("heading", { name: "Produce" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Dairy" })).toBeVisible()

  await page.getByRole("button", { name: "Arrange items" }).click()
  await page.getByRole("button", { name: "Move Sliced onion up", exact: true }).click()
  await expect(page.getByRole("button", { name: /^(Diced|Sliced) onion$/ })).toHaveText([
    "Sliced onion",
    "Diced onion",
  ])
  await page.getByRole("checkbox", { name: "Select Diced onion" }).check()
  await page.getByRole("checkbox", { name: "Select Sliced onion" }).check()
  await page.getByRole("button", { name: "Combine", exact: true }).click()
  await expect(page.getByRole("button", { name: /^Sliced onion/ })).toHaveCount(0)
  await expect(page.getByRole("dialog")).toHaveCount(0)
  await page.getByRole("button", { name: "Done", exact: true }).click()

  await page.getByRole("button", { name: "Activity" }).click()
  await expect(page.getByText(/combined Sliced onion, Diced onion/)).toBeVisible()
})

test("meal planning starts with recipes and saves the selected dish", async ({ page, request }) => {
  const recipe = await seedRecipe(request, "Recipe-first planner soup")
  await page.goto("/plan")
  await page.getByRole("button", { name: "Plan a meal", exact: true }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog.getByRole("button", { name: "Recipes", exact: true })).toHaveAttribute(
    "aria-pressed",
    "true",
  )
  await expect(dialog.getByRole("button", { name: "Save meal", exact: true })).toBeDisabled()
  await dialog.getByRole("textbox", { name: "Find a recipe or saved meal" }).fill(recipe.title)
  await dialog.getByRole("button", { name: recipe.title, exact: true }).click()
  await expect(dialog.getByRole("textbox", { name: "Dish name" })).toHaveValue(recipe.title)
  await expect(dialog.getByRole("button", { name: "Add a dish", exact: true })).toHaveAttribute(
    "aria-expanded",
    "false",
  )
  await dialog.getByRole("button", { name: "Save meal", exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(
    page
      .getByRole("region", { name: "Unscheduled", exact: true })
      .getByRole("link", { name: `Open ${recipe.title}` }),
  ).toBeVisible()
})

test("phone recipes expose cooking and section jumps before the photograph", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Phone cooking soup")
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto(`/recipes/${recipe.id}`)
  const cook = page.getByRole("button", { name: "Cook", exact: true })
  await expect(cook).toBeInViewport()
  await page.getByRole("link", { name: "Ingredients", exact: true }).click()
  await expect(page.getByRole("heading", { name: "Ingredients", exact: true })).toBeInViewport()
  await page.getByRole("link", { name: "Instructions", exact: true }).click()
  await expect(page.getByRole("heading", { name: "Instructions", exact: true })).toBeInViewport()
})
