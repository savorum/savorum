import { cleanup, fireEvent, render, screen as portal } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { InvitedRegistration, LibraryInvitations, appInvitationURL } from "./invitations"
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
      <LibraryInvitations libraryID="default" libraryName="Home" />
      <InvitedRegistration />
    </>
  ))
  await screen.findByLabelText("Display name")
  for (const input of screen.container.querySelectorAll<HTMLInputElement>(
    'input:not([type="number"]):not([readonly])',
  ))
    verify(input)
  const days = screen.getByLabelText("Expires after (days)") as HTMLInputElement
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
  expect(await screen.findByRole("button", { name: "Open in Savorum" })).toBeTruthy()
})
