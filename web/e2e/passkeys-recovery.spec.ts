import { spawn, execFile, type ChildProcess } from "node:child_process"
import { mkdtemp, rm, writeFile } from "node:fs/promises"
import { createServer } from "node:net"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"
import { promisify } from "node:util"

import AxeBuilder from "@axe-core/playwright"
import { expect, test, type Page } from "@playwright/test"

import { settleAnimations } from "./controls"

let server: ChildProcess
let directory: string
let origin: string
const password = "passkey browser fixture long password 2026"
test.beforeAll(async ({ playwright }) => {
  directory = await mkdtemp(join(tmpdir(), "savorum-passkeys-e2e-"))
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
  const listener = createServer()
  await new Promise<void>((resolve) => listener.listen(0, "127.0.0.1", resolve))
  const address = listener.address()
  if (!address || typeof address === "string") throw new Error("No test port")
  await new Promise<void>((resolve, reject) =>
    listener.close((error) => (error ? reject(error) : resolve())),
  )
  origin = `http://localhost:${address.port}`
  server = spawn(
    binary,
    [
      "serve",
      "--database",
      database,
      "--listen",
      `127.0.0.1:${address.port}`,
      "--public-url",
      origin,
      "--auth-enabled",
    ],
    { stdio: ["ignore", "pipe", "pipe"] },
  )
  let output = ""
  for (const stream of [server.stdout, server.stderr])
    stream?.on("data", (data) => {
      output = (output + String(data)).slice(-8000)
    })
  const request = await playwright.request.newContext({ baseURL: origin })
  try {
    await expect
      .poll(async () => {
        if (server.exitCode !== null) throw new Error(output)
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
async function request(page: Page, path: string, body: unknown = {}, method = "POST") {
  return page.evaluate(
    async ({ path, body, method }) => {
      const state = await fetch("/api/v1/account").then((r) => r.json())
      const response = await fetch(`/api/v1${path}`, {
        method,
        headers: { "Content-Type": "application/json", "X-CSRF-Token": state.csrf_token },
        body: JSON.stringify(body),
      })
      return { status: response.status, body: await response.json() }
    },
    { path, body, method },
  )
}
test("roaming discoverable passkey verifies real assertions, rejects tampering and recovers once", async ({
  browser,
}, testInfo) => {
  const context = await browser.newContext()
  const page = await context.newPage()
  const cdp = await context.newCDPSession(page)
  await cdp.send("WebAuthn.enable")
  const { authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: {
      protocol: "ctap2",
      ctap2Version: "ctap2_1",
      transport: "usb",
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      automaticPresenceSimulation: true,
    },
  })
  try {
    await page.goto(origin)
    await page.getByLabel("Login name", { exact: true }).fill("owner")
    await page.getByLabel("Password", { exact: true }).fill(password)
    await page.getByRole("button", { name: "Sign in", exact: true }).click()
    await expect(page.getByRole("combobox", { name: "Search recipes" })).toBeVisible()
    await page.goto(`${origin}/account/security`)
    const name = page.getByLabel("Passkey name", { exact: true })
    await name.fill("Roaming key")
    await page.getByRole("button", { name: "Add passkey", exact: true }).click()
    await expect(page.getByText("Passkey added.", { exact: true })).toBeVisible()
    const credentials = await cdp.send("WebAuthn.getCredentials", { authenticatorId })
    expect(credentials.credentials).toHaveLength(1)
    expect(credentials.credentials[0]?.isResidentCredential).toBe(true)
    expect(credentials.credentials[0]?.rpId).toBe("localhost")
    await page.getByRole("button", { name: "Rename Roaming key", exact: true }).click()
    const rename = page.getByRole("textbox", { name: "Name for Roaming key" })
    await rename.fill("")
    await rename.pressSequentially("Travel security key")
    await page.getByRole("button", { name: "Save name", exact: true }).click()
    await page.getByRole("button", { name: "Rename Travel security key", exact: true }).click()
    await expect(page.getByRole("textbox", { name: "Name for Travel security key" })).toHaveValue(
      "Travel security key",
    )
    await page.getByRole("button", { name: "Cancel", exact: true }).click()
    await expect(
      page.getByRole("button", { name: "Rename Travel security key", exact: true }),
    ).toBeFocused()
    for (const colorScheme of ["light", "dark"] as const) {
      await page.emulateMedia({ colorScheme })
      await page.getByRole("button", { name: "Remove Travel security key", exact: true }).click()
      await expect(
        page.getByRole("dialog", { name: "Remove Travel security key", exact: true }),
      ).toBeVisible()
      await settleAnimations(page)
      expect(
        (await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa"]).analyze())
          .violations,
      ).toEqual([])
      await page.screenshot({ path: testInfo.outputPath(`passkey-removal-${colorScheme}.png`) })
      await page.getByRole("button", { name: "Cancel", exact: true }).click()
      await expect(
        page.getByRole("button", { name: "Remove Travel security key", exact: true }),
      ).toBeFocused()
    }
    await page.emulateMedia({ colorScheme: "light" })
    await page.getByRole("button", { name: "Generate 10 recovery codes", exact: true }).click()
    const codeText = await page.getByLabel("Your recovery codes").textContent()
    const codes = (codeText ?? "").split("\n")
    expect(codes).toHaveLength(10)
    await page.getByRole("button", { name: "I saved my codes", exact: true }).click()
    await expect(page.getByLabel("Your recovery codes")).toHaveCount(0)

    // Complete a real signed assertion using the browser's virtual authenticator.
    // Verify that a tampered assertion is rejected and a valid one succeeds.
    for (const tamper of ["signature", "none"]) {
      const begin = await request(page, "/account/passkeys/login/begin")
      expect(begin.status).toBe(200)
      const assertion = await page.evaluate(
        async ({ options, tamper }) => {
          const decode = (v: string) =>
            Uint8Array.from(atob(v.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0))
          const encode = (v: ArrayBuffer | Uint8Array) =>
            btoa(String.fromCharCode(...new Uint8Array(v)))
              .replace(/\+/g, "-")
              .replace(/\//g, "_")
              .replace(/=+$/, "")
          const credential = (await navigator.credentials.get({
            publicKey: {
              ...options,
              challenge: decode(options.challenge),
              userVerification: "required",
            },
          })) as PublicKeyCredential
          const response = credential.response as AuthenticatorAssertionResponse
          const client = JSON.parse(new TextDecoder().decode(response.clientDataJSON))
          const auth = new Uint8Array(response.authenticatorData)
          const signature = new Uint8Array(response.signature)
          if (tamper === "signature")
            signature[signature.length - 1] = (signature[signature.length - 1] ?? 0) ^ 1
          return {
            id: credential.id,
            rawId: encode(credential.rawId),
            type: "public-key",
            response: {
              clientDataJSON: encode(new TextEncoder().encode(JSON.stringify(client))),
              authenticatorData: encode(auth),
              signature: encode(signature),
              userHandle: encode(response.userHandle!),
            },
          }
        },
        { options: begin.body.publicKey, tamper },
      )
      const finish = await request(page, "/account/passkeys/login/finish", assertion)
      expect(finish.status).toBe(tamper === "none" ? 200 : 401)
      expect((await request(page, "/account/passkeys/login/finish", assertion)).status).toBe(401)
    }
    const state = await context.request.get(`${origin}/api/v1/account`).then((r) => r.json())
    expect(state.user.display_name).toBe("Owner")
    expect(
      (
        await context.request.get(`${origin}/api/v1/account/sessions`).then((r) => r.json())
      ).sessions.some(
        (s: { current: boolean; auth_method: string }) => s.current && s.auth_method === "passkey",
      ),
    ).toBe(true)
    await request(page, "/account/logout")
    const recovered = await request(page, "/account/recovery/login", {
      login: "owner",
      code: codes[0],
    })
    expect(recovered.status).toBe(200)
    expect(recovered.body.recovery_authenticated).toBe(true)
    await page.goto(`${origin}/account/security`)
    await expect(page.getByText(/9 recovery codes remaining/)).toBeVisible()
    expect(
      (await request(page, "/account/recovery/login", { login: "owner", code: codes[0] })).status,
    ).toBe(401)
    await page.getByRole("button", { name: "Generate replacement codes", exact: true }).click()
    await page.getByRole("button", { name: "Replace recovery codes", exact: true }).click()
    await expect(page.getByLabel("Your recovery codes")).toBeVisible()
    expect(
      (await request(page, "/account/recovery/login", { login: "owner", code: codes[1] })).status,
    ).toBe(401)
  } finally {
    await cdp.send("WebAuthn.removeVirtualAuthenticator", { authenticatorId })
    await context.close()
  }
})

test("account lists and preferences stay readable and usable across themes and widths", async ({
  browser,
}, testInfo) => {
  const context = await browser.newContext()
  const page = await context.newPage()
  async function login(loginName: string) {
    await page.goto(`${origin}/account`)
    await page.getByLabel("Login name", { exact: true }).fill(loginName)
    await page.getByLabel("Password", { exact: true }).fill(password)
    await page.getByRole("button", { name: "Sign in", exact: true }).click()
    await expect(page.getByRole("heading", { name: "Your account", exact: true })).toBeVisible()
  }
  try {
    await login("owner")
    await page.goto(`${origin}/admin`)
    await page.getByRole("button", { name: "Add user", exact: true }).click()
    const addUser = page.getByRole("dialog", { name: "Add user", exact: true })
    await addUser.getByLabel("Login name", { exact: true }).fill("review")
    await addUser.getByLabel("Display name", { exact: true }).fill("Morgan Lee")
    await addUser.getByLabel("Initial password", { exact: true }).fill(password)
    await addUser.getByRole("button", { name: "Add user", exact: true }).click()
    await expect(addUser).toHaveCount(0)
    await expect(page.getByRole("button", { name: "Manage Morgan Lee", exact: true })).toBeVisible()
    const former = await request(page, "/users", {
      login: "former",
      display_name: "Former user",
      password,
    })
    expect(former.status).toBe(200)
    expect(
      (await request(page, `/users/${former.body.id}`, { disabled: true }, "PUT")).status,
    ).toBe(200)
    await page.goto(`${origin}/admin`)
    await page.getByRole("button", { name: "Manage Morgan Lee", exact: true }).click()
    const reset = page.getByLabel("New password for Morgan Lee", { exact: true })
    await reset.pressSequentially("a complete new password")
    await expect(reset).toHaveValue("a complete new password")
    await expect(reset).toBeFocused()
    await page.getByRole("button", { name: "Done", exact: true }).click()
    await expect(page.getByRole("button", { name: "Manage Morgan Lee", exact: true })).toBeFocused()
    await page.getByRole("button", { name: "Manage Morgan Lee", exact: true }).click()
    await expect(reset).toHaveValue("")
    await reset.fill(password)
    await page.getByRole("button", { name: "Reset password", exact: true }).click()
    await expect(
      page.getByText("Password reset. This user has been signed out of all sessions.", {
        exact: true,
      }),
    ).toBeVisible()
    await expect(reset).toHaveValue("")
    await page.getByRole("button", { name: "Done", exact: true }).click()
    await page.getByRole("button", { name: "Manage Morgan Lee", exact: true }).click()
    const managed = page.getByRole("dialog", { name: "Manage Morgan Lee", exact: true })
    await managed
      .getByRole("button", { name: "Disable Morgan Lee and revoke their sessions", exact: true })
      .click()
    await managed.getByRole("button", { name: "Disable and revoke sessions", exact: true }).click()
    await expect(managed.getByText("Disabled", { exact: true })).toBeVisible()
    await managed.getByRole("button", { name: "Enable Morgan Lee", exact: true }).click()
    await expect(managed.getByText("Active", { exact: true })).toBeVisible()
    await managed.getByRole("button", { name: "Done", exact: true }).click()
    for (let index = 0; index < 5; index++) {
      const otherContext = await browser.newContext()
      try {
        const otherPage = await otherContext.newPage()
        await otherPage.goto(`${origin}/account`)
        await otherPage.getByLabel("Login name", { exact: true }).fill("owner")
        await otherPage.getByLabel("Password", { exact: true }).fill(password)
        await otherPage.getByRole("button", { name: "Sign in", exact: true }).click()
        await expect(
          otherPage.getByRole("heading", { name: "Your account", exact: true }),
        ).toBeVisible()
      } finally {
        await otherContext.close()
      }
    }
    await page.goto(`${origin}/account/security`)
    const sessions = page
      .locator("section")
      .filter({ has: page.getByRole("heading", { name: "Active sessions", exact: true }) })
    await expect(sessions.getByRole("listitem")).toHaveCount(5)
    await sessions.getByRole("button", { name: "Next", exact: true }).click()
    await expect(sessions.getByText("This browser", { exact: true })).toBeVisible()
    expect(await sessions.getByRole("listitem").count()).toBeLessThanOrEqual(5)
    const audited = await request(page, "/users", {
      login: "audit-subject",
      display_name: "Riley Chen",
      password,
    })
    expect(audited.status).toBe(200)
    expect(
      (await request(page, `/users/${audited.body.id}`, { disabled: true }, "PUT")).status,
    ).toBe(200)
    expect(
      (
        await request(page, "/account/login", {
          login: "unknown-audit-login",
          password: "incorrect password",
        })
      ).status,
    ).toBe(401)
    await page.goto(`${origin}/preferences`)
    const timezone = page.getByRole("combobox", { name: "Timezone", exact: true })
    await timezone.fill("America/")
    await timezone.pressSequentially("Denver")
    await expect(timezone).toHaveValue("America/Denver")
    await expect(timezone).toBeFocused()
    await page.getByRole("button", { name: "Save preferences", exact: true }).click()
    await expect(page.getByText("Preferences saved.", { exact: true })).toBeVisible()
    for (const colorScheme of ["light", "dark"] as const) {
      await page.emulateMedia({ colorScheme })
      for (const width of [1280, 768, 390]) {
        await page.setViewportSize({ width, height: 844 })
        for (const path of [
          "account",
          "admin",
          "admin/connect",
          "admin/audit",
          "account/security",
          "preferences",
        ]) {
          await page.goto(`${origin}/${path}`)
          await expect(page.locator("main")).toBeVisible()
          if (path === "account/security")
            await expect(page.getByText("This browser", { exact: true })).toBeVisible()
          if (path === "admin")
            await expect(
              page.getByRole("button", { name: "Manage Former user", exact: true }),
            ).toBeVisible()
          if (path === "admin/connect")
            await expect(page.getByLabel("Setup document", { exact: true })).toBeVisible()
          if (path === "admin/audit")
            await expect(
              page.getByRole("button", { name: "Apply filters", exact: true }),
            ).toBeVisible()
          if (path === "preferences")
            await expect(
              page.getByRole("combobox", { name: "Timezone", exact: true }),
            ).toBeVisible()
          if (path === "admin/audit") {
            const table = page.getByRole("table").first()
            const caption = table.locator("caption")
            await expect(caption).toBeVisible()
            const bounds = await caption.boundingBox()
            expect(bounds!.height).toBeLessThan(64)
            expect(bounds!.width).toBeGreaterThan((await table.boundingBox())!.width * 0.9)
            await expect(table.getByRole("columnheader")).toHaveCount(4)
            if (width >= 768) {
              const filter = await page
                .getByRole("combobox", { name: "Activity", exact: true })
                .boundingBox()
              const apply = await page
                .getByRole("button", { name: "Apply filters", exact: true })
                .boundingBox()
              expect(Math.abs(filter!.y + filter!.height - apply!.y - apply!.height)).toBeLessThan(
                3,
              )
            }
            if (width === 390) {
              const row = await table.locator("tbody > tr").first().boundingBox()
              expect(row!.height).toBeLessThan(110)
            }
          }
          if (path === "preferences") {
            await page
              .getByRole("combobox", { name: "Measurement presentation", exact: true })
              .focus()
            const save = page.getByRole("button", { name: "Save preferences", exact: true })
            for (let index = 0; index < 15; index++) {
              if (await save.evaluate((button) => button === document.activeElement)) break
              const focused = await page.locator(":focus").boundingBox()
              const footer = await save.locator("..").boundingBox()
              expect(focused).not.toBeNull()
              expect(focused!.y + focused!.height).toBeLessThanOrEqual(footer!.y)
              await page.keyboard.press("Tab")
            }
            await expect(save).toBeFocused()
            await page
              .getByRole("heading", { name: "Your preferences", exact: true })
              .scrollIntoViewIfNeeded()
          }
          await settleAnimations(page)
          expect(
            (
              await new AxeBuilder({ page })
                .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
                .analyze()
            ).violations,
          ).toEqual([])
          expect(
            await page.evaluate(() => document.documentElement.scrollWidth),
          ).toBeLessThanOrEqual(width)
          if (path === "account/security") {
            await page
              .getByRole("heading", { name: "Passkeys", exact: true })
              .scrollIntoViewIfNeeded()
            await page
              .locator("section")
              .filter({ has: page.getByRole("heading", { name: "Passkeys", exact: true }) })
              .screenshot({
                path: testInfo.outputPath(`passkeys-${colorScheme}-${width}.png`),
              })
            await page
              .getByRole("heading", { name: "Active sessions", exact: true })
              .scrollIntoViewIfNeeded()
          }
          const capture = path === "account/security" ? sessions : page
          await capture.screenshot({
            path: testInfo.outputPath(`${path.replaceAll("/", "-")}-${colorScheme}-${width}.png`),
          })
          if (path === "admin/audit") {
            const recordToggle = page.getByRole("button", { name: /^Record details:/ }).first()
            await recordToggle.focus()
            await recordToggle.press("Enter")
            await expect(recordToggle).toBeFocused()
            await expect(
              page.getByRole("heading", { name: "Record details", exact: true }),
            ).toBeVisible()
            expect(
              (
                await new AxeBuilder({ page })
                  .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
                  .analyze()
              ).violations,
            ).toEqual([])
            const record = page.locator(`#${await recordToggle.getAttribute("aria-controls")}`)
            await expect(record).toBeVisible()
            const fields = record.locator("dl > div")
            const firstField = await fields.nth(0).boundingBox()
            const secondField = await fields.nth(1).boundingBox()
            if (width >= 768) expect(secondField!.x).toBeGreaterThan(firstField!.x + 100)
            else expect(secondField!.x).toBe(firstField!.x)
            expect((await record.boundingBox())!.width).toBeLessThanOrEqual(width)
            await record.screenshot({
              path: testInfo.outputPath(`audit-details-${colorScheme}-${width}.png`),
            })
            await recordToggle.click()
            if (colorScheme === "light" && width === 1280) {
              await page.getByRole("combobox", { name: "By", exact: true }).click()
              await page.getByRole("option", { name: "Morgan Lee", exact: true }).click()
              await page.getByRole("button", { name: "Apply filters", exact: true }).click()
              await expect(page.getByText("No matching activity.", { exact: true })).toBeVisible()
              await page.getByRole("combobox", { name: "By", exact: true }).click()
              await page.getByRole("option", { name: "Anyone", exact: true }).click()
              await page.getByRole("button", { name: "Apply filters", exact: true }).click()
              await expect(page.getByRole("table").first()).toBeVisible()
            }
            await page.getByRole("button", { name: "More filters", exact: true }).click()
            const actor = page.getByLabel("Actor ID", { exact: true })
            await actor.fill(former.body.id)
            await page.getByRole("button", { name: "Apply filters", exact: true }).click()
            await expect(page.getByText("No matching activity.", { exact: true })).toBeVisible()
            await actor.fill("")
            await page.getByRole("button", { name: "Apply filters", exact: true }).click()
            await expect(page.getByRole("table").first()).toBeVisible()
            await page.screenshot({
              path: testInfo.outputPath(`audit-filters-${colorScheme}-${width}.png`),
            })
          }
          if (path === "admin") {
            await page.getByRole("button", { name: "Manage Morgan Lee", exact: true }).click()
            await expect(
              page.getByLabel("New password for Morgan Lee", { exact: true }),
            ).toBeVisible()
            await settleAnimations(page)
            expect(
              await page.evaluate(() => document.documentElement.scrollWidth),
            ).toBeLessThanOrEqual(width)
            expect(
              (await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21aa"]).analyze())
                .violations,
            ).toEqual([])
            await page.getByRole("dialog", { name: "Manage Morgan Lee", exact: true }).screenshot({
              path: testInfo.outputPath(`admin-manage-${colorScheme}-${width}.png`),
            })
            await page.getByRole("button", { name: "Done", exact: true }).click()
            await page.getByRole("button", { name: "Manage Former user", exact: true }).click()
            await page.getByRole("dialog", { name: "Manage Former user", exact: true }).screenshot({
              path: testInfo.outputPath(`admin-disabled-${colorScheme}-${width}.png`),
            })
            await page.getByRole("button", { name: "Done", exact: true }).click()
            await page.getByRole("button", { name: "Add user", exact: true }).click()
            await expect(page.getByRole("dialog", { name: "Add user", exact: true })).toBeVisible()
            await settleAnimations(page)
            await page.screenshot({
              path: testInfo.outputPath(`admin-add-${colorScheme}-${width}.png`),
            })
            await page.getByRole("button", { name: "Cancel", exact: true }).click()
          }
        }
      }
    }
    await page.goto(`${origin}/account/security`)
    await page.getByRole("button", { name: "Sign out all other sessions", exact: true }).click()
    await expect(page.getByText("Other sessions revoked.", { exact: true })).toBeVisible()
    await expect(page.getByText("This browser", { exact: true })).toBeVisible()
    await expect(page.getByRole("button", { name: "Next", exact: true })).toHaveCount(0)
    await request(page, "/account/logout")
    await login("review")
    await page.goto(`${origin}/account/security`)
    await expect(page.getByText("No recovery codes available.", { exact: true })).toBeVisible()
  } finally {
    await context.close()
  }
})
