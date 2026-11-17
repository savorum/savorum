import { cleanup, fireEvent, render, screen as portal } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { accountRequest } from "./account-state"
import { InvitedRegistration, appInvitationURL, invitationToken } from "./invitations"
import { InvitationLinkDialog } from "./library-sharing"
import { LinkedSignInMethods } from "./sign-in-methods"

vi.mock("@/api/capabilities", () => ({
  loadCapabilities: async () => ({
    auth_methods: [{ id: "company", kind: "ldap", name: "Company" }],
  }),
}))

vi.mock("./account-state", () => ({
  account: () => ({
    user: { id: "u1" },
    libraries: [{ id: "default", name: "Home", role: "owner" }],
  }),
  authEnabled: () => true,
  libraries: () => [{ id: "default", name: "Home", role: "owner" }],
  administers: () => false,
  roleName: () => "Viewer",
  roleItems: (...roles: string[]) => roles.map((role) => ({ value: role, label: role })),
  refreshAccount: vi.fn(),
  accountRequest: vi.fn(async (path: string) =>
    path === "/auth/providers"
      ? { providers: [{ id: "company", display_name: "Company", kind: "ldap" }] }
      : { invitations: [], methods: [], mappings: [] },
  ),
}))
afterEach(() => {
  cleanup()
  sessionStorage.clear()
})
function verify(input: HTMLInputElement) {
  input.focus()
  for (const value of ["a", "ab", "abc", "abcdef"]) {
    fireEvent.input(input, { target: { value } })
    expect(input.isConnected).toBe(true)
    expect(document.activeElement).toBe(input)
    expect(input.value).toBe(value)
  }
  input.setSelectionRange(3, 3)
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "abc東京def" }, isComposing: true })
  input.setSelectionRange(5, 5)
  fireEvent.compositionEnd(input, { data: "東京" })
  expect(document.activeElement).toBe(input)
  expect(input.selectionStart).toBe(5)
  expect(input.value).toBe("abc東京def")
}
test("invitation and registration inputs retain DOM identity, caret and composition", async () => {
  sessionStorage.setItem("savorum.invitation", "invitation-secret")
  const screen = render(() => (
    <>
      <InvitationLinkDialog libraries={[{ id: "default", name: "Home" }]} onClose={() => {}} />
      <InvitedRegistration />
    </>
  ))
  await screen.findByLabelText("Display name")
  await portal.findByLabelText("Who is it for? (optional)")
  for (const input of document.querySelectorAll<HTMLInputElement>(
    'input:not([type="number"]):not([readonly])',
  ))
    verify(input)
  const days = portal.getByLabelText("Expires after (days)") as HTMLInputElement
  days.focus()
  fireEvent.input(days, { target: { value: "14" } })
  expect(document.activeElement).toBe(days)
  expect(days.value).toBe("14")
})
/**
 * A chooser draws its list in a portal on `document.body`, outside the
 * container `render` scopes its queries to, so the options are looked for in
 * the document rather than in the tree the component rendered.
 */
test("provider link credentials retain focus and composition", async () => {
  const screen = render(() => <LinkedSignInMethods />)
  fireEvent.click(await screen.findByRole("combobox", { name: "Add a sign-in provider" }))
  fireEvent.click(await portal.findByRole("option", { name: "Company" }))
  verify((await screen.findByLabelText("Provider username")) as HTMLInputElement)
  verify(screen.getByLabelText("Provider password") as HTMLInputElement)
})

test("an invitation opens in the app with the token in the fragment", async () => {
  expect(appInvitationURL("https://recipes.example", "a b/c")).toBe(
    "savorum://invitation#server=https%3A%2F%2Frecipes.example&token=a%20b%2Fc",
  )
  sessionStorage.setItem("savorum.invitation", "token-token-token")
  const screen = render(() => <InvitedRegistration />)
  const link = await screen.findByRole("link", { name: "Open in the Savorum app" })
  expect(link.getAttribute("href")).toBe(
    appInvitationURL(window.location.origin, "token-token-token"),
  )
})

test("registration has one create action and retains entered values when switching to sign-in", async () => {
  sessionStorage.setItem("savorum.invitation", "invitation-secret")
  const screen = render(() => <InvitedRegistration signIn={<p>Existing account form</p>} />)
  await screen.findByRole("heading", { name: "Create your account to join" })
  expect(screen.getAllByRole("button", { name: /Create/ })).toHaveLength(1)
  const display = screen.getByLabelText("Display name") as HTMLInputElement
  verify(display)
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }))
  await screen.findByRole("heading", { name: "Sign in to join" })
  expect(screen.queryByLabelText("Display name")).toBeNull()
  expect(screen.getByText("Existing account form")).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Create an account" }))
  expect((screen.getByLabelText("Display name") as HTMLInputElement).value).toBe("abc東京def")
  expect(screen.getAllByRole("button", { name: /Create/ })).toHaveLength(1)
})

test("a created link replaces the form until another is started", async () => {
  vi.mocked(accountRequest)
    .mockImplementationOnce(async () => ({ providers: [] }))
    .mockImplementationOnce(async () => ({
      token: "short-secret-token-22c",
      url: "https://recipes.example/join#short-secret-token-22c",
    }))
  render(() => (
    <InvitationLinkDialog libraries={[{ id: "default", name: "Home" }]} onClose={() => {}} />
  ))
  fireEvent.click(await portal.findByRole("button", { name: "Create link" }))
  const link = (await portal.findByLabelText("Invitation link")) as HTMLInputElement
  expect(link.value).toBe("https://recipes.example/join#short-secret-token-22c")
  expect(portal.queryByLabelText("Who is it for? (optional)")).toBeNull()
  fireEvent.click(portal.getByRole("button", { name: "Create another link" }))
  expect(await portal.findByLabelText("Who is it for? (optional)")).toBeTruthy()
})

test("a join link stores its token and leaves the address bar", () => {
  history.replaceState(null, "", "/join#short-secret-token-22c")
  expect(invitationToken()).toBe("short-secret-token-22c")
  expect(window.location.hash).toBe("")
  expect(sessionStorage.getItem("savorum.invitation")).toBe("short-secret-token-22c")
  history.replaceState(null, "", "/")
})
