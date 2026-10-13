import { type Page, expect, test } from "@playwright/test"

import { seedRecipe } from "./library"

const star = (page: Page, value: number) =>
  page
    .getByRole("radiogroup", { name: "Your rating" })
    .getByRole("radio", { name: `${value} of 5` })

test.describe("recipe mark controls", () => {
  test("the heart toggles favoriting and updates persisted state", async ({ page, request }) => {
    const recipe = await seedRecipe(request, "Marked aubergine supper")
    await page.goto(`/recipes/${recipe.id}`)

    const addHeart = page.getByRole("button", { name: "Add to favorites" })
    await expect(addHeart).toBeVisible()
    await addHeart.click()

    const removeHeart = page.getByRole("button", { name: "Remove from favorites" })
    await expect(removeHeart).toBeVisible()
    await expect
      .poll(
        async () =>
          (await (await request.get(`/api/v1/recipes/${recipe.id}/organization`)).json()).user_state
            ?.favorite,
      )
      .toBe(true)

    await removeHeart.click()
    await expect(addHeart).toBeVisible()
    await expect
      .poll(
        async () =>
          (await (await request.get(`/api/v1/recipes/${recipe.id}/organization`)).json()).user_state
            ?.favorite,
      )
      .toBe(false)
  })

  test("the stars set rating and update persisted state", async ({ page, request }) => {
    const recipe = await seedRecipe(request, "Rated aubergine supper")
    await page.goto(`/recipes/${recipe.id}`)

    await expect(star(page, 3)).toBeVisible()
    await star(page, 3).click()
    await expect(star(page, 3)).toHaveAttribute("aria-checked", "true")

    await expect
      .poll(
        async () =>
          (await (await request.get(`/api/v1/recipes/${recipe.id}/organization`)).json()).user_state
            ?.rating,
      )
      .toBe(3)
  })

  test("rating supports keyboard selection", async ({ page, request }) => {
    const recipe = await seedRecipe(request, "Keyboard rated supper")
    await page.goto(`/recipes/${recipe.id}`)
    await expect(star(page, 1)).toHaveAttribute("aria-checked", "false")
    await star(page, 1).focus()
    await page.keyboard.press("Space")
    await expect(star(page, 1)).toHaveAttribute("aria-checked", "true")
    await expect(star(page, 1)).toHaveAttribute("aria-disabled", "false")
    await page.keyboard.press("ArrowRight")
    await expect(star(page, 2)).toHaveAttribute("aria-checked", "true")
    await expect
      .poll(
        async () =>
          (await (await request.get(`/api/v1/recipes/${recipe.id}/organization`)).json()).user_state
            ?.rating,
      )
      .toBe(2)
  })
})
