import { type APIRequestContext, type Locator, type Page, expect, test } from "@playwright/test"

import { choose } from "./controls"
import { seedRecipe } from "./library"

const arabicRecipe = {
  language: "ar",
  description: "شاي أخضر بالنعناع الطازج، يُقدَّم ساخنًا.",
  yields: [{ quantity: { kind: "simple", amount: { value: "4" }, unit: "count.serving" } }],
  times: { prep: { value: "PT5M" }, cook: { value: "PT5M" } },
  notes: ["يمكن استبدال السكر بالعسل. See https://example.com/tea for the source."],
  ingredient_sections: [
    {
      id: "tea",
      ingredients: [
        {
          id: "green-tea",
          name: "شاي أخضر صيني (Gunpowder)",
          quantity: { kind: "simple", amount: { value: "10" }, unit: "mass.gram" },
        },
        {
          id: "water",
          name: "ماء مغلي",
          quantity: { kind: "simple", amount: { value: "750" }, unit: "volume.milliliter" },
        },
      ],
    },
  ],
  instruction_sections: [
    {
      id: "method",
      steps: [
        { id: "rinse", text: "اشطف الشاي بقليل من الماء المغلي." },
        { id: "steep", text: "أضف النعناع واتركه 4–5 دقائق." },
      ],
    },
  ],
}

async function setInterfaceLocale(request: APIRequestContext, locale: string) {
  const current = await (await request.get("/api/v1/me/preferences")).json()
  const response = await request.put("/api/v1/me/preferences", {
    data: { revision: current.revision, overrides: { ...current.overrides, ui_locale: locale } },
  })
  expect(response.ok()).toBe(true)
}

async function box(locator: Locator) {
  const value = await locator.boundingBox()
  if (!value) throw new Error("element is not laid out")
  return value
}

async function direction(page: Page, selector: string) {
  return page
    .locator(selector)
    .first()
    .evaluate((node) => getComputedStyle(node).direction)
}

test.afterEach(async ({ request }) => {
  await setInterfaceLocale(request, "")
})

test("an Arabic recipe reads right to left inside an English interface", async ({
  page,
  request,
}, testInfo) => {
  const recipe = await seedRecipe(request, "شاي بالنعناع", arabicRecipe)
  await page.goto(`/recipes/${recipe.id}`)
  const title = page.getByRole("heading", { level: 1, name: "شاي بالنعناع" })
  await expect(title).toBeVisible()

  expect(await page.evaluate(() => document.documentElement.dir)).toBe("ltr")
  expect(await title.evaluate((node) => getComputedStyle(node).direction)).toBe("rtl")
  // The interface around the recipe keeps its own direction.
  expect(await direction(page, "header")).toBe("ltr")
  const back = page.getByRole("link", { name: "Recipes" }).first()
  const main = await box(page.locator("main"))
  expect((await box(back)).x).toBeLessThan(main.x + main.width / 2)
  // The recipe's title starts at the right.
  const heading = await box(title)
  expect(heading.x + heading.width).toBeGreaterThan(main.x + main.width / 2)

  // A Latin amount and a Latin word inside Arabic keep their own order.
  const ingredient = page.locator("#ingredient-green-tea")
  await expect(ingredient).toContainText("10 g")
  await expect(ingredient).toContainText("Gunpowder")

  await testInfo.attach("arabic-recipe-english-interface", {
    body: await page.screenshot({ fullPage: true }),
    contentType: "image/png",
  })
})

test("an Arabic interface mirrors the shell and keeps English recipes left to right", async ({
  page,
  request,
}, testInfo) => {
  const recipe = await seedRecipe(request, "Weeknight tikka")
  await setInterfaceLocale(request, "ar")
  await page.goto(`/recipes/${recipe.id}`)
  await expect(page.getByRole("heading", { level: 1, name: "Weeknight tikka" })).toBeVisible()

  expect(await page.evaluate(() => document.documentElement.dir)).toBe("rtl")
  expect(await page.evaluate(() => document.documentElement.lang)).toBe("ar")
  const title = page.getByRole("heading", { level: 1, name: "Weeknight tikka" })
  expect(await title.evaluate((node) => getComputedStyle(node).direction)).toBe("ltr")

  // A backward arrow points toward the end it leads to.
  const icon = page.locator("main a svg, main button svg").first()
  const transform = await icon.evaluate((node) => getComputedStyle(node).transform)
  expect(transform).toContain("matrix(-1")

  // Typed text finds its own direction in either interface direction.
  await page.goto(`/recipes/${recipe.id}/edit`)
  const input = page.locator("input[dir=auto]").first()
  await expect(input).toBeVisible()

  await testInfo.attach("english-recipe-arabic-interface", {
    body: await page.screenshot({ fullPage: true }),
    contentType: "image/png",
  })
})

test("translations are separate recipes linked explicitly and grouped in the library", async ({
  page,
  request,
}) => {
  const english = await seedRecipe(request, "Mint tea for translation")
  const arabic = await seedRecipe(request, "شاي بالنعناع للترجمة", arabicRecipe)

  await page.goto(`/recipes/${english.id}`)
  await page.getByRole("button", { name: "More actions" }).click()
  await page.getByRole("menuitem", { name: "Translations" }).click()
  const dialog = page.getByRole("dialog", { name: "Translations" })
  await dialog.getByRole("searchbox", { name: "Find a recipe in this library" }).fill("للترجمة")
  await dialog.getByRole("button", { name: /شاي بالنعناع للترجمة/ }).click()
  await dialog.getByRole("button", { name: "Link translation" }).click()
  await expect(dialog.getByRole("button", { name: "Unlink" })).toBeVisible()
  await dialog.getByRole("button", { name: "Done" }).click()

  // Each language is its own address; choosing one navigates to it.
  const languages = page.getByRole("navigation", { name: "Languages" })
  await expect(languages.getByText("English")).toHaveAttribute("aria-current", "page")
  await languages.getByRole("link", { name: "العربية" }).click()
  await expect(page).toHaveURL(new RegExp(`/recipes/${arabic.id}$`))
  await expect(page.getByRole("heading", { level: 1, name: "شاي بالنعناع للترجمة" })).toBeVisible()

  // Linking did not change the Arabic recipe's content.
  const stored = await (await request.get(`/api/v1/recipes/${arabic.id}`)).json()
  expect(stored.recipe.ingredient_sections).toEqual(arabicRecipe.ingredient_sections)

  // Browsing the library shows the pair once.
  await page.goto("/?q=translation")
  await expect(page.getByRole("link", { name: /Mint tea for translation/ })).toBeVisible()
  await page.goto("/")
  const cards = page.getByRole("link", { name: /Mint tea for translation|شاي بالنعناع للترجمة/ })
  await expect(cards).toHaveCount(1)
  await expect(cards).toContainText("also in")

  await page.goto(`/recipes/${english.id}`)
  await page.getByRole("button", { name: "More actions" }).click()
  await page.getByRole("menuitem", { name: "Translations" }).click()
  await page
    .getByRole("dialog", { name: "Translations" })
    .getByRole("button", { name: "Unlink" })
    .click()
  await expect(page.getByRole("dialog").getByText("No translations are linked yet.")).toBeVisible()
})

test("a new translation starts from an unbound copy and is saved as its own recipe", async ({
  page,
  request,
}) => {
  const english = await seedRecipe(request, "Tikka to translate")
  await page.goto(`/recipes/${english.id}`)
  await page.getByRole("button", { name: "More actions" }).click()
  await page.getByRole("menuitem", { name: "Translations" }).click()
  const dialog = page.getByRole("dialog", { name: "Translations" })
  await choose(dialog.getByRole("combobox", { name: "Language of the translation" }), "fr")
  await dialog.getByRole("button", { name: "Write translation" }).click()

  await expect(page).toHaveURL(/\/translate\?/)
  const title = page.getByRole("textbox", { name: "Title" })
  await title.fill("Tikka à traduire")
  await page.getByRole("button", { name: "Save translation" }).click()
  await expect(page.getByRole("heading", { level: 1, name: "Tikka à traduire" })).toBeVisible()
  const translated = page.url().split("/recipes/")[1]
  expect(translated).not.toBe(english.id)

  const original = await (await request.get(`/api/v1/recipes/${english.id}`)).json()
  expect(original.recipe.title).toBe("Tikka to translate")
  expect(original.recipe.translations[0].relation).toBe("translation")
})
