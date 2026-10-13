export type Direction = "ltr" | "rtl"

// Scripts and languages written right to left, for engines without
// Intl.Locale#getTextInfo.
const rtlScripts = new Set(["Adlm", "Arab", "Hebr", "Mand", "Nkoo", "Rohg", "Samr", "Syrc", "Thaa"])
const rtlLanguages = new Set([
  "ar",
  "arc",
  "azb",
  "ckb",
  "dv",
  "fa",
  "he",
  "ks",
  "mzn",
  "nqo",
  "pnb",
  "ps",
  "sd",
  "syr",
  "ug",
  "ur",
  "yi",
])

interface TextInfoLocale extends Intl.Locale {
  getTextInfo?: () => { direction?: string }
  textInfo?: { direction?: string }
}

/** The writing direction of a BCP 47 tag, or undefined when the tag is absent or malformed. */
export function languageDirection(tag: string | undefined): Direction | undefined {
  if (!tag) return undefined
  let locale: TextInfoLocale
  try {
    locale = new Intl.Locale(tag) as TextInfoLocale
  } catch {
    return undefined
  }
  const info = locale.getTextInfo?.() ?? locale.textInfo
  if (info?.direction === "rtl" || info?.direction === "ltr") return info.direction
  if (locale.script) return rtlScripts.has(locale.script) ? "rtl" : "ltr"
  return rtlLanguages.has(locale.language) ? "rtl" : "ltr"
}

/**
 * Language and direction attributes for authored content. An unknown language
 * leaves direction to each piece of text, so mixed-script content is never
 * forced into the surrounding interface's direction.
 */
export function contentAttributes(tag: string | undefined): {
  lang?: string
  dir: Direction | "auto"
} {
  const direction = languageDirection(tag)
  return direction ? { lang: tag, dir: direction } : { dir: "auto" }
}

/** A language's name in that language, such as "Français" for `fr`. */
export function nativeLanguageName(tag: string): string {
  try {
    const name = new Intl.DisplayNames([tag], { type: "language" }).of(tag)
    if (name && name !== tag) return name.charAt(0).toLocaleUpperCase(tag) + name.slice(1)
  } catch {
    // A malformed tag is shown as written.
  }
  return tag
}

/** A language's name in the interface language. */
export function languageName(tag: string, interfaceLocale: string): string {
  try {
    return new Intl.DisplayNames([interfaceLocale], { type: "language" }).of(tag) ?? tag
  } catch {
    return tag
  }
}

/** The step an inline arrow key takes in reading order: 1 forward, -1 back, 0 neither. */
export function inlineArrowStep(key: string, element: Element | null): -1 | 0 | 1 {
  if (key !== "ArrowLeft" && key !== "ArrowRight") return 0
  const rtl = element ? getComputedStyle(element).direction === "rtl" : false
  return (key === "ArrowRight") !== rtl ? 1 : -1
}

const rtlLetters =
  /[\p{Script=Adlam}\p{Script=Arabic}\p{Script=Hanifi_Rohingya}\p{Script=Hebrew}\p{Script=Mandaic}\p{Script=Nko}\p{Script=Samaritan}\p{Script=Syriac}\p{Script=Thaana}]/gu

function interfaceDirection(): Direction {
  return typeof document !== "undefined" && document.documentElement.dir === "rtl" ? "rtl" : "ltr"
}

/**
 * Isolates authored text placed inside an interface sentence, so a title in
 * another script cannot reorder the sentence around it. Text with no letters
 * of the opposite direction is returned unchanged, keeping the isolates out of
 * accessible names where they have no effect. Presentation only: the isolates
 * are never stored.
 */
export function isolate(text: string, surrounding: Direction = interfaceDirection()): string {
  const opposite =
    surrounding === "ltr"
      ? text.search(rtlLetters) >= 0
      : /\p{L}/u.test(text.replace(rtlLetters, ""))
  return opposite ? `\u2068${text}\u2069` : text
}
