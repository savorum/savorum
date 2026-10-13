import {
  cleanup,
  fireEvent,
  render,
  screen as portal,
  waitFor,
  within,
} from "@solidjs/testing-library"
import type { JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { AccountOverview } from "./account-overview"
import { AdminLibrariesPage } from "./admin-libraries-page"
import { AdminPeoplePage } from "./admin-people-page"
import { AdminProvidersPage } from "./admin-providers-page"
import { LibraryAccess } from "./library-access-page"
import { Login } from "./login-page"
import { SecurityPage } from "./security-page"
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
    accountRequest: vi.fn(async (path: string) => {
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
  state.auth_enabled = true
  state.user.instance_admin = true
})

async function verifyInputs(container: HTMLElement) {
  for (const input of container.querySelectorAll<HTMLInputElement>(
    'input:not([type="datetime-local"]):not([type="checkbox"]):not([type="number"]):not([readonly])',
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
