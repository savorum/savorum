import type { ImportPreview, ImportPreviewItem } from "@/api/import-previews"

import { candidateStanding } from "./import-candidate.ts"

export type ImportChoice = { action: "separate" | "update" | "skip"; target_entry_id?: string }

export interface ImportReviewState {
  selected: string[]
  retainedSources: string[]
  choices: Record<string, ImportChoice>
}

function defaultChoice(item: ImportPreviewItem): ImportChoice {
  const match = item.analysis.matches[0]
  if (item.analysis.status === "definite-match" && match) {
    return { action: "skip", target_entry_id: match.entry_id }
  }
  return { action: "separate" }
}

function validChoice(item: ImportPreviewItem, choice: ImportChoice | undefined): boolean {
  if (!choice || !item.can_commit) return false
  if (choice.action === "separate") return true
  return Boolean(
    item.analysis.matches.some((match) => match.entry_id === choice.target_entry_id) &&
    (choice.action !== "update" || item.analysis.status === "definite-match"),
  )
}

/**
 * Reconciles answers with a changed version of the same import preview.
 *
 * This is intentionally different from initializing a new upload: answers
 * survive for candidates that still exist, while answers that no longer fit
 * the candidate's eligibility or duplicate matches fall back to the new
 * candidate's safe default.
 */
export function reconcileImportReviewState(
  previous: ImportPreview,
  next: ImportPreview,
  current: ImportReviewState,
): ImportReviewState {
  const nextByID = new Map(next.items.map((item) => [item.id, item]))
  const previousByID = new Map(previous.items.map((item) => [item.id, item]))

  const selected = current.selected.filter((id) => nextByID.get(id)?.can_commit === true)
  const retainedSources = current.retainedSources.filter(
    (id) => nextByID.get(id)?.source.can_retain === true,
  )
  const choices: Record<string, ImportChoice> = {}

  for (const item of next.items) {
    const oldItem = previousByID.get(item.id)
    const oldChoice = oldItem ? current.choices[item.id] : undefined
    // A candidate that was not importable without review has no prior
    // selection to preserve. Once its blocker is resolved, apply the same
    // helpful default as a newly initialized preview. A candidate that was
    // already ready but explicitly deselected remains deselected.
    if (
      oldItem &&
      selectedByDefault(item) &&
      !selectedByDefault(oldItem) &&
      !selected.includes(item.id)
    ) {
      selected.push(item.id)
    }
    choices[item.id] = validChoice(item, oldChoice) ? oldChoice! : defaultChoice(item)
  }

  return { selected, retainedSources, choices }
}

/**
 * Whether a candidate is selected when a preview first appears. Recipes with
 * open measurement questions are selected; duplicates are not, since only the
 * person can decide what to do with them.
 */
function selectedByDefault(item: ImportPreviewItem): boolean {
  const standing = candidateStanding(item)
  return standing === "ready" || standing === "review"
}

/** The initial answers for a newly staged preview. */
export function initializeImportReviewState(preview: ImportPreview): ImportReviewState {
  return {
    selected: preview.items.filter(selectedByDefault).map((item) => item.id),
    retainedSources: [],
    choices: Object.fromEntries(preview.items.map((item) => [item.id, defaultChoice(item)])),
  }
}
