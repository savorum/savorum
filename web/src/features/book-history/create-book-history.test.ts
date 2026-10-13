import { renderHook, waitFor } from "@solidjs/testing-library"
import { expect, test, vi } from "vitest"

import { getBook, getBookRevision, listBookRevisions, restoreBookRevision } from "@/api/books"
import type { Book } from "@/api/client"
import { listRecipeReferences } from "@/api/recipes"
import { deferred } from "@/test/import-fixtures"

import { createBookHistory } from "./create-book-history"
vi.mock("@/api/books")
vi.mock("@/api/recipes")
test("a failed restore keeps the selection and refetches while a retry restores once", async () => {
  const book: Book = {
    id: "book",
    kind: "personal",
    title: "Book",
    sections: [],
    entries: [],
    revision: 5,
    created_at: "",
    updated_at: "",
  }
  vi.mocked(getBook).mockResolvedValue({ value: book, revision: 5 })
  vi.mocked(listBookRevisions).mockResolvedValue([])
  vi.mocked(listRecipeReferences).mockResolvedValue([])
  vi.mocked(getBookRevision).mockResolvedValue({
    book,
    cause: "api",
    created_at: "",
    state: "active",
  })

  const onRestored = vi.fn()
  const hook = renderHook(() => createBookHistory({ bookId: () => "book", onRestored }))
  try {
    const history = hook.result
    await waitFor(() => expect(history.book()).toBeDefined())

    history.setSelected(2)
    await waitFor(() => expect(getBookRevision).toHaveBeenCalledWith("book", 2))

    history.setConfirm(2)
    const booksBefore = vi.mocked(getBook).mock.calls.length
    const historyBefore = vi.mocked(listBookRevisions).mock.calls.length

    const attempt = deferred<Awaited<ReturnType<typeof restoreBookRevision>>>()
    vi.mocked(restoreBookRevision).mockReturnValueOnce(attempt.promise)

    const failure = new Error("revision conflict")
    const failed = history.restore(2, 5)
    expect(history.restoring()).toBe(true)
    attempt.reject(failure)
    await failed

    expect(history.error()).toBe(failure)
    expect(history.selected()).toBe(2)
    expect(history.confirm()).toBeUndefined()
    expect(history.restoring()).toBe(false)
    expect(onRestored).not.toHaveBeenCalled()
    expect(vi.mocked(getBook).mock.calls.length).toBeGreaterThan(booksBefore)
    expect(vi.mocked(listBookRevisions).mock.calls.length).toBeGreaterThan(historyBefore)

    vi.mocked(restoreBookRevision).mockResolvedValueOnce({ value: book, revision: 6 })
    await history.restore(2, 5)
    expect(restoreBookRevision).toHaveBeenLastCalledWith("book", 2, 5)
    expect(onRestored).toHaveBeenCalledOnce()
  } finally {
    hook.cleanup()
  }
})
