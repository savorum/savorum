import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import type { JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { createBook, listBookPage } from "@/api/books"

import { BooksPage } from "./books-page"

vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { children?: JSX.Element }) => <a>{props.children}</a>,
}))
vi.mock("@/api/books", () => ({ createBook: vi.fn(), listBookPage: vi.fn() }))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

test("book naming keeps composition, focus and caret until a separate Enter submits", async () => {
  vi.mocked(listBookPage).mockResolvedValue({ items: [], total: 0, limit: 24, offset: 0 })
  const created = vi.fn()
  const screen = render(() => <BooksPage onCreated={created} />)
  fireEvent.click(screen.getByRole("button", { name: "New book" }))
  const input = screen.getByRole("textbox", { name: "Title" }) as HTMLInputElement
  input.focus()
  for (const value of ["に", "にほん"]) {
    fireEvent.input(input, { target: { value } })
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(value.length)
  }
  fireEvent.compositionStart(input)
  fireEvent.keyDown(input, { key: "Enter", isComposing: true })
  fireEvent.keyDown(input, { key: "Enter", keyCode: 229 })
  expect(createBook).not.toHaveBeenCalled()
  fireEvent.input(input, { target: { value: "日本料理" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "日本料理" })
  expect(screen.getByRole("textbox", { name: "Title" })).toBe(input)
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("日本料理")
  expect(input.selectionStart).toBe(4)
  vi.mocked(createBook).mockResolvedValue({
    value: {
      id: "book",
      title: "日本料理",
      kind: "personal",
      revision: 1,
      entries: [],
      sections: [],
      created_at: "2026-09-25T00:00:00Z",
      updated_at: "2026-09-25T00:00:00Z",
    },
    revision: 1,
  })
  fireEvent.keyDown(input, { key: "Enter" })
  await waitFor(() => expect(created).toHaveBeenCalledWith("book"))
  expect(createBook).toHaveBeenCalledWith(expect.objectContaining({ title: "日本料理" }))
})

test("a shelf names each book once, on its cover or below its picture", async () => {
  const book = {
    kind: "personal" as const,
    revision: 1,
    updated_at: "2026-09-25T00:00:00Z",
  }
  vi.mocked(listBookPage).mockResolvedValue({
    items: [
      { ...book, id: "plain", title: "Sunday baking", entry_count: 1 },
      {
        ...book,
        id: "preview",
        title: "Weekday meals",
        entry_count: 2,
        preview_asset_id: "photo",
        preview_entry_id: "recipe",
      },
      { ...book, id: "pictured", title: "Summer salads", entry_count: 3, cover_asset_id: "cover" },
    ],
    total: 3,
    limit: 24,
    offset: 0,
  })
  const screen = render(() => <BooksPage onCreated={vi.fn()} />)
  await screen.findByRole("heading", { name: "Sunday baking" })
  expect(screen.getAllByText("Sunday baking")).toHaveLength(1)
  expect(screen.getAllByText("Summer salads")).toHaveLength(1)
  expect(screen.getByText("1 recipe")).toBeTruthy()
  expect(screen.getByText("3 recipes")).toBeTruthy()
  expect(
    screen.container.querySelector(
      'img[src="/api/v1/recipes/recipe/assets/photo/derivatives/card"]',
    ),
  ).toBeTruthy()
})
