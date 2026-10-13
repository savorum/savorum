export function safeSourceURL(value?: string): string | undefined {
  if (!value) return undefined
  try {
    const parsed = new URL(value)
    return parsed.protocol === "http:" || parsed.protocol === "https:"
      ? parsed.toString()
      : undefined
  } catch {
    return undefined
  }
}

export function sourceHost(value?: string): string | undefined {
  const url = safeSourceURL(value)
  if (!url) return undefined
  return new URL(url).hostname.replace(/^www\./, "")
}
