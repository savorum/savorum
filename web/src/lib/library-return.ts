/**
 * What a recipe route carries in browser history so the library it came from
 * can be restored exactly, and so an action taken on the recipe can be
 * answered back in the library. The declaration lives beside these types
 * rather than in the router, so every module that reads history state has it.
 */
export interface LibraryReturnLocation {
  href: string
  historyIndex: number
  scrollX: number
  scrollY: number
}

/**
 * Returns the browser-history movement that reaches the exact library entry.
 * Its history key is what TanStack uses to restore the saved scroll position.
 */
export function libraryReturnDelta(
  currentHistoryIndex: number,
  target: LibraryReturnLocation | undefined,
): number | undefined {
  if (
    !target ||
    !Number.isInteger(currentHistoryIndex) ||
    !Number.isInteger(target.historyIndex) ||
    (target.href !== "/" && !target.href.startsWith("/?") && !target.href.startsWith("/#"))
  ) {
    return undefined
  }
  const delta = target.historyIndex - currentHistoryIndex
  return delta < 0 ? delta : undefined
}

/** A recipe just moved to trash, and the revision a restore has to name. */
export interface TrashedRecipe {
  id: string
  title: string
  revision: number
}

declare module "@tanstack/history" {
  interface HistoryState {
    libraryReturn?: LibraryReturnLocation
    trashedRecipe?: TrashedRecipe
    /** The recipes chosen in the library at this history entry, restored on return. */
    librarySelection?: string[]
  }
}
