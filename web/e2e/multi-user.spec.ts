import { spawn, execFile, type ChildProcess } from "node:child_process"
import { mkdtemp, rm, writeFile } from "node:fs/promises"
import { createServer } from "node:net"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"
import { promisify } from "node:util"

import AxeBuilder from "@axe-core/playwright"
import {
  expect,
  test,
  type APIRequestContext,
  type BrowserContext,
  type Page,
} from "@playwright/test"

import { choose, chooser } from "./controls"
import { seedRecipe } from "./library"

// Authentication runs against its own disposable instance so the local-mode
// suite continues to prove the ordinary one-user installation has no login.
let server: ChildProcess
let directory: string
let origin: string
let serverOutput = ""
const password = "browser fixture long password 2026"
async function unusedPort(): Promise<number> {
  const listener = createServer()
  await new Promise<void>((resolve) => listener.listen(0, "127.0.0.1", resolve))
  const address = listener.address()
  if (!address || typeof address === "string") throw new Error("Could not reserve test port")
  await new Promise<void>((resolve, reject) =>
    listener.close((error) => (error ? reject(error) : resolve())),
  )
  return address.port
}
test.beforeAll(async ({ playwright }) => {
  directory = await mkdtemp(join(tmpdir(), "savorum-auth-e2e-"))
  const database = join(directory, "library.db")
  const passwordFile = join(directory, "password")
  await writeFile(passwordFile, password, { mode: 0o600 })
  const binary = resolve("../bin/savorum")
  await promisify(execFile)(binary, [
    "user",
    "create-admin",
    "--database",
    database,
    "--login",
    "owner",
    "--display-name",
    "Owner",
    "--password-file",
    passwordFile,
  ])
  const port = await unusedPort()
  origin = `http://127.0.0.1:${port}`
  server = spawn(
    binary,
    ["serve", "--database", database, "--listen", `127.0.0.1:${port}`, "--auth-enabled"],
    { stdio: ["ignore", "pipe", "pipe"] },
  )
  for (const stream of [server.stdout, server.stderr])
    stream?.on("data", (data) => {
      serverOutput = (serverOutput + String(data)).slice(-8000)
    })
  const request = await playwright.request.newContext({ baseURL: origin })
  try {
    await expect
      .poll(async () => {
        if (server.exitCode !== null)
          throw new Error(`Authentication server stopped: ${serverOutput}`)
        return (await request.get("/api/v1/health").catch(() => null))?.status()
      })
      .toBe(200)
  } finally {
    await request.dispose()
  }
})
test.afterAll(async () => {
  if (server && server.exitCode === null) {
    server.kill("SIGTERM")
    await new Promise<void>((resolve) => server.once("exit", () => resolve()))
  }
  if (directory) await rm(directory, { recursive: true, force: true })
})
async function login(page: Page, username: string) {
  await page.goto(`${origin}/`)
  await page.getByLabel("Login name", { exact: true }).pressSequentially(username)
  await page.getByLabel("Password", { exact: true }).fill(password)
  await page.getByRole("button", { name: "Sign in", exact: true }).click()
  await expect(page.getByRole("combobox", { name: "Search recipes" })).toBeVisible()
}
async function expectAccessible(page: Page) {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze()
  expect(results.violations).toEqual([])
}
async function expectFieldsAddressTheirOwnControls(page: Page, surface: string) {
  const problems = await page.evaluate(() => {
    const seen = new Map<string, number>()
    for (const element of document.querySelectorAll("[id]"))
      seen.set(element.id, (seen.get(element.id) ?? 0) + 1)
    const targets = new Map<string, string[]>()
    for (const label of document.querySelectorAll<HTMLLabelElement>("label[for]")) {
      const key = label.getAttribute("for")!
      targets.set(key, [...(targets.get(key) ?? []), label.textContent?.trim() ?? ""])
    }
    return {
      duplicateIDs: [...seen].filter(([, count]) => count > 1).map(([id]) => id),
      sharedControls: [...targets].filter(([, labels]) => labels.length > 1),
    }
  })
  expect(problems, `${surface} has fields that do not address their own control`).toEqual({
    duplicateIDs: [],
    sharedControls: [],
  })
}
async function csrf(context: BrowserContext): Promise<string> {
  const response = await context.request.get(`${origin}/api/v1/account`)
  expect(response.ok()).toBe(true)
  return (await response.json()).csrf_token as string
}
async function mutate(
  request: APIRequestContext,
  token: string,
  path: string,
  method: string,
  data?: unknown,
) {
  return request.fetch(`${origin}/api/v1${path}`, {
    method,
    headers: { "X-CSRF-Token": token },
    data,
  })
}
test("real browser principals enforce roles, isolate libraries and keep preferences personal", async ({
  browser,
  playwright,
}) => {
  const ownerContext = await browser.newContext({ baseURL: origin })
  const viewerContext = await browser.newContext({ baseURL: origin })
  const editorContext = await browser.newContext({ baseURL: origin })
  try {
    const owner = await ownerContext.newPage()
    await login(owner, "owner")
    const ownerToken = await csrf(ownerContext)
    const account = await (await ownerContext.request.get(`${origin}/api/v1/account`)).json()
    const home = account.active_library_id as string
    const ownerRequest = await playwright.request.newContext({
      baseURL: origin,
      storageState: await ownerContext.storageState(),
      extraHTTPHeaders: { "X-CSRF-Token": ownerToken },
    })
    const recipe = await seedRecipe(ownerRequest, "Shared household supper")
    for (const [loginName, role] of [
      ["viewer", "viewer"],
      ["editor", "editor"],
    ]) {
      const response = await mutate(ownerContext.request, ownerToken, "/users", "POST", {
        login: loginName,
        display_name: loginName,
        password,
        library_id: home,
        role,
      })
      expect(response.ok(), await response.text()).toBe(true)
    }
    const users = (await (await ownerContext.request.get(`${origin}/api/v1/users`)).json())
      .users as { id: string; display_name: string }[]
    const viewerID = users.find((user) => user.display_name === "viewer")!.id
    const second = await mutate(ownerContext.request, ownerToken, "/libraries", "POST", {
      name: "Private test kitchen",
    })
    expect(second.ok(), await second.text()).toBe(true)
    const privateLibrary = await second.json()
    expect(
      (
        await mutate(ownerRequest, ownerToken, "/account/library", "POST", {
          library_id: privateLibrary.id,
        })
      ).ok(),
    ).toBe(true)
    const secret = await seedRecipe(ownerRequest, "Private owner recipe")
    await ownerRequest.dispose()

    expect(
      (
        await mutate(ownerContext.request, ownerToken, "/account/library", "POST", {
          library_id: privateLibrary.id,
        })
      ).ok(),
    ).toBe(true)
    await owner.goto(`${origin}/recipes/new`)
    await expect(chooser(owner, "Active library")).toHaveText(privateLibrary.name)
    await expect(chooser(owner, "Active library")).toBeDisabled()
    await owner.getByRole("textbox", { name: "Title", exact: true }).fill("Private unsaved draft")
    await owner.getByRole("textbox", { name: "Ingredient", exact: true }).fill("private ingredient")
    await owner.getByRole("textbox", { name: "Step 1", exact: true }).fill("Private method.")
    expect(
      (
        await mutate(ownerContext.request, ownerToken, "/account/library", "POST", {
          library_id: home,
        })
      ).ok(),
    ).toBe(true)
    await owner.getByRole("button", { name: "Save recipe", exact: true }).click()
    await expect(
      owner.getByText(
        "Your account or active library changed. Return to the original account and library before saving this recipe.",
        { exact: true },
      ),
    ).toBeVisible()
    expect(await (await ownerContext.request.get(`${origin}/api/v1/recipes`)).text()).not.toContain(
      "Private unsaved draft",
    )
    await owner.getByRole("button", { name: "Cancel", exact: true }).click()
    await owner.getByRole("button", { name: "Discard changes", exact: true }).click()
    await owner.goto(`${origin}/recipes/new`)
    await expect(owner.getByRole("textbox", { name: "Title", exact: true })).toHaveValue("")

    const viewer = await viewerContext.newPage()
    await login(viewer, "viewer")
    await expect(chooser(viewer, "Active library")).toHaveCount(0)
    await viewer.goto(`${origin}/recipes/${recipe.id}`)
    await viewer.getByRole("button", { name: "Add to favorites", exact: true }).click()
    await expect(
      viewer.getByRole("button", { name: "Remove from favorites", exact: true }),
    ).toBeVisible()
    await viewer.reload()
    await expect(
      viewer.getByRole("button", { name: "Remove from favorites", exact: true }),
    ).toBeVisible()
    const viewerToken = await csrf(viewerContext)
    const forbiddenTag = await mutate(viewerContext.request, viewerToken, "/tags", "POST", {
      name: "Forbidden shared change",
    })
    expect(forbiddenTag.status(), await forbiddenTag.text()).toBe(403)
    expect([403, 404]).toContain(
      (await viewerContext.request.get(`${origin}/api/v1/recipes/${secret.id}`)).status(),
    )
    expect(
      (
        await mutate(viewerContext.request, viewerToken, "/account/library", "POST", {
          library_id: privateLibrary.id,
        })
      ).status(),
    ).toBe(403)
    const listing = await (await viewerContext.request.get(`${origin}/api/v1/recipes`)).text()
    expect(listing).not.toContain(secret.id)

    const editor = await editorContext.newPage()
    await login(editor, "editor")
    await editor.goto(`${origin}/recipes/${recipe.id}`)
    await expect(
      editor.getByRole("button", { name: "Add to favorites", exact: true }),
    ).toBeVisible()
    await editor.getByRole("link", { name: "Edit", exact: true }).click()
    await editor
      .getByRole("textbox", { name: "Step 1", exact: true })
      .fill("Whisk gently until smooth.")
    await editor.getByRole("button", { name: "Save changes", exact: true }).click()
    await expect(editor.getByText("Whisk gently until smooth.", { exact: true })).toBeVisible()

    await viewer.goto(`${origin}/preferences`)
    await viewer.getByLabel("Timezone", { exact: true }).pressSequentially("America/Denver")
    await choose(chooser(viewer, "Measurement presentation"), "metric")
    await viewer.getByRole("button", { name: "Save preferences", exact: true }).click()
    await expect(viewer.getByText("Preferences saved.", { exact: true })).toBeVisible()
    await editor.goto(`${origin}/preferences`)
    await expect(editor.getByLabel("Timezone", { exact: true })).toHaveValue("")
    await editor.getByLabel("Timezone", { exact: true }).fill("Europe/Paris")
    await editor.getByRole("button", { name: "Save preferences", exact: true }).click()
    await expect(editor.getByText("Preferences saved.", { exact: true })).toBeVisible()
    await viewer.reload()
    await expect(viewer.getByLabel("Timezone", { exact: true })).toHaveValue("America/Denver")
    await expect(chooser(viewer, "Measurement presentation")).toHaveText("Metric")

    // Membership updates apply to existing sessions without another login.
    expect(
      (
        await mutate(
          ownerContext.request,
          ownerToken,
          `/libraries/${home}/members/${viewerID}`,
          "DELETE",
        )
      ).ok(),
    ).toBe(true)
    expect([403, 404]).toContain(
      (await viewerContext.request.get(`${origin}/api/v1/recipes/${recipe.id}`)).status(),
    )
    expect((await viewerContext.request.get(`${origin}/api/v1/me/preferences`)).ok()).toBe(true)
    const editorToken = await csrf(editorContext)
    expect(
      (
        await editorContext.request.post(`${origin}/api/v1/tags`, {
          data: { name: "Missing CSRF" },
        })
      ).status(),
    ).toBe(403)
    expect((await mutate(editorContext.request, editorToken, "/account/logout", "POST")).ok()).toBe(
      true,
    )
    expect((await editorContext.request.get(`${origin}/api/v1/recipes`)).status()).toBe(401)
  } finally {
    await Promise.allSettled([ownerContext.close(), viewerContext.close(), editorContext.close()])
  }
})

test("account management creates users, revokes sessions, changes passwords and safely deletes", async ({
  browser,
}) => {
  const ownerContext = await browser.newContext({ baseURL: origin })
  const memberContext = await browser.newContext({ baseURL: origin })
  const otherContext = await browser.newContext({ baseURL: origin })
  try {
    const owner = await ownerContext.newPage()
    await login(owner, "owner")
    const ownerAccount = await (await ownerContext.request.get(`${origin}/api/v1/account`)).json()
    const home =
      ownerAccount.libraries.find((library: { id: string }) => library.id === "default")?.id ??
      ownerAccount.active_library_id

    // The personal scope carries no administration; administration is reached
    // through its own navigation.
    await owner.goto(`${origin}/account`)
    await expect(owner.getByRole("heading", { name: "Your account", exact: true })).toBeVisible()
    await expect(owner.getByRole("heading", { name: "Accounts", exact: true })).toHaveCount(0)
    await owner.getByRole("button", { name: "More", exact: true }).click()
    await owner.getByRole("menuitem", { name: "Instance administration", exact: true }).click()
    await expect(
      owner.getByRole("heading", { name: "People", exact: true, level: 1 }),
    ).toBeVisible()

    await owner.getByRole("button", { name: "Add user", exact: true }).click()
    const create = owner.getByRole("dialog", { name: "Add user", exact: true })
    await create.getByLabel("Login name", { exact: true }).pressSequentially("lifecycle")
    await create.getByLabel("Display name", { exact: true }).pressSequentially("Lifecycle member")
    await create.getByLabel("Initial password", { exact: true }).fill(password)
    await choose(chooser(owner, "Add to a library you own"), home)
    const creation = owner.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/users") && response.request().method() === "POST",
    )
    await create.getByRole("button", { name: "Add user", exact: true }).click()
    expect((await creation).ok()).toBe(true)
    await owner.getByRole("button", { name: "Manage Lifecycle member", exact: true }).click()
    const management = owner.getByRole("dialog", { name: "Manage Lifecycle member", exact: true })
    const disable = management.getByRole("button", {
      name: "Disable Lifecycle member and revoke their sessions",
      exact: true,
    })
    await expect(disable).toBeVisible()

    const member = await memberContext.newPage()
    await login(member, "lifecycle")
    await disable.click()
    await management
      .getByRole("button", { name: "Disable and revoke sessions", exact: true })
      .click()
    const enable = management.getByRole("button", { name: "Enable Lifecycle member", exact: true })
    await expect(enable).toBeVisible()
    expect((await memberContext.request.get(`${origin}/api/v1/recipes`)).status()).toBe(401)
    await enable.click()
    await expect(enable).toHaveCount(0)
    await management.getByRole("button", { name: "Done", exact: true }).click()
    await login(member, "lifecycle")
    const other = await otherContext.newPage()
    await login(other, "lifecycle")

    // A member sees their own security, and nothing about anyone else.
    await member.goto(`${origin}/account/security`)
    await expect(
      member.getByRole("heading", { name: "Sign-in and security", exact: true, level: 1 }),
    ).toBeVisible()
    const revoke = member.getByRole("button", { name: /^Revoke the session last active/ })
    await expect(revoke).toHaveCount(1)
    await revoke.click()
    await expect(
      member.getByRole("button", { name: /^Revoke the session last active/ }),
    ).toHaveCount(0)
    expect((await otherContext.request.get(`${origin}/api/v1/recipes`)).status()).toBe(401)

    const newPassword = "changed browser fixture password 2026"
    await member.getByLabel("Current password", { exact: true }).fill(password)
    await member.getByLabel("New password", { exact: true }).fill(newPassword)
    await member.getByRole("button", { name: "Change password", exact: true }).click()
    await expect(
      member.getByRole("heading", { name: "Sign in to Savorum", exact: true }),
    ).toBeVisible()
    await member.getByLabel("Login name", { exact: true }).fill("lifecycle")
    await member.getByLabel("Password", { exact: true }).fill(newPassword)
    await member.getByRole("button", { name: "Sign in", exact: true }).click()
    await expect(member.getByRole("combobox", { name: "Search recipes" })).toBeVisible()

    // Hiding a control is never the check: administration refuses this member.
    const memberToken = await csrf(memberContext)
    for (const path of ["/users", `/libraries/${home}/members`, "/account/audit"])
      expect((await memberContext.request.get(`${origin}/api/v1${path}`)).status()).toBe(403)
    expect(
      (
        await mutate(memberContext.request, memberToken, `/libraries/${home}/invitations`, "POST", {
          role: "viewer",
          hint: "refused",
          days: 7,
        })
      ).status(),
    ).toBe(403)
    await member.goto(`${origin}/admin`)
    await expect(
      member.getByText(
        "Instance administration is available to administrators when authentication is enabled.",
        { exact: true },
      ),
    ).toBeVisible()
    await member.goto(`${origin}/libraries/${home}/access`)
    await expect(
      member.getByText("Only an owner can manage access to My library.", { exact: true }),
    ).toBeVisible()

    // The final administrator also cannot accidentally delete the instance owner.
    await owner.goto(`${origin}/account/security`)
    await owner.getByRole("button", { name: "Delete your account", exact: true }).click()
    const ownerDialog = owner.getByRole("dialog")
    const ownerPassword = ownerDialog.getByRole("textbox", {
      name: "Current password to confirm recent authentication",
      exact: true,
    })
    const ownerConfirmation = ownerDialog.getByRole("textbox", {
      name: "Type DELETE to confirm",
      exact: true,
    })
    // The dialog moves focus to its first field, selecting its text, a frame
    // after it appears; typing before then can land in the wrong field.
    await expect(ownerPassword).toBeFocused()
    await ownerPassword.fill(password)
    await ownerConfirmation.fill("DELETE")
    await expect(ownerPassword).toHaveValue(password)
    await expect(ownerConfirmation).toHaveValue("DELETE")
    const refused = owner.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/account") && response.request().method() === "DELETE",
    )
    await ownerDialog
      .getByRole("button", { name: "Permanently delete my account", exact: true })
      .click()
    expect((await refused).ok()).toBe(false)
    await expect(ownerDialog.getByRole("alert")).toBeVisible()

    // Exercise the separate sole-library-owner rule with an ordinary account.
    const memberAccount = await (await memberContext.request.get(`${origin}/api/v1/account`)).json()
    const ownerToken = await csrf(ownerContext)
    const ownedLibraryResponse = await mutate(
      ownerContext.request,
      ownerToken,
      "/libraries",
      "POST",
      { name: "Lifecycle owned library" },
    )
    expect(ownedLibraryResponse.ok()).toBe(true)
    const ownedLibrary = await ownedLibraryResponse.json()
    expect(
      (
        await mutate(
          ownerContext.request,
          ownerToken,
          `/libraries/${ownedLibrary.id}/members/${memberAccount.user.id}`,
          "PUT",
          { role: "owner" },
        )
      ).ok(),
    ).toBe(true)
    expect(
      (
        await mutate(
          ownerContext.request,
          ownerToken,
          `/libraries/${ownedLibrary.id}/members/${ownerAccount.user.id}`,
          "DELETE",
        )
      ).ok(),
    ).toBe(true)
    async function attemptSelfDeletion() {
      await member.goto(`${origin}/account/security`)
      await member.getByRole("button", { name: "Delete your account", exact: true }).click()
      const dialog = member.getByRole("dialog")
      const field = dialog.getByRole("textbox", {
        name: "Current password to confirm recent authentication",
        exact: true,
      })
      const confirmation = dialog.getByRole("textbox", {
        name: "Type DELETE to confirm",
        exact: true,
      })
      await expect(field).toBeFocused()
      await field.fill(newPassword)
      await confirmation.fill("DELETE")
      await expect(field).toHaveValue(newPassword)
      await expect(confirmation).toHaveValue("DELETE")
      const response = member.waitForResponse(
        (event) => event.url().endsWith("/api/v1/account") && event.request().method() === "DELETE",
      )
      await dialog
        .getByRole("button", { name: "Permanently delete my account", exact: true })
        .click()
      return response
    }
    expect((await attemptSelfDeletion()).status()).toBe(409)
    expect((await memberContext.request.get(`${origin}/api/v1/me/preferences`)).ok()).toBe(true)
    expect(
      (
        await mutate(
          ownerContext.request,
          ownerToken,
          `/libraries/${ownedLibrary.id}/recover`,
          "POST",
          { user_id: ownerAccount.user.id },
        )
      ).ok(),
    ).toBe(true)

    await attemptSelfDeletion()
    await expect(
      member.getByRole("heading", { name: "Sign in to Savorum", exact: true }),
    ).toBeVisible()
    expect((await memberContext.request.get(`${origin}/api/v1/me/preferences`)).status()).toBe(401)
    expect((await ownerContext.request.get(`${origin}/api/v1/recipes`)).ok()).toBe(true)
  } finally {
    await Promise.allSettled([ownerContext.close(), memberContext.close(), otherContext.close()])
  }
})

test("a library's access page keeps acting on its own library after another tab switches", async ({
  browser,
}) => {
  const context = await browser.newContext({ baseURL: origin })
  try {
    const page = await context.newPage()
    await login(page, "owner")
    const token = await csrf(context)
    const created = await mutate(context.request, token, "/libraries", "POST", {
      name: "Cross-tab kitchen",
    })
    expect(created.ok(), await created.text()).toBe(true)
    const second = await created.json()

    await page.goto(`${origin}/libraries/default/access`)
    await expect(
      page.getByRole("heading", { name: "Access to My library", exact: true, level: 1 }),
    ).toBeVisible()

    const switcher = await context.newPage()
    await switcher.goto(`${origin}/`)
    await choose(chooser(switcher, "Active library"), second.id)
    await expect
      .poll(
        async () =>
          (await (await context.request.get(`${origin}/api/v1/account`)).json()).active_library_id,
      )
      .toBe(second.id)

    await page.getByLabel("Private label (optional)", { exact: true }).fill("Cross-tab invitation")
    await page.getByRole("button", { name: "Create invitation", exact: true }).click()
    await expect(page.getByLabel("Invitation link — shown once")).toBeVisible()
    await expect(
      page.getByRole("heading", { name: "Access to My library", exact: true, level: 1 }),
    ).toBeVisible()

    const home = await (
      await context.request.get(`${origin}/api/v1/libraries/default/invitations`)
    ).json()
    const elsewhere = await (
      await context.request.get(`${origin}/api/v1/libraries/${second.id}/invitations`)
    ).json()
    expect(home.invitations.map((invitation: { hint: string }) => invitation.hint)).toContain(
      "Cross-tab invitation",
    )
    expect(elsewhere.invitations).toHaveLength(0)
  } finally {
    await context.close()
  }
})

test("signing out removes the recipes the worker kept for offline reading", async ({
  browser,
  playwright,
}) => {
  const context = await browser.newContext({ baseURL: origin, serviceWorkers: "allow" })
  let request: APIRequestContext | undefined
  try {
    const page = await context.newPage()
    await login(page, "owner")
    const token = await csrf(context)
    const account = await (await context.request.get(`${origin}/api/v1/account`)).json()
    request = await playwright.request.newContext({
      baseURL: origin,
      storageState: await context.storageState(),
      extraHTTPHeaders: { "X-CSRF-Token": token },
    })
    const recipe = await seedRecipe(request, "Offline supper")
    const path = `/api/v1/recipes/${recipe.id}`
    const readRecipe = () =>
      page.evaluate(
        async ({ path, library }) => {
          const response = await fetch(path, { headers: { "X-Savorum-Library": library } })
          return { status: response.status, text: await response.text() }
        },
        { path, library: account.active_library_id as string },
      )
    const dataCaches = () =>
      page.evaluate(async () =>
        (await caches.keys()).filter((name) => name.startsWith("savorum-data-")),
      )

    // The worker keeps responses only once it has been told whose they are.
    await expect
      .poll(() =>
        page.evaluate(async () => {
          await navigator.serviceWorker.ready
          return (await (await caches.open("savorum-scope-v1")).keys()).length
        }),
      )
      .toBeGreaterThan(0)
    await page.goto(`${origin}/recipes/${recipe.id}`)
    await expect(page.getByRole("heading", { name: recipe.title, level: 1 })).toBeVisible()
    await expect.poll(dataCaches).toHaveLength(1)

    await context.setOffline(true)
    expect(await readRecipe()).toMatchObject({ status: 200 })
    await context.setOffline(false)

    await page.goto(`${origin}/account`)
    await page.getByRole("button", { name: "More", exact: true }).click()
    await page.getByRole("menuitem", { name: "Sign out", exact: true }).click()
    await page.getByRole("dialog").getByRole("button", { name: "Sign out", exact: true }).click()
    await expect(page.getByLabel("Login name", { exact: true })).toBeVisible()
    await expect.poll(dataCaches).toEqual([])

    expect((await readRecipe()).status).toBe(401)
    await context.setOffline(true)
    const offline = await readRecipe()
    expect(offline.status).toBe(503)
    expect(offline.text).not.toContain(recipe.title)
  } finally {
    await request?.dispose()
    await context.close()
  }
})

test("populated account, library access and administration surfaces pass axe", async ({
  browser,
}) => {
  const context = await browser.newContext({ baseURL: origin })
  try {
    const page = await context.newPage()
    await login(page, "owner")
    for (const path of [
      "/account",
      "/account/security",
      "/account/tokens",
      "/libraries/default/access",
      "/admin",
      "/admin/libraries",
      "/admin/providers",
      "/admin/audit",
    ]) {
      await page.goto(`${origin}${path}`)
      await expect(page.locator("main")).toBeVisible()
      await expectAccessible(page)
      // A destructive confirmation adds its own fields to the page, and a label
      // that resolves to another field's identifier would confirm the wrong
      // thing.
      for (const trigger of ["Delete your account", "Retire library"]) {
        const button = page.getByRole("button", { name: trigger, exact: true })
        if ((await button.count()) === 0) continue
        await button.click()
        await expect(page.getByRole("dialog")).toBeVisible()
        await expectFieldsAddressTheirOwnControls(page, `${path} with ${trigger} open`)
        await page.getByRole("button", { name: "Cancel", exact: true }).click()
        await expect(page.getByRole("dialog")).toHaveCount(0)
      }
      await expectFieldsAddressTheirOwnControls(page, path)
    }
  } finally {
    await context.close()
  }
})

test("account, library access and administration views reflow at 320 CSS pixels", async ({
  browser,
}) => {
  const context = await browser.newContext({
    baseURL: origin,
    viewport: { width: 320, height: 800 },
  })
  try {
    const page = await context.newPage()
    await login(page, "owner")
    for (const path of [
      "/account",
      "/account/security",
      "/account/tokens",
      "/libraries/default/access",
      "/admin",
      "/admin/libraries",
      "/admin/providers",
      "/admin/audit",
    ]) {
      await page.goto(`${origin}${path}`)
      await expect(page.locator("main")).toBeVisible()
      const overflow = await page.evaluate(() =>
        Array.from(document.querySelectorAll<HTMLElement>("body *"))
          .filter((element) => {
            const rect = element.getBoundingClientRect()
            return rect.right > window.innerWidth + 1 || rect.left < -1
          })
          .slice(0, 12)
          .map((element) => ({
            tag: element.tagName,
            className: element.className,
            text: element.textContent?.trim().slice(0, 80),
          })),
      )
      expect(overflow, `${path} has horizontal overflow`).toEqual([])
    }
  } finally {
    await context.close()
  }
})

test("shared login and account inputs preserve focus, caret and composition", async ({
  browser,
}) => {
  const context = await browser.newContext({ baseURL: origin })
  const page = await context.newPage()
  async function checkInputs() {
    const inputs = page.locator('input:not([type]), input[type="text"], input[type="password"]')
    for (const input of await inputs.all()) {
      if (!(await input.isVisible()) || !(await input.isEnabled())) continue
      await input.fill("")
      await input.pressSequentially("Continuous entry")
      await input.evaluate((element: HTMLInputElement) => {
        element.dataset.editIdentity = "retained"
        element.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true }))
        element.setRangeText("茶", element.selectionStart!, element.selectionEnd!, "end")
        element.dispatchEvent(
          new InputEvent("input", { bubbles: true, data: "茶", isComposing: true }),
        )
        element.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: "茶" }))
      })
      await expect(input).toHaveValue("Continuous entry茶")
      await expect(input).toBeFocused()
      await expect(input).toHaveAttribute("data-edit-identity", "retained")
      expect(await input.evaluate((element: HTMLInputElement) => element.selectionStart)).toBe(17)
    }
  }
  try {
    await page.goto(origin)
    await checkInputs()
    await login(page, "owner")
    await page.goto(`${origin}/admin`)
    await page.getByRole("button", { name: "Add user", exact: true }).click()
    const create = page.getByRole("dialog", { name: "Add user", exact: true })
    await expect(create).toBeVisible()
    await checkInputs()
    await create.getByRole("button", { name: "Cancel", exact: true }).click()
    await page.goto(`${origin}/account/security`)
    await expect(
      page.getByRole("heading", { name: "Sign-in and security", exact: true, level: 1 }),
    ).toBeVisible()
    await checkInputs()
  } finally {
    await context.close()
  }
})
