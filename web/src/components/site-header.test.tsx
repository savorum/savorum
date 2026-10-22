import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library"
import type { JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { SiteHeader } from "./site-header"

const state = vi.hoisted(() => ({ enabled: true }))
vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { children?: JSX.Element }) => <a href="/">{props.children}</a>,
  useNavigate: () => vi.fn(),
  useLocation: () => () => ({ pathname: "/" }),
}))
vi.mock("@/features/account/account-state", () => ({
  account: () => ({ user: { id: "u1" }, libraries: [] }),
  authEnabled: () => state.enabled,
  administers: () => false,
  accountRequest: vi.fn(),
}))
vi.mock("@/features/cooking/resume-cooking", () => ({ ResumeCooking: () => null }))
vi.mock("@/features/cooking/timer-status", () => ({ TimerStatus: () => null }))
vi.mock("@/features/account/sign-out-dialog", () => ({
  SignOutDialog: (props: { open: boolean }) => <span>{props.open ? "Confirm sign out" : ""}</span>,
}))
afterEach(() => {
  cleanup()
  state.enabled = true
})

test("the menu opens sign-out confirmation for authenticated accounts", async () => {
  render(() => <SiteHeader />)
  fireEvent.click(screen.getByRole("button", { name: /more/i }))
  const signOut = await screen.findByRole("menuitem", { name: "Sign out" })
  expect(screen.getAllByRole("menuitem").at(-1)).toBe(signOut)
  fireEvent.click(signOut)
  expect(await screen.findByText("Confirm sign out")).toBeTruthy()
})

test("local mode has no sign-out action", async () => {
  state.enabled = false
  render(() => <SiteHeader />)
  fireEvent.click(screen.getByRole("button", { name: /more/i }))
  await screen.findByRole("menu")
  expect(screen.queryByRole("menuitem", { name: "Sign out" })).toBeNull()
})
