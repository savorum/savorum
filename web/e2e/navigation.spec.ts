import { expect, test } from "@playwright/test"

import { seedRecipe } from "./library"

test("reader Recipes returns through editor history to the exact library view", async ({
  page,
  request,
}) => {
  const recipes = await Promise.all(
    Array.from({ length: 50 }, (_, index) =>
      seedRecipe(request, `Return path recipe ${String(index + 1).padStart(2, "0")}`),
    ),
  )
  const target = recipes[40]!
  await page.goto("/?q=Return%20path&sort=title-asc")
  const targetCard = page.getByRole("link", { name: new RegExp(target.title) })
  await expect(page.locator("#library-results > ul > li")).toHaveCount(36)
  await page.locator("#library-continuation").scrollIntoViewIfNeeded()
  await expect(targetCard).toBeVisible()
  await targetCard.scrollIntoViewIfNeeded()
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())))
  const libraryScroll = await page.evaluate(() => window.scrollY)
  expect(libraryScroll).toBeGreaterThan(0)

  await targetCard.click()
  await page.getByRole("link", { name: "Edit" }).click()
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page.getByRole("heading", { name: target.title })).toBeVisible()
  await page.getByRole("button", { name: "Recipes" }).click()

  await expect(page).toHaveURL(/\/?q=Return(?:%20|\+)path&sort=title-asc&shown=72$/)
  await expect(page.getByRole("link", { name: new RegExp(target.title) })).toBeVisible()
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(libraryScroll)
})

test("offline continuation preserves the library address and retries from the footer", async ({
  page,
  request,
  context,
}) => {
  await Promise.all(
    Array.from({ length: 50 }, (_, index) =>
      seedRecipe(request, `Offline browsing recipe ${String(index + 1).padStart(2, "0")}`),
    ),
  )
  await page.goto("/?q=Offline%20browsing&sort=title-asc")
  const cards = page.locator("#library-results > ul > li")
  await expect(cards).toHaveCount(36)
  const address = page.url()
  await context.setOffline(true)
  await page.getByRole("button", { name: "Load more recipes" }).click()
  const footer = page.locator("#library-continuation")
  await expect(footer.getByRole("alert")).toBeVisible()
  await expect(page.getByRole("alert")).toHaveCount(1)
  await expect(cards).toHaveCount(36)
  await expect(page).toHaveURL(address)
  await context.setOffline(false)
  await footer.getByRole("button", { name: "Try again" }).click()
  await expect(cards).toHaveCount(50)
  await expect(page).toHaveURL(/shown=72$/)
  await expect(page.getByRole("alert")).toHaveCount(0)
})

test("reader Recipes returns to the same library view after a cooking detour", async ({
  page,
  request,
}) => {
  const recipes = await Promise.all(
    Array.from({ length: 26 }, (_, index) =>
      seedRecipe(request, `Cooking return recipe ${String(index + 1).padStart(2, "0")}`),
    ),
  )
  const target = recipes[20]!
  await page.goto("/?q=Cooking%20return&sort=title-asc")
  const targetCard = page.getByRole("link", { name: new RegExp(target.title) })
  await targetCard.scrollIntoViewIfNeeded()
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())))
  const libraryScroll = await page.evaluate(() => window.scrollY)
  expect(libraryScroll).toBeGreaterThan(0)

  await targetCard.click()
  await page.getByRole("button", { name: "Cook", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${target.id}/cook`))
  await page.getByRole("button", { name: "Exit cooking", exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${target.id}$`))

  await page.getByRole("button", { name: "Recipes" }).click()
  await expect(page).toHaveURL(/\/?q=Cooking(?:%20|\+)return&sort=title-asc$/)
  await expect(page.getByRole("link", { name: new RegExp(target.title) })).toBeVisible()
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(libraryScroll)
})

test("reader Recipes uses the library route after direct entry", async ({ page, request }) => {
  const recipe = await seedRecipe(request, "Direct reader return recipe")
  await page.goto(`/recipes/${recipe.id}`)

  await page.locator("#main-content").getByRole("link", { name: "Recipes" }).click()

  await expect(page).toHaveURL("/")
  await expect(page.getByRole("heading", { name: "Recipes" })).toBeVisible()
})
