import { batch, createSignal, onCleanup } from "solid-js"

import { type ImportPreview } from "@/api/import-previews"
import {
  type CandidateFilter,
  candidateMatchesFilter,
  summarizeCandidates,
} from "@/lib/import-candidate"
import {
  type ImportChoice,
  initializeImportReviewState,
  reconcileImportReviewState,
} from "@/lib/import-preview-state"

const emptyImportPreview: ImportPreview = {
  id: "",
  expires_at: "",
  preview_revision: 1,
  items: [],
  resolution_decisions: [],
  resolution_groups: [],
  resolution_profiles: [],
}

export function createImportReview() {
  let generation = 0
  let disposed = false
  const listeners = new Set<() => void>()
  const captureSession = () => {
    const captured = generation
    return () => !disposed && captured === generation
  }
  const onSessionChange = (listener: () => void) => {
    listeners.add(listener)
    onCleanup(() => listeners.delete(listener))
  }
  onCleanup(() => {
    disposed = true
    generation++
  })

  const [importPreview, setImportPreview] = createSignal<ImportPreview>(emptyImportPreview)
  const [selectedImports, setSelectedImports] = createSignal<string[]>([])
  // Source retention is deliberately opt-in and independent of recipe image
  // selection. It is carried by item ID so refreshing a preview cannot move a
  // reader's choice to another row.
  const [retainedSources, setRetainedSources] = createSignal<string[]>([])
  const [importChoices, setImportChoices] = createSignal<Record<string, ImportChoice>>({})
  const [importFilter, setImportFilter] = createSignal<CandidateFilter>("all")
  const [lastImportSelected, setLastImportSelected] = createSignal<string>()
  const previewItems = () => importPreview().items
  const importSummary = () => summarizeCandidates(previewItems())
  const shownImports = () =>
    previewItems().filter((item) => candidateMatchesFilter(item, importFilter()))
  const importSelected = (id: string) => selectedImports().includes(id)
  const setImportSelected = (id: string, selected: boolean) =>
    setSelectedImports((current) =>
      selected
        ? current.includes(id)
          ? current
          : [...current, id]
        : current.filter((candidate) => candidate !== id),
    )
  const retainSource = (id: string, retained: boolean) =>
    setRetainedSources((current) =>
      retained
        ? current.includes(id)
          ? current
          : [...current, id]
        : current.filter((candidate) => candidate !== id),
    )
  const selectImportRange = (id: string, selected: boolean, range: boolean) => {
    const shown = shownImports()
    const staticPrevious = lastImportSelected()
    if (range && staticPrevious) {
      const from = shown.findIndex((item) => item.id === staticPrevious)
      const to = shown.findIndex((item) => item.id === id)
      if (from >= 0 && to >= 0) {
        for (const item of shown.slice(Math.min(from, to), Math.max(from, to) + 1)) {
          if (item.can_commit) setImportSelected(item.id, selected)
        }
      } else {
        setImportSelected(id, selected)
      }
    } else {
      setImportSelected(id, selected)
    }
    setLastImportSelected(id)
  }
  const selectVisibleImports = () =>
    setSelectedImports((current) => [
      ...new Set([
        ...current,
        ...shownImports()
          .filter((item) => item.can_commit)
          .map((item) => item.id),
      ]),
    ])
  const selectEligibleImports = () =>
    setSelectedImports(
      previewItems()
        .filter((item) => item.can_commit)
        .map((item) => item.id),
    )
  const acceptImportPreview = (staged: ImportPreview, mode: "initialize" | "reconcile") => {
    const previous = importPreview()
    const normalized = staged
    const staticReview =
      mode === "reconcile"
        ? reconcileImportReviewState(previous, normalized, {
            selected: selectedImports(),
            retainedSources: retainedSources(),
            choices: importChoices(),
          })
        : initializeImportReviewState(normalized)
    batch(() => {
      generation++
      for (const listener of listeners) listener()
      setImportPreview(normalized)
      setSelectedImports(staticReview.selected)
      setRetainedSources(staticReview.retainedSources)
      setImportChoices(staticReview.choices)
      if (mode === "initialize") setImportFilter("all")
      if (
        mode === "initialize" ||
        !normalized.items.some((item) => item.id === lastImportSelected())
      )
        setLastImportSelected(undefined)
    })
  }

  const clearSelection = () => setSelectedImports([])
  const chooseImport = (id: string, choice: ImportChoice) =>
    setImportChoices((current) => ({ ...current, [id]: choice }))
  const startSession = (preview: ImportPreview) => acceptImportPreview(preview, "initialize")
  const refreshSession = (preview: ImportPreview) => acceptImportPreview(preview, "reconcile")
  const closeSession = () => startSession(emptyImportPreview)
  // Job remainders intentionally initialize choices for the remaining candidates.
  const acceptJobRemainder = (preview?: ImportPreview) =>
    startSession(preview ?? emptyImportPreview)

  return {
    importPreview,
    selectedImports,
    retainedSources,
    importChoices,
    importFilter,
    setImportFilter,
    previewItems,
    importSummary,
    shownImports,
    importSelected,
    retainSource,
    selectImportRange,
    selectVisibleImports,
    selectEligibleImports,
    clearSelection,
    chooseImport,
    startSession,
    refreshSession,
    closeSession,
    acceptJobRemainder,
    captureSession,
    onSessionChange,
  }
}
export type ImportReview = ReturnType<typeof createImportReview>
