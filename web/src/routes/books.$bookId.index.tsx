import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { BookPage } from "@/features/books/book-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/books/$bookId/")({
  component: BookRoute,
  head: () => ({ meta: [{ title: m.books_bookId_index_book_savorum() }] }),
})
function BookRoute() {
  const params = Route.useParams()
  const navigate = useNavigate()
  return <BookPage bookID={() => params().bookId} onTrashed={() => navigate({ to: "/books" })} />
}
