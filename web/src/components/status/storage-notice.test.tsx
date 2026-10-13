import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { sessionFetch } from "@/api/session"
import { setStorageStatus, storageSupportURL } from "@/lib/storage-status"

import { StorageContactSupport, StorageNotice } from "./storage-notice"

const request = vi.hoisted(() => vi.fn())
vi.mock("@/features/account/account-state", () => ({ accountRequest: request }))

afterEach(() => {
  cleanup()
  setStorageStatus(undefined)
  request.mockReset()
  vi.unstubAllGlobals()
})

test("one authenticated read warns at capacity without scheduling more reads", async () => {
  request.mockResolvedValue({
    state: "warning",
    hosted: true,
    support_url: "https://cloud.example/cloud/account/manage",
  })
  const screen = render(() => <StorageNotice />)
  const summary = await screen.findByText("Storage is nearing its limit")
  const trigger = screen.getByRole("button", { name: "Storage is nearing its limit" })
  expect(trigger.getAttribute("aria-expanded")).toBe("false")
  expect(summary.closest("details")).toBeNull()
  expect(trigger.querySelector("svg")).not.toBeNull()
  fireEvent.click(trigger)
  await waitFor(() => expect(trigger.getAttribute("aria-expanded")).toBe("true"))
  expect(screen.getByRole("link", { name: "Contact hosting support" }).getAttribute("href")).toBe(
    "https://cloud.example/cloud/account/manage",
  )
  expect(request).toHaveBeenCalledExactlyOnceWith("/storage-status")
})

test("unknown observation does not announce zero usage or refuse editing", async () => {
  request.mockRejectedValue(new Error("unavailable"))
  const screen = render(() => <StorageNotice />)
  await waitFor(() => expect(request).toHaveBeenCalledTimes(1))
  expect(screen.queryByRole("status")).toBeNull()
})

test("unsafe support destinations fall back to server administrator guidance", () => {
  for (const support_url of [
    "http://cloud.example/cloud/account/manage",
    "https://user@cloud.example/cloud/account/manage",
    "https://cloud.example/cloud/account/manage?next=bad",
    "https://cloud.example/untrusted",
  ]) {
    expect(storageSupportURL({ state: "limited", hosted: true, support_url })).toBeUndefined()
  }
  setStorageStatus({ state: "limited", hosted: false })
  const screen = render(() => <StorageContactSupport />)
  expect(screen.getByText(/Contact your server administrator/)).toBeTruthy()
  expect(screen.queryByRole("link")).toBeNull()
})

test("successful user writes and storage refusals refresh the observation", async () => {
  request
    .mockResolvedValueOnce({ state: "normal", hosted: false })
    .mockResolvedValueOnce({ state: "warning", hosted: false })
    .mockResolvedValueOnce({ state: "limited", hosted: false })
  const screen = render(() => <StorageNotice />)
  await waitFor(() => expect(request).toHaveBeenCalledTimes(1))
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValueOnce(new Response("{}", { status: 200 }))
      .mockResolvedValueOnce(new Response("{}", { status: 507 })),
  )
  await sessionFetch("/api/v1/assets", { method: "POST" })
  await screen.findByText("Storage is nearing its limit")
  await sessionFetch("/api/v1/assets", { method: "POST" })
  await screen.findByText("Storage has reached a safety limit")
  expect(request).toHaveBeenCalledTimes(3)
})
