import {
  cleanup,
  fireEvent,
  render,
  screen as portal,
  waitFor,
  within,
} from "@solidjs/testing-library"
import { createSignal, type JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { AccountOverview } from "./account-overview"
import { AdminConnectPage } from "./admin-connect-page"
import { AdminLibrariesPage } from "./admin-libraries-page"
import { AdminPeoplePage } from "./admin-people-page"
import { AdminProvidersPage } from "./admin-providers-page"
import { AssistantApproval } from "./assistant-approval"
import { AssistantPanel } from "./assistant-panel"
import { LibraryAccess } from "./library-access-page"
import { Login } from "./login-page"
import { SecurityPage } from "./security-page"
import { SignOutDialog } from "./sign-out-dialog"
import { TokensPage } from "./token-panel"

const state = vi.hoisted(() => ({
  auth_enabled: true,
  user: { id: "u1", display_name: "Alex", instance_admin: true },
  libraries: [{ id: "l1", name: "Home", role: "owner" }],
  active_library_id: "l1",
}))

vi.mock("@/api/capabilities", () => ({
  loadCapabilities: async () => ({
    auth_methods: [{ id: "directory", kind: "ldap", name: "Company directory" }],
  }),
}))

vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { children?: JSX.Element }) => <a href="/account">{props.children}</a>,
  useParams: () => () => ({ libraryId: "l1" }),
  useRouter: () => undefined,
}))

vi.mock("./account-state", async () => {
  const messages = await import("../../lib/messages.ts")
  return {
    account: () => state,
    authEnabled: () => state.auth_enabled,
    libraries: () => state.libraries,
    libraryByID: (id: string) => state.libraries.find((library) => library.id === id),
    administers: () => state.auth_enabled && state.user.instance_admin,
    roleName: () => messages.m.account_role_owner(),
    refreshAccount: vi.fn(),
    accountRequest: vi.fn(async (path: string, method?: string) => {
      if (path === "/account/connections") return { connections: [] }
      if (path.startsWith("/account/authorization?"))
        return {
          client_name: "Assistant",
          scope: "read write",
          user_code: "ABC123",
          remember_days: 90,
          verification_required: true,
        }
      if (path === "/account/authorization") return { redirect_uri: "" }
      if (path === "/account/tokens" && method === "POST")
        return { secret: "svt_example", expires_at: 1800000000 }
      if (path === "/instance/connect")
        return { configured: false, state: "disabled", certificate_ready: false }
      if (path.includes("/audit?")) return { events: [] }
      if (path.endsWith("/sessions")) return { sessions: [] }
      if (path.endsWith("/members"))
        return { members: [{ user_id: "u2", display_name: "Sam", role: "viewer" }] }
      if (path.endsWith("/tokens"))
        return {
          tokens: [],
          policy: { default_days: 90, maximum_days: 365, allow_nonexpiring: false },
        }
      if (path.endsWith("/methods"))
        return {
          methods: [{ id: "local", provider: "local", name: "Local password", enabled: true }],
        }
      if (path.endsWith("/passkeys"))
        return {
          passkeys: [
            {
              id: "key",
              name: "Personal key",
              created_at: "2026-09-08T00:00:00Z",
              last_used_at: null,
            },
          ],
          available: true,
        }
      if (path.endsWith("/recovery")) return { remaining: 0, allowed: true }
      if (path.endsWith("/invitations")) return { invitations: [] }
      if (path.endsWith("/group-mappings")) return { mappings: [] }
      if (path.endsWith("/dependencies")) return { users: [] }
      if (path.endsWith("/auth/providers"))
        return {
          providers: [{ id: "directory", kind: "ldap", display_name: "Company directory" }],
        }
      return {
        users: [
          { id: "u2", display_name: "Sam", instance_admin: false },
          { id: "u3", display_name: "Robin", instance_admin: false },
        ],
      }
    }),
  }
})

afterEach(() => {
  cleanup()
  window.history.replaceState({}, "", "/")
  vi.clearAllMocks()
  state.auth_enabled = true
  state.user.instance_admin = true
  state.libraries = [{ id: "l1", name: "Home", role: "owner" }]
  state.active_library_id = "l1"
})

async function verifyInputs(container: HTMLElement) {
  for (const input of container.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>(
    'input:not([type="datetime-local"]):not([type="checkbox"]):not([type="number"]):not([readonly]), textarea:not([readonly])',
  )) {
    await waitFor(() => {
      input.focus()
      expect(document.activeElement).toBe(input)
    })
    for (const value of ["a", "abcdef"]) {
      fireEvent.input(input, { target: { value } })
      expect(input.isConnected).toBe(true)
      expect(document.activeElement).toBe(input)
      expect(input.value).toBe(value)
    }
    input.setSelectionRange(3, 3)
    expect(input.selectionStart).toBe(3)
    fireEvent.compositionStart(input)
    fireEvent.input(input, { target: { value: "abc東京def" }, isComposing: true })
    input.setSelectionRange(5, 5)
    fireEvent.compositionEnd(input, { data: "東京" })
    expect(input.isConnected).toBe(true)
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(5)
    expect(input.value).toBe("abc東京def")
  }
}

test("login inputs retain identity, focus, caret, and composed text", async () => {
  const screen = render(() => <Login />)
  await screen.findByLabelText("Directory username")
  for (const button of screen.container.querySelectorAll<HTMLButtonElement>(
    'button[aria-expanded="false"]',
  )) {
    fireEvent.click(button)
  }
  await verifyInputs(screen.container)
})

test("security inputs retain identity, focus, caret, and composed text", async () => {
  const screen = render(() => <SecurityPage />)
  fireEvent.click(await screen.findByRole("button", { name: /Personal key/ }))
  await screen.findByLabelText("Name for Personal key")
  await verifyInputs(screen.container)
})

test("library access inputs retain identity, focus, caret, and composed text", async () => {
  const screen = render(() => <LibraryAccess libraryID="l1" />)
  await screen.findByLabelText("Private label (optional)")
  await verifyInputs(screen.container)
})

test("administration inputs retain identity, focus, caret, and composed text", async () => {
  const people = render(() => <AdminPeoplePage />)
  await people.findByLabelText("New password for Sam")
  await verifyInputs(people.container)
  const libraries = render(() => <AdminLibrariesPage />)
  await libraries.findByLabelText("Library identifier")
  await verifyInputs(libraries.container)
  const providers = render(() => <AdminProvidersPage />)
  await providers.findByLabelText("Exact external group")
  await verifyInputs(providers.container)
})

test("token inputs retain identity, focus, caret, and composed text", async () => {
  const screen = render(() => <TokensPage />)
  await screen.findByLabelText("Token name")
  await verifyInputs(screen.container)
})

test("the setup prompt contains no secret and creates no credential", async () => {
  const { accountRequest } = await import("./account-state")
  const screen = render(() => <AssistantPanel />)
  const prompt = await screen.findByLabelText("Prompt for your assistant")
  expect((prompt as HTMLInputElement).value).toContain("/llms.txt")
  expect((prompt as HTMLInputElement).value).not.toMatch(/Bearer|svt_example|savorum_pat_/)
  expect(vi.mocked(accountRequest).mock.calls.some((call) => call[1] === "POST")).toBe(false)
})

async function chooseApproval(name: string, option: string) {
  fireEvent.click(await portal.findByRole("combobox", { name }))
  fireEvent.click(await portal.findByRole("option", { name: option }))
}

test("approval defaults to one library and read-only access", async () => {
  window.history.replaceState({}, "", "/account/authorize?code=ABC123")
  const { accountRequest } = await import("./account-state")
  const screen = render(() => <AssistantApproval />)
  fireEvent.click(await screen.findByRole("button", { name: "Allow access" }))
  await screen.findByText("You can return to your assistant.")
  expect(accountRequest).toHaveBeenCalledWith("/account/authorization", "POST", {
    code: "ABC123",
    library_id: "l1",
    access: "read",
    remember: false,
    deny: false,
  })
})

test("viewer library resets approval to read-only and keeps the selected library", async () => {
  window.history.replaceState({}, "", "/account/authorize?code=ABC123")
  state.libraries.push({ id: "l2", name: "Shared", role: "viewer" })
  const { accountRequest } = await import("./account-state")
  const screen = render(() => <AssistantApproval />)
  await chooseApproval("Permissions", "Read and write")
  await chooseApproval("Library", "Shared")
  expect(screen.getByRole("combobox", { name: "Permissions" }).textContent).toContain("Read only")
  expect(screen.getByText("You have read-only access to this library.")).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Allow access" }))
  await screen.findByText("You can return to your assistant.")
  expect(accountRequest).toHaveBeenCalledWith(
    "/account/authorization",
    "POST",
    expect.objectContaining({ library_id: "l2", access: "read" }),
  )
})

test("failed approval preserves choices and retries only on request", async () => {
  window.history.replaceState({}, "", "/account/authorize?code=ABC123")
  const { accountRequest } = await import("./account-state")
  const screen = render(() => <AssistantApproval />)
  await chooseApproval("Permissions", "Read and write")
  fireEvent.click(screen.getByRole("checkbox", { name: "Remember this connection" }))
  vi.mocked(accountRequest).mockRejectedValueOnce(new Error("Connection interrupted"))
  fireEvent.click(screen.getByRole("button", { name: "Allow access" }))
  await screen.findByText("Connection interrupted")
  expect(screen.getByRole("combobox", { name: "Permissions" }).textContent).toContain(
    "Read and write",
  )
  fireEvent.click(screen.getByRole("button", { name: "Allow access" }))
  await screen.findByText("You can return to your assistant.")
  expect(accountRequest).toHaveBeenLastCalledWith(
    "/account/authorization",
    "POST",
    expect.objectContaining({ access: "write", remember: true }),
  )
})

test("an older session signs in before approval instead of failing after confirmation", async () => {
  window.history.replaceState({}, "", "/account/authorize?code=ABCDEF123456")
  const { accountRequest } = await import("./account-state")
  vi.mocked(accountRequest).mockResolvedValueOnce({
    client_name: "Assistant",
    scope: "read",
    user_code: "ABCDEF123456",
    remember_days: 90,
    verification_required: true,
    reauth_required: true,
  })
  const screen = render(() => <AssistantApproval />)
  await screen.findByLabelText("Password")
  expect(screen.queryByRole("button", { name: "Allow access" })).toBeNull()
})

test("approval code retains identity, focus, caret and composed text", async () => {
  const screen = render(() => <AssistantApproval />)
  await screen.findByLabelText("Connection code")
  await verifyInputs(screen.container)
})

test("a library owner chooses a person by name rather than typing an identifier", async () => {
  const screen = render(() => <LibraryAccess libraryID="l1" />)
  fireEvent.click(await screen.findByRole("combobox", { name: "Person" }))
  // The list is drawn in a portal, so it is looked for in the document.
  const listbox = await portal.findByRole("listbox")
  await within(listbox).findByRole("option", { name: "Robin" })
  const offered = within(listbox)
    .getAllByRole("option")
    .map((option) => option.textContent)
  expect(offered).not.toContain("Sam")
  expect(screen.queryByLabelText("Savorum user ID")).toBeNull()
})

test("an owner without administration adds a member by identifier", async () => {
  state.user.instance_admin = false
  const screen = render(() => <LibraryAccess libraryID="l1" />)
  await screen.findByLabelText("Savorum user ID")
  expect(screen.queryByLabelText("Person")).toBeNull()
})

test("local mode hides sign-in, token, access and administration controls", async () => {
  state.auth_enabled = false
  const overview = render(() => <AccountOverview />)
  await overview.findByText("Preferences")
  expect(overview.queryByText("Sign out")).toBeNull()
  expect(overview.queryByText("Savorum user ID")).toBeNull()
  expect(overview.queryByText("Sign-in and security")).toBeNull()
  const security = render(() => <SecurityPage />)
  expect(security.queryByLabelText("Current password")).toBeNull()
  const tokens = render(() => <TokensPage />)
  expect(tokens.queryByLabelText("Token name")).toBeNull()
  const access = render(() => <LibraryAccess libraryID="l1" />)
  expect(access.queryByLabelText("Private label (optional)")).toBeNull()
  const people = render(() => <AdminPeoplePage />)
  expect(people.queryByLabelText("Login name")).toBeNull()
  expect(
    people.getByText(
      "Instance administration is available to administrators when authentication is enabled.",
    ),
  ).toBeTruthy()
})

test("administration stays hidden from a member who is not an administrator", () => {
  state.user.instance_admin = false
  const people = render(() => <AdminPeoplePage />)
  expect(people.queryByLabelText("Login name")).toBeNull()
  expect(people.queryByRole("navigation")).toBeNull()
})

test("Connect setup retains its input while typing and composing", async () => {
  const page = render(() => <AdminConnectPage />)
  const input = page.getByLabelText("Setup document") as HTMLTextAreaElement
  input.focus()
  for (const value of ["{", '{"', '{"version', '{"version": 1}']) {
    fireEvent.input(input, { target: { value } })
    expect(page.getByLabelText("Setup document")).toBe(input)
    expect(document.activeElement).toBe(input)
    expect(input.value).toBe(value)
  }
  input.setSelectionRange(3, 3)
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "setup 日本語" }, isComposing: true })
  input.setSelectionRange(9, 9)
  fireEvent.compositionEnd(input, { data: "日本語" })
  expect(page.getByLabelText("Setup document")).toBe(input)
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("setup 日本語")
  expect(input.selectionStart).toBe(9)
})

test("inactive tokens use bounded history pages", async () => {
  const { accountRequest } = await import("./account-state")
  vi.mocked(accountRequest).mockResolvedValueOnce({
    tokens: Array.from({ length: 25 }, (_, i) => ({
      id: String(i),
      name: `Old token ${i}`,
      scopes: ["recipes:read"],
      libraries: [],
      created_at: 1,
      expires_at: 0,
      last_used_at: 0,
      revoked_at: 100 + i,
    })),
    policy: { default_days: 90, maximum_days: 365, allow_nonexpiring: false },
  })
  const screen = render(() => <TokensPage />)
  fireEvent.click(await screen.findByRole("button", { name: "Revoked (25)" }))
  expect(screen.getAllByText(/^Old token/)).toHaveLength(10)
  expect(screen.getByText("Old token 24")).toBeTruthy()
  fireEvent.click(await screen.findByRole("button", { name: "Next" }))
  expect(screen.queryByText("Old token 24")).toBeNull()
  fireEvent.click(await screen.findByRole("button", { name: "Next" }))
  expect(screen.getAllByText(/^Old token/)).toHaveLength(5)
  expect(screen.getByRole("button", { name: "Next" }).hasAttribute("disabled")).toBe(true)
})

test("connection load failure keeps the setup prompt and retry available", async () => {
  const { accountRequest } = await import("./account-state")
  vi.mocked(accountRequest).mockRejectedValueOnce(new Error("Connection interrupted"))
  const screen = render(() => <AssistantPanel />)
  await screen.findByText("Connection interrupted")
  expect(screen.getByLabelText("Prompt for your assistant")).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Retry" }))
  await waitFor(() => expect(screen.queryByText("Connection interrupted")).toBeNull())
})

test("approval lookup failure keeps its retry control usable", async () => {
  window.history.replaceState({}, "", "/account/authorize?code=ABCDEF123456")
  const { accountRequest } = await import("./account-state")
  vi.mocked(accountRequest).mockRejectedValueOnce(new Error("Connection interrupted"))
  const screen = render(() => <AssistantApproval />)
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }))
  await screen.findByRole("button", { name: "Allow access" })
})

test("a mistyped approval code can be corrected without reloading", async () => {
  const { accountRequest } = await import("./account-state")
  const screen = render(() => <AssistantApproval />)
  const code = screen.getByLabelText("Connection code")
  expect(accountRequest).not.toHaveBeenCalled()
  vi.mocked(accountRequest).mockRejectedValueOnce(new Error("Unknown code"))
  fireEvent.input(code, { target: { value: "MISTYPED" } })
  fireEvent.click(screen.getByRole("button", { name: "Continue" }))
  fireEvent.click(await screen.findByRole("button", { name: "Connection code" }))
  const corrected = screen.getByLabelText("Connection code") as HTMLInputElement
  expect(corrected.value).toBe("MISTYPED")
  fireEvent.input(corrected, { target: { value: "ABC123" } })
  fireEvent.click(screen.getByRole("button", { name: "Continue" }))
  await screen.findByRole("button", { name: "Allow access" })
  expect(accountRequest).toHaveBeenLastCalledWith("/account/authorization?code=ABC123")
})

test("connections with nullable restrictions still render", async () => {
  const { accountRequest } = await import("./account-state")
  vi.mocked(accountRequest).mockResolvedValueOnce({
    connections: [
      {
        id: "assistant",
        name: "Assistant",
        scopes: null,
        libraries: null,
        expires_at: 1800000000,
        last_used_at: 0,
      },
    ],
  })
  const screen = render(() => <AssistantPanel />)
  await screen.findByRole("heading", { name: "Assistant", level: 4 })
  expect(screen.getByRole("button", { name: "Disconnect" })).toBeTruthy()
})

test("sign-out stays open while pending and clears an error when closed", async () => {
  const { accountRequest } = await import("./account-state")
  let reject!: (error: Error) => void
  vi.mocked(accountRequest).mockImplementationOnce(
    () =>
      new Promise<never>((_, fail) => {
        reject = fail
      }),
  )
  const [open, setOpen] = createSignal(true)
  const closed = vi.fn(() => setOpen(false))
  render(() => <SignOutDialog open={open()} onClose={closed} />)
  fireEvent.click(await portal.findByRole("button", { name: "Sign out" }))
  fireEvent.click(portal.getByRole("button", { name: "Cancel" }))
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" })
  expect(closed).not.toHaveBeenCalled()
  reject(new Error("Sign-out interrupted"))
  await portal.findByText("Sign-out interrupted")
  fireEvent.click(portal.getByRole("button", { name: "Cancel" }))
  await waitFor(() => expect(closed).toHaveBeenCalledTimes(1))
  setOpen(true)
  await portal.findByRole("button", { name: "Sign out" })
  expect(portal.queryByText("Sign-out interrupted")).toBeNull()
})
