import fixedNumberWords from "../../../internal/wording/fixed-numbers.generated.json" with { type: "json" }

interface Reader {
  numbers: RegExp
  after: string[]
  before: string[]
  compact: boolean
}

const number = String.raw`\p{N}+(?:[.,]\p{N}+)?(?:\s+\p{N}+\/\p{N}+|\/\p{N}+)?`
const escape = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
const readers = new Map<string, Reader>(
  fixedNumberWords.map((words) => {
    const span = ["-", "–", "—", ...(words.span ? [escape(words.span)] : [])].join("|")
    return [
      words.locale,
      {
        numbers: new RegExp(String.raw`${number}(?:\s*(?:${span})\s*${number})?`, "gu"),
        after: words.after,
        before: words.before,
        compact: words.compact,
      },
    ]
  }),
)
const ratio = /^\p{N}{2,}\/\p{N}{2,}$/u
const endsInLetter = /\p{L}$/u
const opensWord = /^[\p{L}\p{N}]/u

function readerFor(language: string | undefined): Reader {
  const base = (language ?? "").split(/[-_]/)[0]?.toLowerCase() ?? ""
  return readers.get(base) ?? readers.get("en")!
}

function dimensionContinues(rest: string): boolean {
  return /^[x×]\s*\p{N}/u.test(rest)
}

function follows(reader: Reader, rest: string): boolean {
  const lowered = rest.toLowerCase()
  return reader.after.some((word) => {
    if (!lowered.startsWith(word)) return false
    const tail = lowered.slice(word.length)
    return reader.compact || !endsInLetter.test(word) || !opensWord.test(tail)
  })
}

function precedes(reader: Reader, head: string): boolean {
  const lowered = head.trimEnd().toLowerCase()
  return reader.before.some((word) => {
    if (!lowered.endsWith(word)) return false
    return reader.compact || !endsInLetter.test(lowered.slice(0, -word.length))
  })
}

/** Keep this classifier in sync with internal/wording. */
export function statesAmount(text: string | undefined, language: string | undefined): boolean {
  if (!text) return false
  const reader = readerFor(language)
  const matches = [...text.matchAll(reader.numbers)]
  let fixed = false
  // Read dimensions right to left so "9 x 13-inch" applies the unit to both numbers.
  for (const match of matches.reverse()) {
    const start = match.index
    const end = start + match[0].length
    const rest = text.slice(end).replace(/^[ \-‐]+/u, "")
    if (!dimensionContinues(rest)) {
      fixed =
        ratio.test(match[0]) || follows(reader, rest) || precedes(reader, text.slice(0, start))
    }
    if (!fixed) return true
  }
  return false
}
