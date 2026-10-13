import { type Accessor, createEffect, createSignal, onCleanup } from "solid-js"

import { startImportJob } from "@/api/import-jobs"
import {
  type ImportPreview,
  type ImportPreviewItem,
  cancelImportPreview,
  editImportPreviewText,
  previewExternalRecipes,
  previewRecipeURL,
  readImportPreview,
} from "@/api/import-previews"
import type { ImportPhase } from "@/lib/import-progress"

import { m } from "../../lib/messages.ts"
import { createImportReview } from "./create-import-review"
import { createMeasurementResolution } from "./create-measurement-resolution"

export function createImportWorkflow(
  capture: Accessor<string | undefined>,
  onJob: (id: string, remainingPreviewID?: string) => Promise<void>,
  staged: Accessor<string | undefined> = () => undefined,
) {
  const review = createImportReview()
  let disposed = false
  const [mutationBusy, setMutationBusy] = createSignal(false)
  // Mutations are exclusive across text edits, apply/undo, staging and job handoff.
  // A client cancellation never releases this lock before the request settles.
  const beginMutation = () => {
    if (disposed || mutationBusy()) return false
    setMutationBusy(true)
    return true
  }
  const endMutation = () => setMutationBusy(false)
  const measurement = createMeasurementResolution(review, {
    busy: mutationBusy,
    begin: beginMutation,
    end: endMutation,
  })
  const { importPreview, selectedImports, retainedSources, importChoices } = review
  const [importing, setImporting] = createSignal(false)
  const [previewing, setPreviewing] = createSignal(false)
  const [importPhase, setImportPhase] = createSignal<ImportPhase>({ kind: "idle" })
  const [importError, setImportError] = createSignal<unknown>()
  const [urlImportOpen, setURLImportOpen] = createSignal(false)
  const [textImportOpen, setTextImportOpen] = createSignal(false)
  const [editingTextItem, setEditingTextItem] = createSignal<ImportPreviewItem>()
  const [urlImport, setURLImport] = createSignal("")
  const [urlImportError, setURLImportError] = createSignal<unknown>()
  let previewController: AbortController | undefined

  const prepareImportPreview = async (
    opening: ImportPhase,
    load: (signal: AbortSignal) => Promise<ImportPreview>,
  ) => {
    if (!beginMutation()) return false
    const controller = new AbortController()
    previewController = controller
    const finish = () => {
      if (previewController !== controller) return
      setPreviewing(false)
      setImportPhase({ kind: "idle" })
      previewController = undefined
    }
    setPreviewing(true)
    setImportPhase(opening)
    setImportError(undefined)
    try {
      const previous = importPreview()
      if (previous.id) {
        await cancelImportPreview(previous.id)
        // The old preview is no longer usable even if the subsequent upload fails.
        if (!disposed) review.closeSession()
      }
      if (controller.signal.aborted || disposed) return false
      const staged = await load(controller.signal)
      if (controller.signal.aborted || disposed) return false
      finish()
      review.startSession(staged)
      return true
    } catch (error) {
      if (!disposed && !controller.signal.aborted) setImportError(error)
      return false
    } finally {
      finish()
      endMutation()
    }
  }

  const stageImportFiles = (files: readonly File[]) => {
    const total = files.reduce((bytes, file) => bytes + file.size, 0)
    return prepareImportPreview(
      { kind: "uploading", files: files.length, sent: 0, total },
      (signal) =>
        previewExternalRecipes(files, {
          signal,
          onUpload: ({ sent, total: reported }) =>
            !signal.aborted &&
            !disposed &&
            setImportPhase({
              kind: "uploading",
              files: files.length,
              sent,
              total: reported || total,
            }),
          onReading: () =>
            !signal.aborted &&
            !disposed &&
            setImportPhase({ kind: "reading", files: files.length }),
        }),
    ).then(() => undefined)
  }

  const stageImportURL = async () => {
    const value = urlImport().trim()
    let parsed: URL
    try {
      parsed = new URL(value)
      if (parsed.protocol !== "http:" && parsed.protocol !== "https:") throw new Error()
    } catch {
      setURLImportError(
        new Error(m.create_import_workflow_enter_a_complete_http_or_https_recipe_url()),
      )
      return
    }
    setURLImportError(undefined)
    const staged = await prepareImportPreview(
      { kind: "fetching", url: parsed.toString() },
      (signal) => previewRecipeURL(parsed.toString(), signal),
    )
    if (staged) {
      setURLImportOpen(false)
      setURLImport("")
    } else {
      setURLImportError(importError())
    }
  }

  /** Reopens a review left staged; an expired one quietly does not reopen. */
  let handledStaged: string | undefined
  createEffect(() => {
    const previewID = staged()
    if (!previewID || previewID === handledStaged || typeof window === "undefined") return
    handledStaged = previewID
    if (importPreview().id === previewID || !beginMutation()) return
    void (async () => {
      try {
        const reopened = await readImportPreview(previewID)
        if (!disposed && reopened.items.length > 0) review.startSession(reopened)
      } catch {
        // The review is gone. The library it was staged against is unchanged.
      } finally {
        endMutation()
      }
    })()
  })

  let handledCapture: string | undefined
  createEffect(() => {
    const captured = capture()
    if (!captured || captured === handledCapture || typeof window === "undefined") return
    handledCapture = captured
    setURLImport(captured)
    void stageImportURL()
  })

  /** Hands the selection to a background import and goes to watch it. */
  const startSelectedImport = async () => {
    const preview = importPreview()
    if (!preview.id || selectedImports().length === 0 || !beginMutation()) return
    const isCurrent = review.captureSession()
    setImporting(true)
    setImportError(undefined)
    setImportPhase({ kind: "importing", finished: 0, total: selectedImports().length })
    try {
      const started = await startImportJob(preview.id, {
        resolutions: selectedImports().map((itemID) => ({
          item_id: itemID,
          ...(importChoices()[itemID] ?? { action: "separate" as const }),
        })),
        retainSourceItemIDs: retainedSources().filter((itemID) =>
          selectedImports().includes(itemID),
        ),
      })
      // Unselected recipes stay staged; the job links back to this review.
      if (!isCurrent()) return
      review.acceptJobRemainder(started.remaining)
      await onJob(started.job.id, started.remaining ? preview.id : undefined)
    } catch (error) {
      if (!disposed) setImportError(error)
    } finally {
      endMutation()
      setImporting(false)
      setImportPhase({ kind: "idle" })
    }
  }

  const applyEditedText = async (files: readonly File[]) => {
    const item = editingTextItem()
    const preview = importPreview()
    if (!item || !preview.id || files.length !== 1 || !beginMutation()) return
    const isCurrent = review.captureSession()
    setImportError(undefined)
    try {
      const text = await files[0].text()
      const updated = await editImportPreviewText(preview.id, item.id, text)
      if (!isCurrent()) return
      review.refreshSession(updated)
      setEditingTextItem(undefined)
    } catch (error) {
      if (isCurrent()) setImportError(error)
    } finally {
      endMutation()
    }
  }

  const closeImportPreview = async () => {
    if (previewing()) {
      previewController?.abort()
      return
    }
    if (!beginMutation()) return
    const isCurrent = review.captureSession()
    const staged = importPreview()
    setImportError(undefined)
    try {
      if (staged.id) await cancelImportPreview(staged.id)
      if (isCurrent()) review.closeSession()
    } catch (error) {
      if (isCurrent()) setImportError(error)
    } finally {
      endMutation()
    }
  }

  const cancelStaging = () => previewController?.abort()
  review.onSessionChange(() => setEditingTextItem(undefined))
  onCleanup(() => {
    disposed = true
    cancelStaging()
  })

  return {
    review,
    measurement,
    importing,
    previewing,
    busy: mutationBusy,
    importPhase,
    importError,
    urlImportOpen,
    setURLImportOpen,
    textImportOpen,
    setTextImportOpen,
    editingTextItem,
    setEditingTextItem,
    urlImport,
    setURLImport,
    urlImportError,
    setURLImportError,
    stageImportFiles,
    stageImportURL,
    startSelectedImport,
    applyEditedText,
    closeImportPreview,
    cancelStaging,
  }
}
export type ImportWorkflow = ReturnType<typeof createImportWorkflow>
