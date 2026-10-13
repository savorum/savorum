import { createResource, createSignal } from "solid-js"
import type { Accessor } from "solid-js"

import { getBook, listBookSourceRecipes } from "@/api/books"
import { suggestRecipes } from "@/api/selections"
export function createBookQuery(bookID: Accessor<string>) {
  const [book, { refetch }] = createResource(() => bookID(), getBook)
  const [sourced] = createResource(() => bookID(), listBookSourceRecipes)
  const [candidateQuery, setCandidateQuery] = createSignal("")
  const [candidateOffset, setCandidateOffset] = createSignal(0)
  // Render the latest resolved page below: a normal read would suspend this
  // route for each query and take its active search control out of the DOM.
  const [candidates] = createResource(
    () => ({
      limit: 10,
      offset: candidateOffset(),
      query: candidateQuery(),
      sort: "title-asc" as const,
    }),
    async (options) => {
      const page = await suggestRecipes(options.query, options.offset)
      return { ...page, total: page.offset + page.items.length + (page.has_more ? 1 : 0) }
    },
  )

  return {
    book,
    refetch,
    sourced,
    candidateQuery,
    setCandidateQuery,
    setCandidateOffset,
    candidates,
  }
}
