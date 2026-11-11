import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import type { JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { BookMembership } from "./book-membership"
import type { BookViewProps } from "./book-types"
import { createBookEditor } from "./create-book-editor"
vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { children: JSX.Element }) => <a>{props.children}</a>,
}))
vi.mock("@/api/selections", () => ({
  lookupRecipes: async () => ({ items: [], unresolved_ids: [] }),
}))
const replaceBook = vi.fn()
vi.mock("@/api/books", async (original) => ({
  ...(await original<typeof import("@/api/books")>()),
  replaceBook: (...args: unknown[]) => replaceBook(...args),
}))
afterEach(cleanup)
test("repeated book moves use the current neighbors and button states", () => {
  const props: BookViewProps = {
    book: {
      id: "book",
      title: "Book",
      kind: "personal",
      revision: 1,
      created_at: "",
      updated_at: "",
      sections: [],
      entries: ["A", "B", "C"].map((id, position) => ({
        library_entry_id: id,
        position,
        recipe_state: "active",
      })),
    },
    revision: 1,
    sourced: [],
    candidateQuery: "",
    onCandidateQuery: vi.fn(),
    onCandidatePage: vi.fn(),
    onReload: vi.fn(),
    onTrashed: vi.fn(async () => {}),
  }
  render(() => (
    <BookMembership {...props} editor={createBookEditor(props, () => {})} onAdd={() => {}} />
  ))
  const row = () => document.querySelector('[data-book-entry-id="B"]')!
  fireEvent.click(row().querySelectorAll("button")[0])
  expect((row().querySelectorAll("button")[0] as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(row().querySelectorAll("button")[1])
  expect(
    Array.from(document.querySelectorAll("[data-book-entry-id]")).map((node) =>
      node.getAttribute("data-book-entry-id"),
    ),
  ).toEqual(["A", "B", "C"])
})

test("a chapter without a name is not saved", async () => {
  const props: BookViewProps = {
    book: {
      id: "book",
      title: "Book",
      kind: "personal",
      revision: 1,
      created_at: "",
      updated_at: "",
      sections: [{ id: "chapter", title: "Starters", position: 0 }],
      entries: [],
    },
    revision: 1,
    sourced: [],
    candidateQuery: "",
    onCandidateQuery: vi.fn(),
    onCandidatePage: vi.fn(),
    onReload: vi.fn(),
    onTrashed: vi.fn(async () => {}),
  }
  const editor = createBookEditor(props, () => {})
  render(() => <BookMembership {...props} editor={editor} onAdd={() => {}} />)
  const name = document.querySelector<HTMLInputElement>("[data-chapter-name]")!
  fireEvent.input(name, { target: { value: "  " } })

  await editor.save()
  expect(replaceBook).not.toHaveBeenCalled()
  expect(document.activeElement).toBe(name)
})
