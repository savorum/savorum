import { renderHook } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { bookInput, replaceBook } from "@/api/books"
import type { Book, Versioned } from "@/api/client"
import { deferred } from "@/test/import-fixtures"

import type { BookViewProps } from "./book-types"
import { createBookEditor } from "./create-book-editor"
vi.mock("@/api/books", async (original) => ({
  ...(await original<typeof import("@/api/books")>()),
  replaceBook: vi.fn(),
}))
vi.mock("@/api/selections", () => ({
  lookupRecipes: vi.fn(async () => ({ items: [], unresolved_ids: [] })),
}))
test("book ordering stays within chapters and a failed save retains the draft and revision", async () => {
  const props: BookViewProps = {
    book: {
      id: "book",
      title: "Book",
      kind: "personal",
      revision: 7,
      created_at: "",
      updated_at: "",
      sections: [{ id: "chapter", title: "Dinner", position: 3 }],
      entries: [
        { library_entry_id: "one", section_id: "chapter", position: 2, recipe_state: "active" },
        { library_entry_id: "loose", position: 4, recipe_state: "active" },
        { library_entry_id: "two", section_id: "chapter", position: 5, recipe_state: "active" },
      ],
    },
    revision: 7,
    sourced: [],
    candidateQuery: "",
    onCandidateQuery: vi.fn(),
    onCandidatePage: vi.fn(),
    onReload: vi.fn(),
    onTrashed: vi.fn(async () => {}),
  }
  const focus = vi.fn()
  const hook = renderHook(() => createBookEditor(props, focus))
  try {
    const editor = hook.result
    editor.moveWithin(editor.grouped().groups[0].entries, "one", 1)
    expect(focus).toHaveBeenCalledWith("one")
    expect(editor.dirty()).toBe(true)
    vi.mocked(replaceBook).mockRejectedValueOnce(new Error("conflict"))
    await editor.save()
    expect(editor.error()).toEqual(new Error("conflict"))
    expect(editor.dirty()).toBe(true)
    expect(props.onReload).not.toHaveBeenCalled()
    const payload = vi.mocked(replaceBook).mock.calls[0]
    expect(payload.slice(0, 2)).toEqual(["book", 7])
    expect(payload[2].entries?.map((entry) => [entry.library_entry_id, entry.position])).toEqual([
      ["two", 0],
      ["loose", 0],
      ["one", 1],
    ])
    expect(payload[2].sections?.[0].position).toBe(0)
    expect(editor.pending()).toBe(false)
  } finally {
    hook.cleanup()
  }
})

afterEach(() => vi.clearAllMocks())
const savedBook: Book = {
  id: "book",
  title: "Book",
  kind: "personal",
  revision: 7,
  created_at: "",
  updated_at: "",
  entries: [],
  sections: [],
}
function editorProps(): BookViewProps {
  return {
    book: savedBook,
    revision: 7,
    sourced: [],
    candidateQuery: "",
    onCandidateQuery: vi.fn(),
    onCandidatePage: vi.fn(),
    onReload: vi.fn(),
    onTrashed: vi.fn(),
  }
}
test("save acknowledges the baseline and discard returns to the saved book", async () => {
  const props = editorProps()
  const hook = renderHook(() => createBookEditor(props, vi.fn()))
  try {
    const editor = hook.result
    editor.setDraft("title", "Saved title")
    const acknowledged = { ...savedBook, title: "Saved title", revision: 8 }
    vi.mocked(replaceBook).mockResolvedValue({ value: acknowledged, revision: 8 })
    await editor.save()
    expect(editor.dirty()).toBe(false)
    editor.setDraft("title", "Unsaved title")
    editor.discard()
    expect(editor.draft.title).toBe("Saved title")
    expect(editor.dirty()).toBe(false)
    editor.setDraft("description", "Another change")
    await editor.save()
    expect(replaceBook).toHaveBeenLastCalledWith("book", 8, expect.anything())
    expect(props.onReload).not.toHaveBeenCalled()
  } finally {
    hook.cleanup()
  }
})
test("edits during a save survive the acknowledgement and use its revision", async () => {
  const hook = renderHook(() => createBookEditor(editorProps(), vi.fn()))
  try {
    const editor = hook.result
    const response = deferred<Versioned<Book>>()
    vi.mocked(replaceBook).mockReturnValueOnce(response.promise)
    editor.setDraft("title", "Submitted")
    const saving = editor.save()
    editor.setDraft("title", "Still typing")
    response.resolve({ value: { ...savedBook, title: "Submitted" }, revision: 8 })
    await saving
    expect(editor.draft.title).toBe("Still typing")
    expect(editor.dirty()).toBe(true)
    vi.mocked(replaceBook).mockResolvedValueOnce({
      value: { ...savedBook, title: "Still typing" },
      revision: 9,
    })
    await editor.save()
    expect(replaceBook).toHaveBeenLastCalledWith(
      "book",
      8,
      expect.objectContaining({ title: "Still typing" }),
    )
    expect(editor.dirty()).toBe(false)
    expect(bookInput(savedBook).title).toBe("Book")
  } finally {
    hook.cleanup()
  }
})
