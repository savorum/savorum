import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { BookPrintPage } from "@/features/printing/book-print-page"
import {
  type PrintSearch,
  parsePrintSearch,
  searchFromSettings,
  settingsFromSearch,
} from "@/lib/print-options"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/books/$bookId/print")({
  component: BookPrintRoute,
  validateSearch: (search: Record<string, unknown>): PrintSearch => parsePrintSearch(search),
  head: () => ({ meta: [{ title: m.books_bookId_print_print_book_savorum() }] }),
})
function BookPrintRoute() {
  const params = Route.useParams()
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  return (
    <BookPrintPage
      bookId={() => params().bookId}
      search={search}
      onSettingsChange={(next) => {
        void navigate({
          search: (current: PrintSearch) => ({
            ...current,
            ...searchFromSettings({ ...settingsFromSearch(current), ...next }),
          }),
          replace: true,
        })
      }}
    />
  )
}
