import { createMemo, createResource } from "solid-js"

import type { BookSummary, Facet, Tag } from "@/api/client"
import { libraryRecipeTotal } from "@/api/library-total"
import { listFacets, listTags } from "@/api/organization"
import { suggestBooks, lookupBooks } from "@/api/selections"
import { latestUnlessFailed, settled } from "@/lib/settled"

/** Names what the library knows, so a stored identifier can be shown as a word. */
export interface LibraryVocabulary {
  tags: () => Tag[]
  facets: () => Facet[]
  books: () => BookSummary[]
  /** Active recipes in the library, once known. */
  recipeTotal?: () => number | undefined
  refreshLabels?: () => void
  searchBooks?: (
    query: string,
    offset: number,
  ) => Promise<{ items: BookSummary[]; has_more: boolean }>
  resolveBooks?: (ids: string[]) => Promise<void>
  label: (id: string) => string
}

export function createLibraryVocabulary(): LibraryVocabulary {
  // Recipe requests take a connection before the supporting filter metadata.
  const [tags, { refetch: refetchTags }] = createResource(() => Promise.resolve().then(listTags))
  const [facets, { refetch: refetchFacets }] = createResource(() =>
    Promise.resolve().then(() => listFacets()),
  )
  const [total, { refetch: refetchTotal }] = createResource(() =>
    Promise.resolve().then(libraryRecipeTotal),
  )
  const [books, { mutate: mutateBooks }] = createResource(async () =>
    (await suggestBooks()).items.map((book) => ({
      ...book,
      kind: book.kind as BookSummary["kind"],
      entry_count: 0,
      updated_at: "",
    })),
  )
  const remember = (items: Awaited<ReturnType<typeof suggestBooks>>["items"]) => {
    const current = latestUnlessFailed(books) ?? []
    mutateBooks([
      ...current,
      ...items
        .filter((book) => !current.some((known) => known.id === book.id))
        .map((book) => ({
          ...book,
          kind: book.kind as BookSummary["kind"],
          entry_count: 0,
          updated_at: "",
        })),
    ])
  }
  const names = createMemo(() => {
    const result = new Map<string, string>()
    for (const tag of settled(tags) ?? []) result.set(tag.id, tag.name)
    for (const facet of settled(facets) ?? []) result.set(facet.id, facet.value)
    for (const book of settled(books) ?? []) result.set(book.id, book.title)
    return result
  })
  return {
    tags: () => settled(tags) ?? [],
    facets: () => settled(facets) ?? [],
    books: () => settled(books) ?? [],
    recipeTotal: () => settled(total),
    refreshLabels: () => {
      void refetchTags()
      void refetchFacets()
      void refetchTotal()
    },
    searchBooks: async (query, offset) => {
      const page = await suggestBooks(query, offset)
      remember(page.items)
      return {
        items: page.items.map((book) => ({
          ...book,
          kind: book.kind as BookSummary["kind"],
          entry_count: 0,
          updated_at: "",
        })),
        has_more: page.has_more,
      }
    },
    resolveBooks: async (ids) => remember((await lookupBooks(ids)).items),
    label: (id) => names().get(id) ?? id,
  }
}
