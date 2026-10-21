export function hasGroupContent(rows: readonly unknown[] | null | undefined): boolean {
  return rows?.some(hasContent) ?? false
}

function hasContent(value: unknown): boolean {
  if (typeof value === "string") return value.trim().length > 0
  if (Array.isArray(value)) return value.some(hasContent)
  if (value && typeof value === "object")
    return Object.entries(value).some(([key, entry]) => key !== "id" && hasContent(entry))
  return typeof value === "number" || value === true
}
