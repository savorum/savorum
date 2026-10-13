import type { ImportPreviewDiagnostic, ImportPreviewItem } from "@/api/import-previews"

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
