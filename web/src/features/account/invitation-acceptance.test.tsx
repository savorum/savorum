import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import { afterEach, beforeEach, expect, test, vi } from "vitest"

import { m } from "../../lib/messages.ts"
import { accountRequest, refreshAccount } from "./account-state"
import { InvitationAcceptance } from "./invitations"

vi.mock("./account-state", () => ({
  account: () => ({ user: { id: "cook", display_name: "Cook" } }),
  roleName: () => "Viewer",
  roleItems: (...roles: string[]) => roles.map((role) => ({ value: role, label: role })),
  accountRequest: vi.fn(),
  refreshAccount: vi.fn(),
}))

let committed = false
let loseResponse = false
let commitOnFirstAttempt = true
let failReceiptRead = false
let attempts = 0
let receiptReads = 0

beforeEach(() => {
  committed = false
  loseResponse = false
  commitOnFirstAttempt = true
  failReceiptRead = false
  attempts = 0
  receiptReads = 0
  sessionStorage.setItem("savorum.invitation", "invitation-secret")
  vi.mocked(refreshAccount).mockReset().mockResolvedValue(undefined)
  vi.mocked(accountRequest)
    .mockReset()
    .mockImplementation(async (path) => {
      if (path === "/auth/providers") return { providers: [] }
      if (path === "/account/invitations/preview") {
        receiptReads++
        if (attempts > 0 && failReceiptRead) {
          failReceiptRead = false
          throw new Error("Receipt unavailable")
        }
        return {
          library_id: "family",
          library_name: "Family",
          role: "viewer",
          accepted: committed,
        }
      }
      if (path === "/account/invitations/accept") {
        attempts++
        committed = attempts > 1 || commitOnFirstAttempt
        if (loseResponse && attempts === 1) throw new Error("Response lost")
      }
      return {}
    })
})

afterEach(() => {
  cleanup()
  sessionStorage.clear()
})

test("a lost acceptance response resumes from the same-account receipt", async () => {
  loseResponse = true
  const screen = render(() => <InvitationAcceptance />)
  fireEvent.click(await screen.findByRole("button", { name: m.account_invitation_accept() }))
  await waitFor(() => expect(sessionStorage.getItem("savorum.invitation")).toBeNull())
  expect(attempts).toBe(1)
  expect(receiptReads).toBe(2)
  expect(refreshAccount).toHaveBeenCalledOnce()
})

test("retry checks an uncertain acceptance before sending another POST", async () => {
  loseResponse = true
  failReceiptRead = true
  const screen = render(() => <InvitationAcceptance />)
  fireEvent.click(await screen.findByRole("button", { name: m.account_invitation_accept() }))
  await screen.findByText("Receipt unavailable")
  expect(sessionStorage.getItem("savorum.invitation")).toBe("invitation-secret")
  fireEvent.click(screen.getByRole("button", { name: m.account_invitation_accept() }))
  await waitFor(() => expect(sessionStorage.getItem("savorum.invitation")).toBeNull())
  expect(attempts).toBe(1)
  expect(receiptReads).toBe(3)
})

test("an uncommitted acceptance can be sent again after checking its receipt", async () => {
  loseResponse = true
  commitOnFirstAttempt = false
  const screen = render(() => <InvitationAcceptance />)
  fireEvent.click(await screen.findByRole("button", { name: m.account_invitation_accept() }))
  await screen.findByText("Response lost")
  fireEvent.click(screen.getByRole("button", { name: m.account_invitation_accept() }))
  await waitFor(() => expect(sessionStorage.getItem("savorum.invitation")).toBeNull())
  expect(attempts).toBe(2)
  expect(receiptReads).toBe(3)
})

test("a failed final refresh keeps its error and Open Library retry visible", async () => {
  vi.mocked(refreshAccount).mockRejectedValueOnce(new Error("Refresh unavailable"))
  const screen = render(() => <InvitationAcceptance />)
  fireEvent.click(await screen.findByRole("button", { name: m.account_invitation_accept() }))
  await screen.findByText("Refresh unavailable")
  expect(sessionStorage.getItem("savorum.invitation")).toBe("invitation-secret")
  fireEvent.click(screen.getByRole("button", { name: m.account_invitation_open_library() }))
  await waitFor(() => expect(sessionStorage.getItem("savorum.invitation")).toBeNull())
  expect(attempts).toBe(1)
  expect(refreshAccount).toHaveBeenCalledTimes(2)
})
