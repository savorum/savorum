import type { ImportPreviewDiagnostic, ImportPreviewItem } from "@/api/import-previews"

import { m } from "./messages.ts"

/** Combine repeated messages while retaining the fields they describe. */
export function uniqueImportDiagnostics<
  T extends {
    code: string
    message: string
    class: string
    count: number
    targets?: unknown[] | null
  },
>(diagnostics: readonly T[]): T[] {
  const messages = new Map<string, T>()
  for (const diagnostic of diagnostics) {
    const key = `${diagnostic.class}\u0000${diagnostic.message}`
    const previous = messages.get(key)
    messages.set(
      key,
      previous
        ? {
            ...previous,
            count: previous.count + diagnostic.count,
            targets: [...(previous.targets ?? []), ...(diagnostic.targets ?? [])],
          }
        : diagnostic,
    )
  }
  return [...messages.values()]
}

export function previewDiagnostics(item: ImportPreviewItem): {
  attention: ImportPreviewDiagnostic[]
  details: ImportPreviewDiagnostic[]
} {
  const messages = uniqueImportDiagnostics(item.diagnostics).filter(
    (diagnostic) =>
      !(
        item.analysis.matches.length > 0 &&
        (diagnostic.area === "duplicate" ||
          diagnostic.code === "existing-source-match" ||
          diagnostic.code === "already-in-library")
      ),
  )
  return {
    attention: messages.filter((diagnostic) => diagnostic.class !== "informational"),
    details: messages.filter((diagnostic) => diagnostic.class === "informational"),
  }
}

export interface PreviewNotice {
  code: string
  tone: "danger" | "warning"
  text: string
  post?: string
}

export interface PreviewNotices {
  notices: PreviewNotice[]
  provenance?: string
  post?: string
  details: ImportPreviewDiagnostic[]
}

const countedCodes = new Set(["no-ingredients", "no-instructions", "social-caption-draft"])

const provenanceMessages: Record<string, () => string> = {
  "social-caption": () => m.import_notice_from_caption(),
  "social-comment": () => m.import_notice_from_comment(),
  "social-linked-recipe": () => m.import_notice_from_linked_recipe(),
  "social-creator-recipe": () => m.import_notice_from_creator_site(),
}

const noticeMessages: Record<string, () => string> = {
  "social-creator-recipe": () => m.import_notice_creator_recipe(),
  "text-sections-inferred": () => m.import_notice_check_placement(),
  "plain-text-uncertain": () => m.import_notice_check_placement(),
  "microdata-fallback": () => m.import_notice_details_missing(),
  "ingredient-review-required": () => m.import_notice_measurements(),
}

const noticeRank: Record<string, number> = {
  "social-creator-recipe": 2,
  "ingredient-review-required": 3,
}

export function previewNotices(
  item: ImportPreviewItem,
  options: { measurementsShown?: boolean } = {},
): PreviewNotices {
  const { attention, details } = previewDiagnostics(item)
  const post = item.source.shared_from || (socialCaption(item) ? item.source.url : undefined)
  const notices: (PreviewNotice & { rank: number })[] = []
  const ingredients = item.recipe.ingredient_count
  const steps = item.recipe.step_count
  if (item.can_commit && (ingredients === 0 || steps === 0)) {
    notices.push({
      code: "missing-content",
      tone: "warning",
      rank: 1,
      text:
        ingredients === 0 && steps === 0
          ? m.import_notice_nothing_found()
          : ingredients === 0
            ? m.import_notice_no_ingredients()
            : m.import_notice_no_steps(),
    })
  }
  const seen = new Set<string>()
  for (const diagnostic of attention) {
    if (countedCodes.has(diagnostic.code)) continue
    if (options.measurementsShown && diagnostic.code === "ingredient-review-required") continue
    const text = noticeMessages[diagnostic.code]?.() ?? diagnostic.message
    if (seen.has(text)) continue
    seen.add(text)
    notices.push({
      code: diagnostic.code,
      tone: diagnostic.class === "blocking" ? "danger" : "warning",
      rank: diagnostic.class === "blocking" ? 0 : (noticeRank[diagnostic.code] ?? 4),
      text,
      post: diagnostic.code === "social-creator-recipe" ? post : undefined,
    })
  }
  const provenanceCode = item.diagnostics.find((diagnostic) => provenanceMessages[diagnostic.code])
  return {
    notices: notices
      .sort((left, right) => left.rank - right.rank)
      .map(({ rank: _rank, ...notice }) => notice),
    provenance: provenanceCode ? provenanceMessages[provenanceCode.code]() : undefined,
    post,
    details: details.filter(
      (diagnostic) => !countedCodes.has(diagnostic.code) && !provenanceMessages[diagnostic.code],
    ),
  }
}

function socialCaption(item: ImportPreviewItem) {
  return item.diagnostics.some(
    (diagnostic) => diagnostic.code === "social-caption" || diagnostic.code === "social-comment",
  )
}
