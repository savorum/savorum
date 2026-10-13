import type { ImportPreviewItem } from "@/api/import-previews"

import { formatNumber } from "./locale-format.ts"
import { m } from "./messages.ts"

/**
 * What a reviewer has to do about a candidate before importing it.
 * Informational preservation details are separate from standing.
 */
export type CandidateStanding = "blocked" | "decision" | "review" | "already-here" | "ready"

export function candidateStanding(item: ImportPreviewItem): CandidateStanding {
  if (!item.can_commit) return "blocked"
  if (item.analysis.status !== "new") return "decision"
  if (item.diagnostics.some((diagnostic) => diagnostic.class === "actionable")) return "review"
  if (item.already_here) return "already-here"
  return "ready"
}

export const standingLabels: Record<CandidateStanding, string> = {
  get blocked() {
    return m.import_candidate_blocked()
  },
  get decision() {
    return m.import_candidate_decision()
  },
  get review() {
    return m.import_candidate_review()
  },
  get "already-here"() {
    return m.import_candidate_already_here()
  },
  get ready() {
    return m.import_candidate_ready()
  },
}

/**
 * Whether this recipe arrived with something worth reading but nothing to
 * decide. It is shown on the row and counted separately from standing.
 */
export function hasPreservedDetails(item: ImportPreviewItem): boolean {
  return item.diagnostics.some(
    (diagnostic) => diagnostic.class === "informational" && diagnostic.area !== "duplicate",
  )
}

export interface CandidateSummary {
  total: number
  ready: number
  decisions: number
  review: number
  blocked: number
  alreadyHere: number
  informational: number
}

export function summarizeCandidates(items: readonly ImportPreviewItem[]): CandidateSummary {
  const summary: CandidateSummary = {
    total: items.length,
    ready: 0,
    decisions: 0,
    review: 0,
    blocked: 0,
    alreadyHere: 0,
    informational: 0,
  }
  for (const item of items) {
    switch (candidateStanding(item)) {
      case "blocked":
        summary.blocked += 1
        break
      case "decision":
        summary.decisions += 1
        break
      case "review":
        summary.review += 1
        break
      case "already-here":
        summary.alreadyHere += 1
        break
      case "ready":
        summary.ready += 1
        break
    }
    if (hasPreservedDetails(item)) summary.informational += 1
  }
  return summary
}

/**
 * What this import will do, in the order a person asks about it: what arrives,
 * what they have to decide first, and what is being left out. Zero counts are
 * omitted.
 */
export function describeCandidateOutcome(summary: CandidateSummary): string[] {
  const lines: string[] = []
  const arriving = summary.ready + summary.review
  lines.push(
    arriving > 0
      ? m.import_review_panel_what_is({ count: arriving })
      : m.import_review_panel_nothing_to_add(),
  )
  if (summary.decisions > 0)
    lines.push(m.import_review_panel_needs_decision({ count: summary.decisions }))
  if (summary.alreadyHere > 0)
    lines.push(m.import_review_panel_already_here_note({ count: summary.alreadyHere }))
  if (summary.blocked > 0)
    lines.push(m.import_review_panel_blocked_note({ count: summary.blocked }))
  return lines
}

export type CandidateFilter = "all" | CandidateStanding | "informational"

export function candidateMatchesFilter(item: ImportPreviewItem, filter: CandidateFilter): boolean {
  if (filter === "all") return true
  if (filter === "informational") return hasPreservedDetails(item)
  return candidateStanding(item) === filter
}

export interface CandidateFact {
  label: string
  value: string
}

/** The facts worth seeing without opening a recipe, each one separate. */
export function candidateFacts(item: ImportPreviewItem): CandidateFact[] {
  const facts: CandidateFact[] = []
  if (item.recipe.yield)
    facts.push({
      get label() {
        return m.import_candidate_yield()
      },
      value: item.recipe.yield,
    })
  // The label already names the noun, so the value is the number. "Ingredients
  // 8 ingredients" says it twice.
  facts.push({
    get label() {
      return m.import_candidate_ingredients_label()
    },
    value: formatNumber(item.recipe.ingredient_count),
  })
  facts.push({
    get label() {
      return m.import_candidate_steps()
    },
    value: formatNumber(item.recipe.step_count),
  })
  const photographs = describePhotographs(item)
  if (photographs)
    facts.push({
      get label() {
        return m.import_candidate_photographs()
      },
      value: photographs,
    })
  return facts
}

/**
 * What will become of this recipe's photographs, in one phrase. Only an
 * unavailable photograph is named; deferred ones are just counted.
 */
function describePhotographs(item: ImportPreviewItem): string | undefined {
  const { discovered, unavailable } = item.assets
  if (discovered === 0) return undefined
  if (unavailable === 0) return formatNumber(discovered)
  if (unavailable === discovered)
    return m.import_candidate_none_available({ value1: formatNumber(discovered) })
  return m.import_candidate_unavailable({
    value1: formatNumber(discovered),
    value2: formatNumber(unavailable),
  })
}

type ImportAnalysisMatch = ImportPreviewItem["analysis"]["matches"][number]

/**
 * What one match is, as separate facts, naming its evidence ("same source
 * address" or "84% similar").
 */
export function matchFacts(match: ImportAnalysisMatch): string[] {
  const facts: string[] = []
  if (match.authors.length > 0) facts.push(match.authors.join(", "))
  if (match.yield) facts.push(match.yield)
  facts.push(m.import_candidate_ingredient_count({ count: match.ingredient_count }))
  facts.push(describeMatchEvidence(match))
  return facts
}

function describeMatchEvidence(match: ImportAnalysisMatch): string {
  switch (match.evidence) {
    case "source-identity":
      return m.import_candidate_match_evidence_source_identity()
    case "canonical-url":
      return m.import_candidate_match_evidence_canonical_url()
    case "portable-id":
      return m.import_candidate_match_evidence_portable_id()
    case "similarity":
      return m.import_candidate_match_evidence_similarity({
        value1: formatNumber(Math.round(match.score * 100)),
      })
  }
}
