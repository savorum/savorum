import type * as en from "../paraglide/messages/en.js"
import {
  baseLocale,
  getLocale,
  isLocale,
  type Locale,
  type LocalizedString,
} from "../paraglide/runtime.js"
import { loadUnitLabels } from "./localized-units.ts"

type Catalog = typeof en

const loaders = {
  ar: () => import("../paraglide/messages/ar.js"),
  cs: () => import("../paraglide/messages/cs.js"),
  da: () => import("../paraglide/messages/da.js"),
  de: () => import("../paraglide/messages/de.js"),
  el: () => import("../paraglide/messages/el.js"),
  es: () => import("../paraglide/messages/es.js"),
  fi: () => import("../paraglide/messages/fi.js"),
  fr: () => import("../paraglide/messages/fr.js"),
  he: () => import("../paraglide/messages/he.js"),
  hu: () => import("../paraglide/messages/hu.js"),
  id: () => import("../paraglide/messages/id.js"),
  it: () => import("../paraglide/messages/it.js"),
  ja: () => import("../paraglide/messages/ja.js"),
  ko: () => import("../paraglide/messages/ko.js"),
  nb: () => import("../paraglide/messages/nb.js"),
  nl: () => import("../paraglide/messages/nl.js"),
  pl: () => import("../paraglide/messages/pl.js"),
  "pt-BR": () => import("../paraglide/messages/pt-BR.js"),
  "pt-PT": () => import("../paraglide/messages/pt-PT.js"),
  ro: () => import("../paraglide/messages/ro.js"),
  ru: () => import("../paraglide/messages/ru.js"),
  sv: () => import("../paraglide/messages/sv.js"),
  th: () => import("../paraglide/messages/th.js"),
  tr: () => import("../paraglide/messages/tr.js"),
  uk: () => import("../paraglide/messages/uk.js"),
  vi: () => import("../paraglide/messages/vi.js"),
  "zh-Hans": () => import("../paraglide/messages/zh-Hans.js"),
  "zh-Hant": () => import("../paraglide/messages/zh-Hant.js"),
} satisfies Record<Exclude<Locale, "en">, () => Promise<Catalog>>

const catalogs = new Map<Locale, Catalog>()
const pending = new Map<Locale, Promise<void>>()

/** Load the compiled catalog before applying a locale to account preferences. */
export async function loadMessages(value: string | undefined): Promise<void> {
  const locale = isLocale(value) ? value : baseLocale
  if (locale === baseLocale || catalogs.has(locale)) return
  const existing = pending.get(locale)
  if (existing) return existing
  const request = Promise.all([loaders[locale as Exclude<Locale, "en">](), loadUnitLabels(locale)])
    .then(([catalog]) => {
      catalogs.set(locale, catalog)
    })
    .finally(() => {
      pending.delete(locale)
    })
  pending.set(locale, request)
  return request
}

/** Each message carries only its own English fallback into its consuming chunk. */
export function localized<Key extends keyof Catalog>(
  key: Key,
  fallback: (inputs: Parameters<Catalog[Key]>[0]) => string,
) {
  return (inputs?: Parameters<Catalog[Key]>[0], options?: { locale?: Locale }) => {
    const locale = options?.locale ?? getLocale()
    if (locale === baseLocale) return fallback((inputs ?? {}) as never) as LocalizedString
    const catalog = catalogs.get(locale)
    if (!catalog) throw new Error(`Messages for ${locale} are not loaded`)
    return catalog[key]((inputs ?? {}) as never)
  }
}
