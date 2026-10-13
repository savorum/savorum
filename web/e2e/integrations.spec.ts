import { expect, test } from "@playwright/test"

import { seedRecipe } from "./library"

test("live events refresh recipes and pantry while preserving a pantry draft", async ({
  page,
  request,
}) => {
  await page.goto("/")
  const connected = page.waitForResponse(
    (response) => response.url().includes("/api/v1/events") && response.status() === 200,
  )
  await page.reload()
  await connected
  const title = `Live soup ${Date.now()}`
  await seedRecipe(request, title)
  await expect(page.getByRole("heading", { name: title, exact: true })).toBeVisible()

  await page.goto("/shopping")
  await page.getByRole("button", { name: "Pantry", exact: true }).click()
  await page.getByRole("button", { name: "Add ingredient", exact: true }).click()
  const name = page.getByRole("textbox", { name: "Ingredient", exact: true })
  await name.fill("Café rice")
  await name.focus()
  const id = `live-pantry-${Date.now()}`
  const saved = await request.put(`/api/v1/pantry/${id}`, {
    headers: { "If-None-Match": "*" },
    data: { name: "Shared lentils", amount: "about half a bag" },
  })
  expect(saved.ok()).toBe(true)
  await expect(page.getByText("Shared lentils about half a bag", { exact: true })).toBeVisible()
  await expect(name).toHaveValue("Café rice")
  await expect(name).toBeFocused()
  await name.pressSequentially(" for dinner")
  await expect(name).toHaveValue("Café rice for dinner")
})

test("webhook settings expose a secret once and retain edits after a revision conflict", async ({
  page,
  request,
}) => {
  await page.goto("/account/integrations")
  await page.getByRole("button", { name: "Add subscription", exact: true }).click()
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("Kitchen receiver")
  await page
    .getByRole("textbox", { name: "Destination URL", exact: true })
    .fill("https://93.184.216.34/events")
  await page.getByRole("checkbox", { name: "recipe.updated", exact: true }).check()
  const created = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/v1/webhooks") && response.request().method() === "POST",
  )
  await page.getByRole("button", { name: "Save", exact: true }).click()
  const response = await created
  expect(response.status()).toBe(201)
  const grant = await response.json()
  await expect(page.getByRole("textbox", { name: "Signing secret", exact: true })).toHaveValue(
    grant.secret,
  )
  const listed = await request.get("/api/v1/webhooks")
  expect(await listed.text()).not.toContain(grant.secret)

  await page.getByRole("button", { name: "Edit", exact: true }).click()
  const name = page.getByRole("textbox", { name: "Name", exact: true })
  await name.fill("My unsaved receiver name")
  const changed = await request.put(`/api/v1/webhooks/${grant.subscription.id}`, {
    headers: { "If-Match": `"${grant.subscription.revision}"` },
    data: {
      name: "Another owner's name",
      url: grant.subscription.url,
      event_types: ["recipe.updated"],
      enabled: false,
    },
  })
  expect(changed.ok()).toBe(true)
  const conflict = page.waitForResponse(
    (value) =>
      value.request().method() === "PUT" &&
      value.url().includes("/webhooks/") &&
      value.status() === 412,
  )
  await page.getByRole("button", { name: "Save", exact: true }).click()
  await conflict
  await expect(name).toHaveValue("My unsaved receiver name")
  await expect(page.getByRole("alert")).toBeVisible()
  const fresh = await changed.json()
  expect(
    (
      await request.delete(`/api/v1/webhooks/${fresh.id}`, {
        headers: { "If-Match": `"${fresh.revision}"` },
      })
    ).status(),
  ).toBe(204)
})
