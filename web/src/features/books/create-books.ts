import { createResource, createSignal } from "solid-js"

import { createBook, listBookPage } from "@/api/books"
export type CreateBooksOptions = { onCreated: (bookID: string) => void }
export function createBooks(props: CreateBooksOptions) {
  const [offset, setOffset] = createSignal(0)
  const [books, { refetch }] = createResource(() => ({ limit: 24, offset: offset() }), listBookPage)
  const [creating, setCreating] = createSignal(false)
  const [title, setTitle] = createSignal("")
  const [published, setPublished] = createSignal(false)
  const [pending, setPending] = createSignal(false)
  const [error, setError] = createSignal<unknown>()

  const create = async () => {
    setPending(true)
    setError(undefined)
    try {
      const created = await createBook({
        kind: published() ? "publication" : "personal",
        title: title().trim(),
        sections: [],
        entries: [],
      })
      setCreating(false)
      setTitle("")
      props.onCreated(created.value.id)
    } catch (failure) {
      setError(failure)
    } finally {
      setPending(false)
    }
  }

  const closeCreator = () => {
    setCreating(false)
    setTitle("")
    setPublished(false)
    setError(undefined)
  }
  return {
    offset,
    setOffset,
    books,
    refetch,
    creating,
    setCreating,
    title,
    setTitle,
    published,
    setPublished,
    pending,
    error,
    create,
    closeCreator,
  }
}
