import { Link } from "@tanstack/solid-router"
import { ArrowLeft } from "lucide-solid"
import { createResource } from "solid-js"

import { getBook } from "@/api/books"
import { Loading, ResourceState } from "@/components/status"
import { directionalIcon } from "@/components/ui/directional"
import { PrintWorkspace } from "@/features/printing/print-workspace"
import { measurementSystem } from "@/lib/preferences"
import {
  type PrintSearch,
  type PrintSettings,
  settingsFromSearch,
  toRequestOptions,
} from "@/lib/print-options"

import { m } from "../../lib/messages.ts"

export function BookPrintPage(props: {
  bookId: () => string
  search: () => PrintSearch
  onSettingsChange: (next: Partial<PrintSettings>) => void
}) {
  const [book, { refetch }] = createResource(() => props.bookId(), getBook)

  const settings = (): PrintSettings => settingsFromSearch(props.search())

  // A book prints every recipe at the batch each was written for. One factor
  // across a whole book would mean nothing.
  const presentation = () => ({ measurement: measurementSystem() })

  return (
    <>
      <ResourceState
        resource={book}
        loading={<Loading>{m.book_print_page_loading_book()}</Loading>}
        fallback={m.books_the_book_could_not_be_loaded()}
        onRetry={() => void refetch()}
      >
        {(loaded) => (
          <PrintWorkspace
            title={loaded().value.title}
            backTo={
              <Link
                to="/books/$bookId"
                params={{ bookId: props.bookId() }}
                style={{ display: "inline-flex", "align-items": "center", gap: "0.5rem" }}
              >
                <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />
                {loaded().value.title}
              </Link>
            }
            source={{ book: props.bookId() }}
            settings={settings()}
            onSettingsChange={props.onSettingsChange}
            presentation={presentation()}
            showScale={false}
            request={() => toRequestOptions(settings(), presentation())}
          />
        )}
      </ResourceState>
    </>
  )
}
