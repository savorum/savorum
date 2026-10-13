import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { client } from "@/api/client"

import { ShoppingListPicker } from "./shopping-list-picker"

vi.mock("@/api/client", async (original) => {
  const actual = await original<typeof import("@/api/client")>()
  return {
    ...actual,
    client: {
      GET: vi.fn(async () => ({
        data: [
          { id: "groceries", name: "Groceries" },
          { id: "party", name: "Party" },
        ],
        response: new Response(),
      })),
      DELETE: vi.fn(async () => ({
        error: { detail: "Someone else changed this list" },
        response: new Response(null, { status: 412 }),
      })),
      POST: vi.fn(),
      PATCH: vi.fn(),
    },
  }
})
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

test("a refused delete is reported in its confirmation and not carried into the next dialog", async () => {
  const onSelect = vi.fn()
  render(() => (
    <ShoppingListPicker principal="p" library="l" value="party" canCreate onSelect={onSelect} />
  ))
  fireEvent.click(await screen.findByRole("button", { name: "List actions" }))
  const deleteItem = await screen.findByRole("menuitem", { name: "Delete list" })
  fireEvent.pointerDown(deleteItem)
  fireEvent.click(deleteItem)
  const confirm = await screen.findByRole("dialog")
  fireEvent.click(within(confirm).getByRole("button", { name: "Delete list" }))
  expect(await within(confirm).findByRole("alert")).toBeTruthy()
  expect(client.DELETE).toHaveBeenCalledOnce()
  expect(onSelect).not.toHaveBeenCalled()

  fireEvent.click(within(confirm).getByRole("button", { name: "Cancel" }))
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  fireEvent.click(screen.getByRole("button", { name: "List actions" }))
  const newItem = await screen.findByRole("menuitem", { name: "New list" })
  fireEvent.pointerDown(newItem)
  fireEvent.click(newItem)
  const create = await screen.findByRole("dialog")
  expect(within(create).queryByRole("alert")).toBeNull()
})
