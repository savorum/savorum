import type { RecipeSort } from "@/api/client"
import type { RecipePage } from "@/api/recipes"

export const libraryPageSize = 36
export const libraryWindowSize = libraryPageSize * 5

export interface LibraryPageRequest {
  scope: string
  limit: number
  offset: number
  query: string
  sort: RecipeSort
  id: string
  groupTranslations: string[]
}

/** Keeps a bounded set of pages for this mounted library, including navigation intent. */
export function createLibraryPageCache(
  fetchPage: (options: LibraryPageRequest, signal: AbortSignal) => Promise<RecipePage>,
) {
  const pages = new Map<string, RecipePage>()
  const pending = new Map<string, { promise: Promise<RecipePage>; controller: AbortController }>()
  const keyOf = (options: LibraryPageRequest) => JSON.stringify(options)

  const read = (options: LibraryPageRequest): RecipePage | Promise<RecipePage> => {
    const key = keyOf(options)
    const cached = pages.get(key)
    if (cached) {
      pages.delete(key)
      pages.set(key, cached)
      return cached
    }
    const running = pending.get(key)
    if (running) return running.promise
    const controller = new AbortController()
    const promise = fetchPage(options, controller.signal)
      .then((page) => {
        if (!controller.signal.aborted) {
          pages.delete(key)
          pages.set(key, page)
          while (pages.size > 6) pages.delete(pages.keys().next().value!)
        }
        return page
      })
      .finally(() => {
        if (pending.get(key)?.controller === controller) pending.delete(key)
      })
    pending.set(key, { promise, controller })
    return promise
  }

  const cancelExcept = (options?: LibraryPageRequest | LibraryPageRequest[]) => {
    const kept = new Set((Array.isArray(options) ? options : options ? [options] : []).map(keyOf))
    for (const [key, request] of pending) {
      if (kept.has(key)) continue
      request.controller.abort()
      pending.delete(key)
    }
  }

  return {
    read,
    cancelExcept,
    clear: () => {
      cancelExcept()
      pages.clear()
    },
  }
}

export function canPrepareLibraryPage(): boolean {
  if (typeof navigator === "undefined" || !navigator.onLine) return false
  const connection = (
    navigator as Navigator & {
      connection?: { saveData?: boolean; effectiveType?: string }
    }
  ).connection
  return !connection?.saveData && !["slow-2g", "2g"].includes(connection?.effectiveType ?? "")
}
