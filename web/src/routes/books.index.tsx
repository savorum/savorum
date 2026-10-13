import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { BooksPage } from "@/features/books/books-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/books/")({
  component: BooksPageRoute,
  head: () => ({ meta: [{ title: m.books_index_books_savorum() }] }),
})
function BooksPageRoute() {
  const navigate = useNavigate()
  return (
    <BooksPage
      onCreated={(bookId) => {
        void navigate({ to: "/books/$bookId", params: { bookId } })
      }}
    />
  )
}
