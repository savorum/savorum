import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import { Suspense, type JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { publish } from "@/api/live-events"
import { PantryPanel } from "@/features/shopping/pantry-panel"

import { IntegrationsPage } from "./integrations-page"

const requests = vi.hoisted(() => ({
  changeLibrary: (_library: string) => {},
  GET: vi.fn(async () => ({ data: [], response: new Response("[]") })),
  PUT: vi.fn(async (_path?: unknown, _options?: unknown) => ({
    data: { revision: 1 },
    response: new Response("{}"),
  })),
  DELETE: vi.fn(async () => ({ response: new Response(null, { status: 204 }) })),
  session: vi.fn(
    async () => new Response("[]", { headers: { "Content-Type": "application/json" } }),
  ),
}))
vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { children?: JSX.Element }) => <a href="/account">{props.children}</a>,
}))
vi.mock("./account-state", async () => {
  const { createSignal } = await import("solid-js")
  const [library, setLibrary] = createSignal("home")
  requests.changeLibrary = setLibrary
  return {
    account: () => ({
      auth_enabled: true,
      user: { id: "owner", display_name: "Owner", instance_admin: false },
      active_library_id: library(),
      libraries: [{ id: library(), name: "Home", role: "owner" }],
    }),
    authEnabled: () => true,
    administers: () => false,
    libraries: () => [{ id: "home", name: "Home", role: "owner" }],
  }
})
vi.mock("@/api/capabilities", () => ({
  capabilities: () => ({ event_types: ["recipe.created", "recipe.updated", "user.disabled"] }),
  loadCapabilities: async () => ({ features: ["webhooks", "pantry.presence"] }),
  supports: () => true,
}))
vi.mock("@/api/session", () => ({ sessionFetch: requests.session }))
vi.mock("@/api/client", () => ({
  client: requests,
  ApiError: class extends Error {},
  ifMatch: (revision: number) => `"${revision}"`,
  unwrap: (result: { data?: unknown; error?: unknown }) => {
    if (result.error) throw new Error("conflict")
    return result.data
  },
  expectOk: (result: { error?: unknown }) => {
    if (result.error) throw new Error("conflict")
  },
  failure: () => new Error("The subscription changed. Reload before saving again."),
}))
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  requests.changeLibrary("home")
})

function compose(input: HTMLInputElement, value = "abc東京def") {
  input.focus()
  for (const text of ["a", "abcdef"]) {
    fireEvent.input(input, { target: { value: text } })
    expect(document.activeElement).toBe(input)
    expect(input.isConnected).toBe(true)
    expect(input.value).toBe(text)
  }
  const canSelect = input.type !== "url"
  if (canSelect) input.setSelectionRange(3, 3)
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value }, isComposing: true })
  if (canSelect) input.setSelectionRange(5, 5)
  fireEvent.compositionEnd(input, { data: "東京" })
  expect(document.activeElement).toBe(input)
  expect(input.isConnected).toBe(true)
  expect(input.value).toBe(value)
  if (canSelect) expect(input.selectionStart).toBe(5)
}

test("pantry drafts retain identity, focus, composition, and edits during a live refresh", async () => {
  const view = render(() => (
    <Suspense fallback={<p>Loading</p>}>
      <PantryPanel canEdit />
    </Suspense>
  ))
  fireEvent.click(await view.findByRole("button", { name: "Pantry" }))
  fireEvent.click(await view.findByRole("button", { name: "Add ingredient" }))
  const inputs = [...view.container.querySelectorAll<HTMLInputElement>("form input")]
  expect(inputs).toHaveLength(5)
  for (const input of inputs) compose(input)
  publish({ type: "pantry.entry.updated", resource: { type: "pantry_entry", id: "someone-else" } })
  await waitFor(() => expect(requests.GET.mock.calls.length).toBeGreaterThan(1))
  for (const input of inputs) {
    expect(input.isConnected).toBe(true)
    expect(input.value).toBe("abc東京def")
  }
  expect(document.activeElement).toBe(inputs.at(-1))
  fireEvent.submit(view.container.querySelector("form")!)
  await waitFor(() => expect(requests.PUT).toHaveBeenCalled())
  const options = requests.PUT.mock.calls[0]?.[1] as unknown as {
    params: { header: object }
    body: { amount: string }
  }
  expect(options.params.header).toEqual({ "If-None-Match": "*" })
  expect(options.body.amount).toBe("abc東京def")
})

test("integration input composition survives typing and a rejected conditional save", async () => {
  requests.session.mockImplementation(
    async (_input?: unknown, init?: RequestInit) =>
      new Response(
        init?.method === "POST" ? JSON.stringify({ code: "webhook-revision-conflict" }) : "[]",
        {
          status: init?.method === "POST" ? 412 : 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
  )
  const view = render(() => <IntegrationsPage />)
  fireEvent.click(await view.findByRole("button", { name: "Add subscription" }))
  const name = view.getByLabelText("Name") as HTMLInputElement
  const url = view.getByLabelText("Destination URL") as HTMLInputElement
  compose(name)
  compose(url, "https://example.com/東京")
  fireEvent.click(view.getByLabelText("recipe.created"))
  fireEvent.submit(view.container.querySelector("form")!)
  await waitFor(() => expect(view.getByRole("alert")).toBeTruthy())
  expect(view.getByLabelText("Name")).toBe(name)
  expect(name.value).toBe("abc東京def")
  expect(view.getByLabelText("Destination URL")).toBe(url)
  expect(url.value).toBe("https://example.com/東京")
})

test("changing libraries clears integration drafts from the previous scope", async () => {
  requests.session.mockImplementation(
    async () => new Response("[]", { headers: { "Content-Type": "application/json" } }),
  )
  const view = render(() => <IntegrationsPage />)
  fireEvent.click(await view.findByRole("button", { name: "Add subscription" }))
  const input = view.getByRole("textbox", { name: "Name" })
  fireEvent.input(input, { target: { value: "Home receiver" } })
  requests.changeLibrary("other-library")
  await waitFor(() => expect(input.isConnected).toBe(false))
  expect(view.queryByRole("textbox", { name: "Name" })).toBeNull()
})
