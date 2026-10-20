import { expect, test } from "@playwright/test"

import { seedBook, seedRecipe } from "./library"

test("filter search keeps focus through typing, composition and selection; saving belongs to the query", async ({
  page,
  request,
}) => {
  await seedRecipe(request, "Filter interaction supper")
  for (let index = 0; index < 15; index++) {
    expect(
      (await request.post("/api/v1/tags", { data: { name: `Filter example ${index}` } })).ok(),
    ).toBeTruthy()
  }
  await page.goto("/")
  const filters = page.getByRole("button", { name: /^Filters/ })
  await filters.click()
  const picker = page.getByRole("button", { name: "Choose a tag", exact: true })
  await picker.click()
  const search = page.getByRole("searchbox", { name: "Search tags" })
  const original = await search.elementHandle()
  await search.pressSequentially("Filter example")
  await expect(search).toHaveValue("Filter example")
  await expect(search).toBeFocused()
  await search.press("Home")
  await search.pressSequentially("A")
  await expect(search).toHaveValue("AFilter example")
  expect(await search.evaluate((input: HTMLInputElement) => input.selectionStart)).toBe(1)
  const session = await page.context().newCDPSession(page)
  await session.send("Input.imeSetComposition", {
    text: "週末",
    selectionStart: 2,
    selectionEnd: 2,
  })
  await expect(search).toHaveValue("A週末Filter example")
  await session.send("Input.insertText", { text: "週末" })
  await expect(search).toBeFocused()
  expect(
    await original?.evaluate((node) => node.isConnected && node === document.activeElement),
  ).toBe(true)
  await search.fill("Filter example 1")
  const choice = page.getByRole("checkbox", { name: "Filter example 1", exact: true })
  await choice.focus()
  await choice.press("Space")
  await expect(choice).toBeChecked()
  await expect(choice).toBeFocused()
  await page.getByRole("button", { name: "Done", exact: true }).click()
  await expect(picker).toBeFocused()
  await expect(picker).toHaveAccessibleDescription("Filter example 1")
  await filters.click()
  const save = page.getByRole("button", { name: "Save this search", exact: true })
  await expect(save).toBeVisible()
  const chip = page.getByRole("button", { name: "Filter example 1", exact: true })
  const chipBox = await chip.boundingBox()
  const saveBox = await save.boundingBox()
  expect(Math.abs((chipBox?.y ?? 0) - (saveBox?.y ?? 0))).toBeLessThan(10)
  await chip.focus()
  await chip.press("Enter")
  await expect(filters).toBeFocused()
})

test("a filter can search for a book beyond the first suggestion page", async ({
  page,
  request,
}) => {
  await seedRecipe(request, "Book search supper")
  for (let index = 0; index < 12; index++) await seedBook(request, `A filter book ${index}`, [])
  await seedBook(request, "Zebra weekend book", [])
  await page.goto("/")
  await page.getByRole("button", { name: /^Filters/ }).click()
  await page.getByRole("button", { name: "Choose a book", exact: true }).click()
  await page.getByRole("searchbox", { name: "Search books" }).pressSequentially("Zebra weekend")
  const book = page.getByRole("checkbox", { name: "Zebra weekend book", exact: true })
  await book.check()
  await expect(book).toBeChecked()
  await page.keyboard.press("Escape")
  await expect(page.getByRole("button", { name: "Choose a book", exact: true })).toBeFocused()
})

for (const width of [320, 390]) {
  test(`library controls stay grouped at ${width}px`, async ({ page, request }) => {
    const title = `Mobile filter supper ${width} Rahmschwammerln`
    await seedRecipe(request, title)
    await page.setViewportSize({ width, height: 844 })
    await page.goto("/")
    const search = page.getByRole("combobox", { name: "Search recipes" })
    await search.fill(title)
    await expect(page.getByText("1 recipe", { exact: true })).toBeVisible()
    const filters = page.getByRole("button", { name: /^Filters/ })
    const sort = page.getByRole("combobox", { name: "Sort recipes" })
    const select = page.getByRole("button", { name: "Select", exact: true })
    const save = page.getByRole("button", { name: "Save this search", exact: true })
    await expect(save).toHaveText("Save", { useInnerText: true })
    const filterBounds = (await filters.boundingBox())!
    const sortBounds = (await sort.boundingBox())!
    const countBounds = (await page.getByText("1 recipe", { exact: true }).boundingBox())!
    const selectBounds = (await select.boundingBox())!
    const saveBounds = (await save.boundingBox())!
    expect(Math.abs(filterBounds.y - sortBounds.y)).toBeLessThan(2)
    expect(
      Math.abs(countBounds.y + countBounds.height / 2 - selectBounds.y - selectBounds.height / 2),
    ).toBeLessThan(2)
    expect(saveBounds.y).toBeLessThan(selectBounds.y)
    expect(saveBounds.x).toBeLessThan(filterBounds.x + 12)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
      width,
    )
    const original = await search.elementHandle()
    await search.focus()
    await search.pressSequentially(" again")
    await expect(search).toHaveValue(`${title} again`)
    await expect(search).toBeFocused()
    expect(await original?.evaluate((node) => node === document.activeElement)).toBe(true)
    await search.fill(title)
    await expect(page.getByText("1 recipe", { exact: true })).toBeVisible()
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%"
    })
    const enlargedFilter = (await filters.boundingBox())!
    const enlargedSort = (await sort.boundingBox())!
    expect(enlargedSort.y).toBeGreaterThanOrEqual(enlargedFilter.y + enlargedFilter.height)
    expect(enlargedSort.width).toBeCloseTo((await search.boundingBox())!.width, 0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
      width,
    )
    await expect(sort.locator("[data-part=value-text]")).toHaveCSS("white-space", "normal")
  })
}
