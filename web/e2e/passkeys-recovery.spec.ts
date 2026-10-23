import { spawn, execFile, type ChildProcess } from "node:child_process"
import { mkdtemp, rm, writeFile } from "node:fs/promises"
import { createServer } from "node:net"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"
import { promisify } from "node:util"

import { expect, test, type Page } from "@playwright/test"

import { openSection } from "./controls"

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
async function request(page: Page, path: string, body: unknown = {}) {
  return page.evaluate(
    async ({ path, body }) => {
      const state = await fetch("/api/v1/account").then((r) => r.json())
      const response = await fetch(`/api/v1${path}`, {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": state.csrf_token },
        body: JSON.stringify(body),
      })
      return { status: response.status, body: await response.json() }
    },
    { path, body },
  )
}
test("roaming discoverable passkey verifies real assertions, rejects tampering and recovers once", async ({
  browser,
}) => {
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
    await openSection(page, /^Roaming key\b/)
    const rename = page.getByRole("textbox", { name: "Name for Roaming key" })
    await rename.fill("")
    await rename.pressSequentially("Travel security key")
    await page.getByRole("button", { name: "Save name", exact: true }).click()
    await openSection(page, /^Travel security key\b/)
    await expect(page.getByRole("textbox", { name: "Name for Travel security key" })).toHaveValue(
      "Travel security key",
    )
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
    await expect(page.getByText(/9 unused codes remaining/)).toBeVisible()
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
