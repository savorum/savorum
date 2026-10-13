import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { BookHistoryPage } from "@/features/book-history/book-history-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/books/$bookId/history")({
  component: BookHistoryPageRoute,
  head: () => ({ meta: [{ title: m.books_bookId_history_earlier_versions_savorum() }] }),
})
function BookHistoryPageRoute() {
  const params = Route.useParams()
  const navigate = useNavigate()
  return (
    <BookHistoryPage
      bookId={() => params().bookId}
      onRestored={() => {
        void navigate({ to: "/books/$bookId", params: { bookId: params().bookId } })
      }}
    />
  )
}
