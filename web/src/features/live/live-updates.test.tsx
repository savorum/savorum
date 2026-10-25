import { cleanup, render, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { loadCapabilities } from "@/api/capabilities"
import { publish } from "@/api/live-events"
import { sessionFetch } from "@/api/session"

import { LiveUpdates } from "./live-updates"

vi.mock("@/api/capabilities", () => ({
  loadCapabilities: vi.fn(async () => ({ features: ["events.sse"] })),
}))
vi.mock("@/api/live-events", () => ({ publish: vi.fn() }))
vi.mock("@/api/session", () => ({ sessionFetch: vi.fn() }))
vi.mock("@/features/account/account-state", () => ({
  account: () => ({ user: { id: "cook" }, active_library_id: "home" }),
  sessionEnded: () => false,
}))

afterEach(cleanup)

/** A stream that stays open without sending anything. */
const openStream = () =>
  ({
    ok: true,
    headers: new Headers({ "Content-Type": "text/event-stream" }),
    body: {
      getReader: () => ({
        read: () => new Promise<never>(() => {}),
        cancel: async () => {},
        releaseLock: () => {},
      }),
    },
  }) as unknown as Response

test("focus and visibility leave an open event stream alone", async () => {
  vi.mocked(sessionFetch).mockImplementation(async () => openStream())
  render(() => <LiveUpdates />)
  await waitFor(() => expect(sessionFetch).toHaveBeenCalledTimes(1))

  for (let i = 0; i < 5; i++) {
    window.dispatchEvent(new Event("focus"))
    document.dispatchEvent(new Event("visibilitychange"))
  }
  await new Promise((resolve) => setTimeout(resolve, 20))

  expect(sessionFetch).toHaveBeenCalledTimes(1)
  expect(loadCapabilities).toHaveBeenCalledTimes(1)
  expect(publish).not.toHaveBeenCalled()
})

test("focus reconnects and refreshes when the stream is closed", async () => {
  vi.mocked(sessionFetch).mockRejectedValueOnce(new Error("offline"))
  vi.mocked(sessionFetch).mockImplementation(async () => openStream())
  render(() => <LiveUpdates />)
  await waitFor(() => expect(sessionFetch).toHaveBeenCalledTimes(1))

  window.dispatchEvent(new Event("focus"))

  await waitFor(() => expect(sessionFetch).toHaveBeenCalledTimes(2))
  expect(publish).toHaveBeenCalledTimes(1)
})

test("a quick return reconnects before the aborted stream finishes closing", async () => {
  vi.mocked(sessionFetch).mockImplementation(async () => openStream())
  render(() => <LiveUpdates />)
  await waitFor(() => expect(sessionFetch).toHaveBeenCalledTimes(1))
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden")
  document.dispatchEvent(new Event("visibilitychange"))
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible")
  document.dispatchEvent(new Event("visibilitychange"))
  await waitFor(() => expect(sessionFetch).toHaveBeenCalledTimes(2))
  window.dispatchEvent(new Event("focus"))
  expect(sessionFetch).toHaveBeenCalledTimes(2)
})
