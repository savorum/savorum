import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { LabelChooser } from "./label-choices"

afterEach(cleanup)
test("remote suggestions preserve the search input through typing, composition, and empty results", async () => {
  const load = vi.fn(async () => ({
    items: [] as { id: string; label: string }[],
    has_more: false,
  }))
  render(() => (
    <LabelChooser
      action="Add book"
      searchLabel="Search books"
      values={[]}
      describe={(value) => value.label}
      exhausted="No books"
      load={load}
      onChoose={() => {}}
    />
  ))
  fireEvent.click(screen.getByRole("button", { name: "Add book" }))
  const input = (await screen.findByRole("searchbox", { name: "Search books" })) as HTMLInputElement
  input.focus()
  fireEvent.input(input, { target: { value: "Sou" } })
  fireEvent.input(input, { target: { value: "Soup" } })
  input.setSelectionRange(2, 2)
  await waitFor(() => expect(load).toHaveBeenCalledWith("Soup", 0))
  expect(screen.getByRole("searchbox", { name: "Search books" })).toBe(input)
  expect(document.activeElement).toBe(input)
  expect(input.selectionStart).toBe(2)
  expect(input.value).toBe("Soup")
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "料理" }, isComposing: true })
  fireEvent.compositionEnd(input)
  await waitFor(() => expect(load).toHaveBeenCalledWith("料理", 0))
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("料理")
})

test("late suggestion responses cannot replace the current query", async () => {
  type Page = { items: { id: string; label: string }[]; has_more: boolean }
  const replies = new Map<string, (page: Page) => void>()
  const load = (query: string) => new Promise<Page>((resolve) => replies.set(query, resolve))
  render(() => (
    <LabelChooser
      action="Add book"
      searchLabel="Search books"
      values={[]}
      describe={(value) => value.label}
      exhausted="No books"
      load={load}
      onChoose={() => {}}
    />
  ))
  fireEvent.click(screen.getByRole("button", { name: "Add book" }))
  const input = await screen.findByRole("searchbox", { name: "Search books" })
  fireEvent.input(input, { target: { value: "old" } })
  await waitFor(() => expect(replies.has("old")).toBe(true))
  fireEvent.input(input, { target: { value: "new" } })
  await waitFor(() => expect(replies.has("new")).toBe(true))
  replies.get("new")!({ items: [{ id: "new", label: "Current book" }], has_more: false })
  await screen.findByRole("button", { name: "Current book" })
  replies.get("old")!({ items: [{ id: "old", label: "Stale book" }], has_more: false })
  await waitFor(() => expect(screen.queryByRole("button", { name: "Stale book" })).toBeNull())
  expect(screen.getByRole("button", { name: "Current book" })).toBeTruthy()
})
