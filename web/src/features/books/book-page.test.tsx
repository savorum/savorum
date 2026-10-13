import { fireEvent, render, waitFor } from "@solidjs/testing-library"
import type { JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { getBook, listBookSourceRecipes, replaceBook } from "@/api/books"
import { ApiError, type Book } from "@/api/client"
import { suggestRecipes } from "@/api/selections"

import { BookPage } from "./book-page"

vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { children?: JSX.Element }) => <a>{props.children}</a>,
  useRouter: () => undefined,
}))
vi.mock("@/api/books", async (original) => ({
  ...(await original<typeof import("@/api/books")>()),
  getBook: vi.fn(),
  listBookSourceRecipes: vi.fn(),
  replaceBook: vi.fn(),
}))
vi.mock("@/api/selections", () => ({
  suggestRecipes: vi.fn(),
  lookupRecipes: vi.fn(async () => ({ items: [], unresolved_ids: [] })),
}))
afterEach(() => vi.resetAllMocks())

test("conflict reload replaces the displayed draft and revision before another save", async () => {
  const initial: Book = {
    id: "book",
    title: "Original",
    kind: "personal",
    revision: 7,
    created_at: "",
    updated_at: "",
    entries: [],
    sections: [],
  }
  vi.mocked(getBook).mockResolvedValueOnce({ value: initial, revision: 7 })
  vi.mocked(listBookSourceRecipes).mockResolvedValue([])
  vi.mocked(suggestRecipes).mockResolvedValue({ items: [], has_more: false, limit: 10, offset: 0 })
  const screen = render(() => (
    <BookPage bookID={() => "book"} onTrashed={() => Promise.resolve()} />
  ))
  const editDetails = () => {
    fireEvent.click(screen.getByRole("button", { name: "Organize book" }))
    fireEvent.click(screen.getByRole("button", { name: "Book details" }))
  }
  try {
    const organize = await screen.findByRole("button", { name: "Organize book" }, { interval: 10 })
    fireEvent.click(organize)
    fireEvent.click(screen.getByRole("button", { name: "Book details" }))
    const title = screen.getByRole("textbox", { name: "Title" }) as HTMLInputElement
    title.focus()
    expect(document.activeElement).toBe(title)
    for (const value of ["M", "My draft"]) {
      fireEvent.input(title, { target: { value } })
      expect(title.isConnected).toBe(true)
      expect(document.activeElement).toBe(title)
      expect(title.selectionStart).toBe(value.length)
      expect(title.value).toBe(value)
    }
    expect(screen.getByRole("textbox", { name: "Title" })).toBe(title)
    fireEvent.compositionStart(title)
    fireEvent.input(title, { target: { value: "My draft 日本" }, isComposing: true })
    fireEvent.compositionEnd(title, { data: "日本" })
    expect(document.activeElement).toBe(title)
    expect(title.value).toBe("My draft 日本")
    vi.mocked(replaceBook).mockRejectedValueOnce(new ApiError("Conflict", 412))
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }))
    const reload = await screen.findByRole(
      "button",
      { name: "Reload the current version" },
      { interval: 10 },
    )
    expect(title.value).toBe("My draft 日本")
    const concurrent = {
      ...initial,
      title: "Concurrent title",
      description: "Their change",
      revision: 8,
    }
    vi.mocked(getBook).mockResolvedValueOnce({ value: concurrent, revision: 8 })
    fireEvent.click(reload)
    await screen.findByRole("heading", { name: "Concurrent title" }, { interval: 10 })
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull()
    editDetails()
    const newTitle = screen.getByRole("textbox", { name: "Title" })
    expect((newTitle as HTMLInputElement).value).toBe("Concurrent title")
    fireEvent.input(newTitle, { target: { value: "Revised title" } })
    vi.mocked(replaceBook).mockResolvedValueOnce({
      value: { ...concurrent, title: "Revised title" },
      revision: 9,
    })
    const saveButton = screen.getByRole("button", { name: "Save changes" })
    fireEvent.click(saveButton)
    await waitFor(() => expect(saveButton.isConnected).toBe(false), { interval: 10 })
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull()
    expect(replaceBook).toHaveBeenLastCalledWith(
      "book",
      8,
      expect.objectContaining({ title: "Revised title", description: "Their change" }),
    )
    expect(screen.getByRole("textbox", { name: "Title" })).toBe(newTitle)
  } finally {
    screen.unmount()
  }
})
