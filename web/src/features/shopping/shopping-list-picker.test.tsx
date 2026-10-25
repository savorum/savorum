import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { client } from "@/api/client"

import { ShoppingListHeading } from "./shopping-list-picker"

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
    <ShoppingListHeading principal="p" library="l" value="party" canCreate onSelect={onSelect} />
  ))
  fireEvent.click(await screen.findByRole("button", { name: "Party" }))
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
  fireEvent.click(screen.getByRole("button", { name: "Party" }))
  const newItem = await screen.findByRole("menuitem", { name: "New list" })
  fireEvent.pointerDown(newItem)
  fireEvent.click(newItem)
  const create = await screen.findByRole("dialog")
  expect(within(create).queryByRole("alert")).toBeNull()
})

test("renaming updates the heading while the shopping store still has its previous name", async () => {
  const onSelect = vi.fn()
  const [name, setName] = createSignal("Party")
  render(() => (
    <ShoppingListHeading
      principal="p"
      library="l"
      value="party"
      name={name()}
      canCreate
      onSelect={onSelect}
    />
  ))
  fireEvent.click(await screen.findByRole("button", { name: "Party" }))
  const rename = await screen.findByRole("menuitem", { name: "Rename list" })
  fireEvent.pointerDown(rename)
  fireEvent.click(rename)
  const dialog = await screen.findByRole("dialog", { name: "Rename list" })
  const input = within(dialog).getByRole("textbox", { name: "List name" })
  fireEvent.input(input, { target: { value: "Dinner party" } })
  vi.mocked(client.PATCH).mockResolvedValueOnce({
    data: { id: "party", name: "Dinner party" },
    response: new Response(),
  } as Awaited<ReturnType<typeof client.PATCH>>)
  vi.mocked(client.GET).mockResolvedValueOnce({
    data: [{ id: "party", name: "Dinner party" }],
    response: new Response(),
  } as Awaited<ReturnType<typeof client.GET>>)
  fireEvent.submit(input.closest("form")!)
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  expect(screen.getByRole("button", { name: "Dinner party" })).toBeTruthy()
  expect(onSelect).toHaveBeenCalledWith("party")
  vi.mocked(client.GET).mockResolvedValueOnce({
    data: [{ id: "party", name: "Summer picnic" }],
    response: new Response(),
  } as Awaited<ReturnType<typeof client.GET>>)
  setName("Summer picnic")
  expect(await screen.findByRole("button", { name: "Summer picnic" })).toBeTruthy()
})
