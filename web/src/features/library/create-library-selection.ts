import { createSignal, onCleanup, untrack } from "solid-js"

import { capabilities } from "@/api/capabilities"
import { maximumBundleRecipes } from "@/api/exports"
import { listRecipes, listSavedViewRecipes } from "@/api/recipes"
import { filterQuery } from "@/lib/library-query"
import { formatNumber } from "@/lib/locale-format"

import { m } from "../../lib/messages.ts"
import type { LibraryQuery } from "./create-library-query"

/** The instance reports its own limits; these apply until it has. */
const printLimit = () => capabilities()?.limits?.print_recipes ?? 250
const bundleLimit = () => capabilities()?.limits?.export_recipes ?? maximumBundleRecipes

export function createLibrarySelection(
  query: Pick<LibraryQuery, "text" | "filters" | "viewID" | "sort">,
  /**
   * Recipes already chosen when the library was reached. Returning from an
   * action taken on a selection returns to that selection; nothing else opens
   * the library already choosing.
   */
  resumed?: { selected?: () => string[] | undefined; onLeft?: () => void },
) {
  const { text, filters, viewID, sort } = query
  const initial = untrack(() => resumed?.selected?.() ?? [])
  let generation = 0
  const [selectionBusy, setSelectionBusy] = createSignal(false)
  const [selectionError, setSelectionError] = createSignal<unknown>()
  const invalidate = () => {
    generation++
    setSelectionBusy(false)
    setSelectionError(undefined)
  }
  onCleanup(invalidate)
  const [selecting, setSelecting] = createSignal(initial.length > 0)
  const [selected, setSelected] = createSignal<string[]>(initial)
  const exceedsPrintLimit = () => selected().length > printLimit()
  const exceedsBundleLimit = () => selected().length > bundleLimit()
  const selectionLimitNote = () => {
    const print = formatNumber(printLimit())
    const bundle = formatNumber(bundleLimit())
    if (exceedsPrintLimit() && exceedsBundleLimit())
      return printLimit() === bundleLimit()
        ? m.create_library_selection_printing_and_sref_bundles_hold_up_to_recipes_narrow_thi({
            value1: print,
          })
        : m.create_library_selection_printing_and_sref_bundles_hold_different_limits({
            value1: print,
            value2: bundle,
          })
    if (exceedsPrintLimit())
      return m.create_library_selection_printing_holds_up_to_recipes({ value1: print })
    if (exceedsBundleLimit())
      return m.create_library_selection_sref_bundles_hold_up_to_recipes({ value1: bundle })
    return undefined
  }

  const isSelected = (id: string) => selected().includes(id)
  const toggleSelected = (id: string) => {
    invalidate()
    setSelected((current) =>
      current.includes(id) ? current.filter((entry) => entry !== id) : [...current, id],
    )
  }
  const selectShown = (ids: string[]) => {
    invalidate()
    setSelected(ids)
  }
  const clearSelection = () => selectShown([])
  const enterSelection = () => setSelecting(true)

  const leaveSelection = () => {
    invalidate()
    setSelecting(false)
    setSelected([])
    // A selection that has been finished with must not come back with the
    // browser's back button.
    resumed?.onLeft?.()
  }

  /** Snapshot the view at invocation; newer selection intent invalidates completion. */
  const selectAllMatching = async () => {
    invalidate()
    const request = generation
    const snapshot = { query: filterQuery(text(), filters()), id: viewID(), sort: sort() }
    setSelectionBusy(true)
    try {
      const found: string[] = []
      const size = 200
      for (let offset = 0; ; offset += size) {
        const result = snapshot.id
          ? await listSavedViewRecipes({ id: snapshot.id, limit: size, offset })
          : await listRecipes({ limit: size, offset, query: snapshot.query, sort: snapshot.sort })
        if (request !== generation) return
        found.push(...result.items.map((entry) => entry.id))
        if (found.length >= result.total || result.items.length === 0) break
      }
      setSelected(found)
    } catch (error) {
      if (request === generation) setSelectionError(error)
    } finally {
      if (request === generation) setSelectionBusy(false)
    }
  }

  return {
    selecting,
    enterSelection,
    selected,
    selectShown,
    clearSelection,
    selectionBusy,
    selectionError,
    exceedsPrintLimit,
    exceedsBundleLimit,
    selectionLimitNote,
    isSelected,
    toggleSelected,
    leaveSelection,
    selectAllMatching,
  }
}
export type LibrarySelection = ReturnType<typeof createLibrarySelection>
