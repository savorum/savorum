import {
  type Accessor,
  batch,
  createComputed,
  createMemo,
  createResource,
  createSignal,
  onCleanup,
} from "solid-js"
import { createStore, reconcile } from "solid-js/store"

import type { RecipeSort, RecipeSummary } from "@/api/client"
import { listRecipes, listSavedViewRecipes, type RecipePage } from "@/api/recipes"
import { createSavedView, listSavedViews } from "@/api/saved-views"
import { account, sessionEnded } from "@/features/account/account-state"
import { createLibraryVocabulary } from "@/features/library/create-library-vocabulary"
import {
  type LibraryFilters,
  activeFilterCount,
  filterQuery,
  parseFilterQuery,
} from "@/lib/library-query"
import { contentLanguages } from "@/lib/preferences"
import { settled } from "@/lib/settled"

import {
  canPrepareLibraryPage,
  createLibraryPageCache,
  libraryPageSize,
  libraryWindowSize,
} from "./library-pages"
import {
  type LibrarySearch,
  type UpdateLibrarySearch,
  filterSearch,
  searchFilters,
} from "./library-search"

export function createLibraryQuery(search: Accessor<LibrarySearch>, update: UpdateLibrarySearch) {
  const [views, { refetch: refetchViews }] = createResource(() =>
    Promise.resolve().then(listSavedViews),
  )
  /** The name being given to a search, while the reader is giving it. */
  const [namingSearch, setNamingSearch] = createSignal<string>()
  const [savingSearch, setSavingSearch] = createSignal(false)
  const [saveSearchError, setSaveSearchError] = createSignal<unknown>()
  const vocabulary = createLibraryVocabulary()
  const [filtersOpen, setFiltersOpen] = createSignal(false)
  const viewID = () => search().view ?? ""
  const offset = () => search().offset ?? 0
  const requestedLimit = () => search().shown ?? libraryPageSize
  const activeView = () => {
    const id = viewID()
    return id ? (settled(views) ?? []).find((view) => view.id === id) : undefined
  }

  /** Filters stored with the active saved view. */
  const opened = () => {
    const view = activeView()
    return view ? parseFilterQuery(view.query) : undefined
  }

  const text = () => opened()?.text ?? search().q ?? ""
  const sort = () =>
    (activeView()?.sort as RecipeSort | undefined) ??
    search().sort ??
    (text() ? "relevance" : "updated-desc")
  const filters = (): LibraryFilters => {
    const shown = opened()
    if (shown) return shown.filters
    return searchFilters(search())
  }

  /** Editing a saved view's filters returns to a regular library search. */
  const editSearch = (next: Partial<LibrarySearch>, replace = false) => {
    const shown = opened()
    if (!shown) return update(next, replace)
    update(
      {
        view: undefined,
        q: shown.text || undefined,
        sort: (activeView()?.sort as RecipeSort | undefined) ?? undefined,
        ...filterSearch(shown.filters),
        ...next,
      },
      replace,
    )
  }

  const applyFilters = (next: LibraryFilters) => editSearch(filterSearch(next))

  /** The search on screen, in the grammar a saved search stores. */
  const currentQuery = () => filterQuery(text(), filters())

  /** Whether there is a search to save that is not already the open saved one. */
  const savableSearch = () =>
    !activeView() && (text().trim() !== "" || activeFilterCount(filters()) > 0)

  const labelFor = (id: string) => vocabulary.label(id)

  const saveSearch = async (title: string) => {
    const named = title.trim()
    if (named === "" || savingSearch()) return
    setSavingSearch(true)
    setSaveSearchError(undefined)
    try {
      const view = await createSavedView({
        title: named,
        query: currentQuery(),
        sort: sort(),
      })
      await refetchViews()
      setNamingSearch(undefined)
      // Opening it is the confirmation: the reader lands in the thing they
      // just made rather than being told it exists somewhere.
      update({ view: view.id })
    } catch (error) {
      setSaveSearchError(error)
    } finally {
      setSavingSearch(false)
    }
  }

  const options = createMemo(
    () => ({
      scope: JSON.stringify([account()?.user?.id, account()?.active_library_id]),
      limit: libraryPageSize,
      offset: offset(),
      shown: requestedLimit(),
      query: viewID() ? "" : filterQuery(text(), filters()),
      sort: viewID() ? ("relevance" as RecipeSort) : sort(),
      id: viewID(),
      groupTranslations: contentLanguages(),
    }),
    undefined,
    { equals: (previous, next) => JSON.stringify(previous) === JSON.stringify(next) },
  )
  const cache = createLibraryPageCache((request, signal) =>
    request.id
      ? listSavedViewRecipes({
          id: request.id,
          limit: request.limit,
          offset: request.offset,
          groupTranslations: request.groupTranslations,
          signal,
        })
      : listRecipes({ ...request, signal }),
  )
  const [moreLoading, setMoreLoading] = createSignal(false)
  const [moreError, setMoreError] = createSignal<unknown>()
  let continuation = 0
  const resetContinuation = () => {
    continuation++
    setMoreLoading(false)
    setMoreError(undefined)
  }
  createComputed(() => {
    options()
    resetContinuation()
  })
  onCleanup(() => {
    resetContinuation()
    cache.clear()
  })
  createComputed(() => {
    if (sessionEnded()) {
      resetContinuation()
      cache.clear()
    }
  })
  const [page, actions] = createResource(options, (request) => {
    const { shown: limit, ...first } = request
    const requests = Array.from({ length: limit / libraryPageSize }, (_, index) => ({
      ...first,
      offset: first.offset + index * libraryPageSize,
    }))
    cache.cancelExcept(requests)
    const results = requests.map(cache.read)
    const combine = (pages: RecipePage[]) => ({
      offset: first.offset,
      limit,
      total: pages.at(-1)?.total ?? 0,
      items: [
        ...new Map(pages.flatMap((part) => part.items).map((item) => [item.id, item])).values(),
      ],
    })
    return results.some((result) => result instanceof Promise)
      ? Promise.all(results.map((result) => Promise.resolve(result))).then(combine)
      : combine(results as RecipePage[])
  })
  const refetch = () => {
    resetContinuation()
    cache.clear()
    return actions.refetch()
  }
  const prefetchPage = (nextOffset: number) => {
    if (
      page.state !== "ready" ||
      moreLoading() ||
      moreError() ||
      sessionEnded() ||
      !canPrepareLibraryPage() ||
      nextOffset < 0 ||
      nextOffset === offset() ||
      nextOffset >= total()
    )
      return
    const { shown: _shown, ...request } = options()
    const next = { ...request, offset: nextOffset }
    cache.cancelExcept(next)
    void Promise.resolve(cache.read(next)).catch(() => undefined)
  }
  const [focusOffset, setFocusOffset] = createSignal<number>()
  // Reconcile by recipe ID and retain the resolved page through loading and failure.
  const [shown, setShown] = createStore({
    items: [] as RecipeSummary[],
    total: 0,
    offset: offset(),
    limit: 0,
  })
  createComputed(() => {
    const result = settled(page)
    if (!result) return
    if (
      page.state === "ready" &&
      result.offset === offset() &&
      result.offset > 0 &&
      result.offset >= result.total
    ) {
      const last = Math.max(0, Math.ceil(result.total / libraryPageSize) - 1) * libraryPageSize
      if (focusOffset() !== undefined) setFocusOffset(last)
      update({ offset: last || undefined }, true)
      return
    }
    batch(() => {
      setShown("items", reconcile(result.items, { key: "id" }))
      setShown({ total: result.total, offset: result.offset, limit: result.limit })
    })
  })
  const entries = () => shown.items
  const total = () => shown.total
  const shownOffset = () => shown.offset
  const shownLimit = () => shown.limit
  const nextOffset = () => shown.offset + shown.limit
  const hasMore = () => shown.limit > 0 && nextOffset() < total()
  const canAppend = () => hasMore() && shown.limit < libraryWindowSize
  const loading = () => page.loading || moreLoading()
  const error = () => page.error ?? moreError()
  const loadMore = async (focus = false) => {
    if (!canAppend() || page.state !== "ready" || moreLoading() || sessionEnded()) return
    const version = ++continuation
    const target = nextOffset()
    const nextLimit = shown.limit + libraryPageSize
    const { shown: _shown, ...request } = options()
    const next = { ...request, offset: target }
    batch(() => {
      setMoreLoading(true)
      setMoreError(undefined)
    })
    cache.cancelExcept(next)
    try {
      await cache.read(next)
      if (version !== continuation) return
      if (focus) setFocusOffset(target)
      update({ offset: request.offset || undefined, shown: nextLimit }, true)
    } catch (error) {
      if (version === continuation) setMoreError(error)
    } finally {
      if (version === continuation) setMoreLoading(false)
    }
  }
  /**
   * Whether what is on screen is a subset of the library. Filters count: a
   * filter that matches nothing must not be reported as an empty library.
   */
  const narrowed = () =>
    text() !== "" ||
    viewID() !== "" ||
    activeFilterCount(filters()) > 0 ||
    entries().length < total()

  return {
    views,
    namingSearch,
    setNamingSearch,
    savingSearch,
    saveSearchError,
    vocabulary,
    filtersOpen,
    setFiltersOpen,
    viewID,
    offset,
    shownOffset,
    shownLimit,
    nextOffset,
    hasMore,
    canAppend,
    loading,
    error,
    moreError,
    loadMore,
    prefetchPage,
    focusOffset,
    setFocusOffset,
    activeView,
    opened,
    text,
    sort,
    filters,
    editSearch,
    applyFilters,
    currentQuery,
    savableSearch,
    labelFor,
    saveSearch,
    page,
    refetch,
    entries,
    total,
    narrowed,
  }
}
export type LibraryQuery = ReturnType<typeof createLibraryQuery>
