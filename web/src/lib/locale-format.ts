/** Resolved account presentation; formatting never changes stored recipe data. */
export interface PresentationContext {
  locale: string
  timeZone: string
}
let resolvePresentation: () => PresentationContext = () => ({ locale: "en", timeZone: "UTC" })

export function configurePresentation(resolver: () => PresentationContext): void {
  resolvePresentation = resolver
}
export function presentationContext(): PresentationContext {
  return resolvePresentation()
}
export function formatNumber(
  value: number | bigint,
  options: Intl.NumberFormatOptions = {},
): string {
  return new Intl.NumberFormat(presentationContext().locale, options).format(value)
}
export function formatPercent(value: number): string {
  return formatNumber(value, { style: "percent", maximumFractionDigits: 1 })
}

/** Only the active decimal separator and ASCII dot are accepted, never grouping.
 * Thus an ambiguous `1,234` in an English editor cannot silently become 1.234.
 * Exact arithmetic happens in the caller after this textual normalization.
 */
export function normalizeNumericInput(
  value: string,
  locale = presentationContext().locale,
): string {
  const formatter = new Intl.NumberFormat(locale, { useGrouping: false })
  const decimal = formatter.formatToParts(1.1).find((part) => part.type === "decimal")?.value ?? "."
  let normalized = value
  for (let digit = 0; digit <= 9; digit++) {
    normalized = normalized.replaceAll(formatter.format(digit), String(digit))
  }
  return decimal === "." ? normalized : normalized.replaceAll(decimal, ".")
}

/** Localize an already rounded, exact decimal string without passing through Number. */
export function localizeDecimal(value: string, locale = presentationContext().locale): string {
  const formatter = new Intl.NumberFormat(locale, { useGrouping: false })
  const decimal = formatter.formatToParts(1.1).find((part) => part.type === "decimal")?.value ?? "."
  return value.replace(/[0-9.]/g, (character) =>
    character === "." ? decimal : formatter.format(Number(character)),
  )
}

/** Joins names into one phrase in the interface language, such as "A, B and C". */
export function formatList(values: string[]): string {
  try {
    return new Intl.ListFormat(presentationContext().locale, { type: "conjunction" }).format(values)
  } catch {
    return values.join(", ")
  }
}
