import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { deleteCalendarFeed, listCalendarFeeds } from "@/api/calendar-feeds"
import { ApiError } from "@/api/client"

import { CalendarFeeds } from "./calendar-feeds"

vi.mock("@/api/calendar-feeds", () => ({
  listCalendarFeeds: vi.fn(async () => [
    { id: "feed", name: "Kitchen", mine: true, created_by_name: "Me", created_at: "" },
  ]),
  createCalendarFeed: vi.fn(),
  rotateCalendarFeed: vi.fn(),
  deleteCalendarFeed: vi.fn(async () => {
    throw new ApiError("The subscription could not be revoked", 409)
  }),
  feedAddress: () => "",
}))
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

test("a refused revoke is reported in its confirmation and the list is read again", async () => {
  render(() => <CalendarFeeds onClose={() => {}} />)
  fireEvent.click(await screen.findByRole("button", { name: "Revoke" }))
  const confirm = (await screen.findAllByRole("dialog")).at(-1)!
  fireEvent.click(within(confirm).getByRole("button", { name: "Revoke" }))
  expect(await within(confirm).findByText("The subscription could not be revoked")).toBeTruthy()
  expect(deleteCalendarFeed).toHaveBeenCalledWith("feed")
  await waitFor(() => expect(listCalendarFeeds).toHaveBeenCalledTimes(2))
})

test("a failed load is reported without taking the page down", async () => {
  vi.mocked(listCalendarFeeds).mockRejectedValueOnce(new Error("offline"))
  render(() => <CalendarFeeds onClose={() => {}} />)
  expect(await screen.findByText("Calendar subscriptions could not be loaded.")).toBeTruthy()
})
