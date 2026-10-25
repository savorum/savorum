import { Link } from "@tanstack/solid-router"
import { ArrowLeft } from "lucide-solid"
import type { Accessor } from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { SkeletonBlock, SkeletonGroup, SkeletonLine } from "@/components/skeleton"
import { ResourceState } from "@/components/status"
import { directionalIcon } from "@/components/ui/directional"
import { usePageTitle } from "@/lib/page-title"
import { latestUnlessFailed } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { EntryShapes } from "./book-reader"
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
      <ResourceState resource={book} loading={<BookLoading />} onRetry={() => void refetch()}>
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

function BookLoading() {
  return (
    <SkeletonGroup>
      <span class={styles.masthead}>
        <SkeletonBlock aspect="3 / 4" />
        <span class={styles.heading}>
          <SkeletonLine width="70%" size="lg" />
          <SkeletonLine width="40%" size="sm" />
          <SkeletonLine width="55%" size="sm" />
        </span>
      </span>
      <span class={styles.entries}>
        <EntryShapes count={5} />
      </span>
    </SkeletonGroup>
  )
}

const styles = {
  masthead: css({
    display: "grid",
    gap: { base: "5", md: "8" },
    gridTemplateColumns: { base: "5rem minmax(0, 1fr)", sm: "9rem minmax(0, 1fr)" },
  }),
  heading: css({ alignContent: "start", display: "grid", gap: "3", paddingTop: "1" }),
  entries: css({ display: "block", marginTop: "12" }),
}
