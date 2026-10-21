import { Link } from "@tanstack/solid-router"
import { ArrowLeft } from "lucide-solid"
import type { Accessor } from "solid-js"
import { pageShell } from "styled-system/recipes"

import { ResourceState } from "@/components/status"
import { directionalIcon } from "@/components/ui/directional"
import { usePageTitle } from "@/lib/page-title"
import { latestUnlessFailed } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { BookView } from "./book-view"
import { createBookQuery } from "./create-book-query"

const layout = pageShell({ width: "medium" })
export function BookPage(props: { bookID: Accessor<string>; onTrashed: () => Promise<void> }) {
  const {
    book,
    refetch,
    sourced,
    candidateQuery,
    setCandidateQuery,
    setCandidateOffset,
    candidates,
  } = createBookQuery(() => props.bookID())
  usePageTitle(() =>
    book.error || !book.latest ? undefined : m.page_title_named({ name: book.latest.value.title }),
  )
  return (
    <main class={layout.page}>
      <Link class={layout.back} to="/books">
        <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />{" "}
        {m.book_page_books()}{" "}
      </Link>
      <ResourceState resource={book} onRetry={() => void refetch()}>
        {(loaded) =>
          loaded().value.id === props.bookID() ? (
            <BookView
              book={loaded().value}
              revision={loaded().revision}
              sourced={sourced() ?? []}
              candidatePage={latestUnlessFailed(candidates)}
              candidateQuery={candidateQuery()}
              onCandidateQuery={(query) => {
                setCandidateQuery(query)
                setCandidateOffset(0)
              }}
              onCandidatePage={(offset) => setCandidateOffset(offset)}
              onReload={() => void refetch()}
              onTrashed={props.onTrashed}
            />
          ) : undefined
        }
      </ResourceState>
    </main>
  )
}
