import vocabulary from "../../../internal/recipetext/vocabulary.json" with { type: "json" }

export interface SourceTiming {
  kind: "cook" | "rest"
  amount: string
  maximum?: string
  unit: string
  source: string
}

const escapePattern = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
const alternatives = (words: string[]) =>
  `(?:${[...words]
    .sort((a, b) => b.length - a.length)
    .map((word) => escapePattern(word))
    .join("|")})`
const amount = String.raw`(\p{Decimal_Number}+(?:[.,٫]\p{Decimal_Number}+)?)`
const patterns = vocabulary.flatMap((entry) => {
  if (entry.locale === "en") return []
  const before = entry.compact ? "" : String.raw`(?:^|[^\p{L}\p{N}_])`
  const after = entry.compact ? "" : String.raw`(?=$|[^\p{L}\p{N}_])`
  return (["cook", "rest"] as const).flatMap((kind) =>
    Object.entries(entry.units).map(([unit, labels]) => ({
      kind,
      unit,
      action: new RegExp(before + alternatives(entry.actions[kind]) + after, "iu"),
      cadence: new RegExp(before + alternatives(entry.cadence) + after, "iu"),
      duration: new RegExp(
        String.raw`(?:^|[^\p{N}.,٫٬/'’+−\-])(` +
          amount +
          String.raw`\s*(?:(?:-|–|—|` +
          escapePattern(entry.span) +
          String.raw`)\s*` +
          amount +
          String.raw`\s*)?` +
          alternatives(labels) +
          ")" +
          after,
        "giu",
      ),
    })),
  )
})

// Unicode decimal blocks consist of ordered runs of ten digits.
const digitValue = (character: string) => {
  let point = character.codePointAt(0) ?? 0
  let digit = 0
  while (point > 0 && /\p{Decimal_Number}/u.test(String.fromCodePoint(--point))) digit++
  return String(digit % 10)
}
const numericText = (value: string) =>
  value.replace(/\p{Decimal_Number}/gu, digitValue).replace(/[,٫]/g, ".")

/** Recognizes explicit source vocabulary; ambiguous clauses and cadences remain prose. */
export function sourceTimings(text: string): SourceTiming[] {
  return text.split(/[!?;\n。！？；]|\.(?:\s|$)/u).flatMap((clause) => {
    const kinds = new Set(
      patterns.filter((pattern) => pattern.action.test(clause)).map((pattern) => pattern.kind),
    )
    if (kinds.size !== 1) return []
    const found = new Map<string, SourceTiming>()
    for (const pattern of patterns) {
      const action = pattern.action.exec(clause)
      if (!action || pattern.cadence.test(clause)) continue
      for (const match of clause.matchAll(pattern.duration)) {
        const end = match.index + match[0].length
        const start = end - match[1].length
        if (/[\p{Decimal_Number}/+−'’.,٫٬-]$/u.test(clause.slice(0, start).trimEnd())) continue
        const gap =
          end < action.index
            ? clause.slice(end, action.index)
            : start > action.index + action[0].length
              ? clause.slice(action.index + action[0].length, start)
              : ""
        if (Array.from(gap).length > 80) continue
        const key = `${start}:${end}`
        if (!found.has(key))
          found.set(key, {
            kind: pattern.kind,
            unit: pattern.unit,
            amount: numericText(match[2]),
            maximum: match[3] ? numericText(match[3]) : undefined,
            source: match[1].trim(),
          })
      }
    }
    if (found.size > 1) return []
    return [...found.values()]
  })
}
