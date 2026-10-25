import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "@playwright/test"

import { settleAnimations } from "./controls"
import { seedRecipe } from "./library"

test("recipe shortcuts use existing actions and yield to typing, modifiers and dialogs", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Shortcut supper")
  await page.goto(`/recipes/${recipe.id}`)
  await expect(page.getByRole("heading", { name: recipe.title, level: 1 })).toBeVisible()
  await page.dispatchEvent("body", "keydown", { key: "e", ctrlKey: true })
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  await page.keyboard.press("?")
  const dialog = page.getByRole("dialog", { name: "Keyboard shortcuts" })
  await expect(dialog).toBeVisible()
  await page.keyboard.press("e")
  await expect(dialog).toBeVisible()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  await page.keyboard.press("Escape")
  await expect(dialog).toBeHidden()
  await page.keyboard.press("e")
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}/edit$`))
  const title = page.getByRole("textbox", { name: "Title", exact: true })
  await expect(title).toHaveValue(recipe.title)
  await title.fill("Peppers")
  await expect(title).toHaveValue("Peppers")
  await title.pressSequentially(" and cheese")
  await expect(title).toHaveValue("Peppers and cheese")
  await expect(title).toBeFocused()
  await page.goto(`/recipes/${recipe.id}`)
  await expect(page.getByRole("link", { name: "Print", exact: true })).toBeVisible()
  await page.keyboard.press("p")
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}/print`))
  await page.goto(`/recipes/${recipe.id}`)
  await expect(page.getByRole("button", { name: "Cook", exact: true })).toBeVisible()
  await page.keyboard.press("c")
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}/cook`))
})

test("shortcuts switch off persistently while menus and ordinary controls remain usable", async ({
  page,
  request,
}) => {
  const recipe = await seedRecipe(request, "Shortcut preference supper")
  await page.goto(`/recipes/${recipe.id}`)
  await expect(page.getByRole("heading", { name: recipe.title, level: 1 })).toBeVisible()
  await page.keyboard.press("?")
  const dialog = page.getByRole("dialog", { name: "Keyboard shortcuts" })
  const toggle = dialog.getByRole("checkbox", { name: "Enable shortcuts" })
  await expect(toggle).toBeChecked()
  await toggle.focus()
  await page.keyboard.press("Space")
  await expect(toggle).not.toBeChecked()
  await expect(toggle).toBeFocused()
  await page.keyboard.press("Escape")
  await page.keyboard.press("e")
  await page.keyboard.press("?")
  await expect(dialog).toBeHidden()
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}$`))
  await expect(page.getByRole("link", { name: "Edit", exact: true })).not.toHaveAttribute(
    "aria-keyshortcuts",
  )
  await page.getByRole("button", { name: "Cook", exact: true }).click()
  const firstStep = page.getByRole("heading", { name: "Step 1 of 2", exact: true })
  await expect(firstStep).toBeVisible()
  await page.keyboard.press("j")
  await expect(firstStep).toBeVisible()
  await page.keyboard.press("ArrowRight")
  await expect(page.getByRole("heading", { name: "Step 2 of 2", exact: true })).toBeVisible()
  await page.goto(`/recipes/${recipe.id}`)
  await page.reload()
  await page.getByRole("button", { name: "More", exact: true }).first().click()
  await page.getByRole("menuitem", { name: "Keyboard shortcuts" }).click()
  await expect(toggle).not.toBeChecked()
  await toggle.focus()
  await page.keyboard.press("Space")
  await expect(toggle).toBeChecked()
  await expect(toggle).toBeFocused()
  await page.keyboard.press("Escape")
  await expect(page.getByRole("button", { name: "More", exact: true }).first()).toBeFocused()
  await page.keyboard.press("e")
  await expect(page).toHaveURL(new RegExp(`/recipes/${recipe.id}/edit$`))
})

test("library, plan and shopping shortcuts follow page context", async ({ page }) => {
  await page.goto("/")
  await expect(page.getByRole("combobox", { name: "Search recipes" })).toBeVisible()
  await page.keyboard.press("/")
  await expect(page.getByRole("combobox", { name: "Search recipes" })).toBeFocused()
  await page.keyboard.press("Tab")
  await page.keyboard.press("n")
  await expect(page.getByRole("menuitem", { name: "New recipe", exact: true })).toBeVisible()
  await page.keyboard.press("Escape")
  await page.goto("/plan?view=week&date=2026-09-14")
  await expect(page.getByRole("button", { name: "Plan a meal", exact: true })).toBeVisible()
  await page.keyboard.press("]")
  await expect(page).toHaveURL(/date=2026-09-21/)
  await page.keyboard.press("[")
  await expect(page).toHaveURL(/date=2026-09-14/)
  await page.keyboard.press("n")
  await expect(page.getByRole("dialog", { name: "Plan a meal" })).toBeVisible()
  await page.keyboard.press("Escape")
  await page.goto("/shopping")
  await expect(page.getByRole("textbox", { name: "Item name" })).toBeVisible()
  await page.keyboard.press("a")
  await expect(page.getByRole("textbox", { name: "Item name" })).toBeFocused()
  await page.keyboard.type("Tea")
  await expect(page.getByRole("textbox", { name: "Item name" })).toHaveValue("Tea")
})

for (const colorScheme of ["light", "dark"] as const) {
  test(`shortcuts and help icons are accessible in ${colorScheme} mode`, async ({
    page,
  }, testInfo) => {
    await page.emulateMedia({ colorScheme })
    await page.goto("/plan")
    await expect(page.getByRole("heading", { name: "Plan", level: 1 })).toBeVisible()
    await page.keyboard.press("?")
    const dialog = page.getByRole("dialog", { name: "Keyboard shortcuts" })
    await expect(dialog).toBeVisible()
    await settleAnimations(page)
    const lastShortcut = await dialog.getByText("Timers", { exact: true }).boundingBox()
    const done = await dialog.getByRole("button", { name: "Done", exact: true }).boundingBox()
    expect(lastShortcut).not.toBeNull()
    expect(done).not.toBeNull()
    expect(lastShortcut!.y + lastShortcut!.height).toBeLessThan(done!.y)
    expect(
      (
        await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
          .analyze()
      ).violations,
    ).toEqual([])
    await page.screenshot({ path: testInfo.outputPath(`shortcuts-${colorScheme}.png`) })
    await page.setViewportSize({ width: 390, height: 844 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)
    await page.screenshot({ path: testInfo.outputPath(`shortcuts-phone-${colorScheme}.png`) })
    await page.keyboard.press("Escape")
    for (const path of ["/plan", "/shopping", "/imports"]) {
      await page.goto(path)
      const help = page.getByRole("link", { name: /^Help / })
      await help.focus()
      await expect(page.getByRole("tooltip")).toBeVisible()
      await expect(help).toBeFocused()
      await expect(help).toHaveAttribute("href", /^https:\/\/docs\.savorum\.app\/guide\//)
      await settleAnimations(page)
      expect(
        (await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa"]).analyze())
          .violations,
      ).toEqual([])
      await page.screenshot({
        path: testInfo.outputPath(`help-${path.slice(1)}-${colorScheme}.png`),
      })
      await page.keyboard.press("Escape")
      await expect(page.getByRole("tooltip")).toBeHidden()
      await expect(help).toBeFocused()
    }
  })
}

test("contextual help has a 44px touch target", async ({ browser, baseURL }) => {
  const context = await browser.newContext({
    baseURL,
    hasTouch: true,
    viewport: { width: 390, height: 844 },
  })
  try {
    const page = await context.newPage()
    await page.goto("/shopping")
    const help = page.getByRole("link", { name: /^Help / })
    await expect(help).toBeVisible()
    const box = await help.boundingBox()
    expect(box?.width).toBeGreaterThanOrEqual(44)
    expect(box?.height).toBeGreaterThanOrEqual(44)
  } finally {
    await context.close()
  }
})
