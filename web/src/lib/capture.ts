export function capturedRecipeURL(value?: string): string {
  if (!value) return ""
  const candidates = [
    value.trim(),
    ...(value.match(/https?:\/\/[^\s<>]+/g) ?? []).map((match) => match.replace(/[),.;]+$/, "")),
  ]
  for (const candidate of candidates) {
    try {
      const url = new URL(candidate)
      if (url.protocol === "https:" || url.protocol === "http:") return url.toString()
    } catch {
      /* Shared text can include a title before its link. */
    }
  }
  return ""
}
