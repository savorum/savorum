import { expect, test } from "@playwright/test"

test("shared links use normal import review and require an explicit commit", async ({
  page,
  request,
}) => {
  // Supply a real held preview so the final commit exercises the normal job API.
  await page.goto("/")
  const pending = page.waitForResponse((response) =>
    response.url().endsWith("/api/v1/recipes/import-preview"),
  )
  await page.locator('input[type="file"]').setInputFiles("e2e/fixtures/capture-recipe.json")
  const payload = await (await pending).text()
  let previews = 0
  let commits = 0
  page.on("request", (req) => {
    if (req.method() === "POST" && req.url().endsWith("/jobs")) commits += 1
  })
  await page.route("**/api/v1/recipes/import-url/preview", async (route) => {
    previews += 1
    expect(route.request().postDataJSON()).toMatchObject({ url: "https://recipes.example/carrot" })
    await route.fulfill({ contentType: "application/json", body: payload })
  })
  await page.goto(
    `/capture?text=${encodeURIComponent("Carrot soup https://recipes.example/carrot")}`,
  )
  const url = page.getByRole("textbox", { name: "Recipe URL" })
  await expect(url).toHaveValue("https://recipes.example/carrot")
  await url.focus()
  await url.press("End")
  await url.pressSequentially("/soup")
  await expect(url).toHaveValue("https://recipes.example/carrot/soup")
  for (let index = 0; index < 5; index += 1) await url.press("Backspace")
  await expect(url).toHaveValue("https://recipes.example/carrot")
  await expect(url).toBeFocused()
  expect(previews).toBe(0)
  expect(commits).toBe(0)
  await page.getByRole("button", { name: "Preview recipe" }).click()
  const review = page.getByRole("region", { name: "Save this recipe" })
  await expect(review).toContainText("Captured carrot soup")
  expect(previews).toBe(1)
  expect(commits).toBe(0)
  await review.getByRole("button", { name: "Save recipe", exact: true }).click()
  await expect(page.getByRole("heading", { name: "1 recipe imported" })).toBeVisible()
  expect(commits).toBe(1)
  await expect(page.getByRole("link", { name: "Captured carrot soup" })).toBeVisible()

  const manifest = await request.get("/manifest.webmanifest")
  expect(manifest.ok()).toBe(true)
  expect(manifest.headers()["content-type"]).toContain("manifest")
  expect((await manifest.json()).share_target).toMatchObject({
    action: "/capture",
    method: "GET",
    params: { url: "url", text: "text" },
  })
})

test("capture rejects executable URLs and offers a token-free bookmarklet", async ({ page }) => {
  await page.goto("/capture?url=javascript%3Aalert(1)")
  await expect(page.getByRole("textbox", { name: "Recipe URL" })).toHaveValue("")
  await page.getByRole("textbox", { name: "Recipe URL" }).fill("javascript:alert(1)")
  await page.getByRole("button", { name: "Preview recipe" }).click()
  await expect(page.getByRole("alert")).toContainText("Enter a recipe link")
  await page.getByText("Save recipes from your browser", { exact: true }).click()
  const bookmarklet = await page
    .getByRole("link", { name: "Save with Savorum" })
    .getAttribute("href")
  expect(bookmarklet).toContain("encodeURIComponent(location.href)")
})
