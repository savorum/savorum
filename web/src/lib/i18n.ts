import { createEffect } from "solid-js"

import { baseLocale, getLocale, isLocale, overwriteGetLocale } from "../paraglide/runtime.js"
import { languageDirection } from "./direction"
import { uiLocale } from "./preferences"

/**
 * Only install after hydration: the shipped SPA shell always prerenders in
 * English. `onChange` runs when the language changes after that, for text
 * such as route titles that was written before the change.
 */
export function configureLocale(onChange?: () => void): void {
  overwriteGetLocale(() => {
    const locale = uiLocale()
    return isLocale(locale) ? locale : baseLocale
  })
  let applied: string | undefined
  // The interface direction follows its language; recipe content sets its own.
  createEffect(() => {
    const locale = getLocale()
    document.documentElement.lang = locale
    document.documentElement.dir = languageDirection(locale) ?? "ltr"
    if (applied !== undefined && applied !== locale) onChange?.()
    applied = locale
  })
}

/** Attributes that keep interface controls in the interface's direction inside recipe content. */
export function interfaceAttributes(): { lang: string; dir: "ltr" | "rtl" } {
  const locale = getLocale()
  return { lang: locale, dir: languageDirection(locale) ?? "ltr" }
}
