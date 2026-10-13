// Fails on catalog defects: malformed JSON or messages, duplicate members,
// placeholders that differ from the source, invalid plural categories, unknown
// messages, and locale lists that disagree. Missing translations are reported
// by `i18n:report`, not here.
import fs from "node:fs"

import {
  context,
  duplicateMembers,
  messages,
  readCatalog,
  referencedMessages,
  registry,
  settings,
  shape,
} from "./i18n-catalog.mjs"

const errors = []
const base = settings.baseLocale
const sourceCatalog = readCatalog(base)
errors.push(...duplicateMembers(sourceCatalog.file, sourceCatalog.text))
const source = messages(JSON.parse(sourceCatalog.text))
const sourceShapes = Object.fromEntries(
  Object.entries(source).map(([key, value]) => [key, shape(value)]),
)

for (const [key, value] of Object.entries(sourceShapes)) {
  if (value.malformed) errors.push(`${sourceCatalog.file}: malformed message ${key}`)
  if (typeof source[key] === "string" && !source[key].trim())
    errors.push(`${sourceCatalog.file}: empty message ${key}`)
  if (/^[a-z]+_\d+$/.test(key))
    errors.push(`${sourceCatalog.file}: ${key} does not describe its message`)
}

/** Selector names a message declares as plural. */
const pluralSelectors = (value) =>
  new Set(
    value.flatMap((variant) =>
      (variant.declarations ?? [])
        .map((declaration) => declaration.match(/^local (\w+) = \w+\s*:\s*plural/)?.[1])
        .filter(Boolean),
    ),
  )

for (const locale of settings.locales.filter((tag) => tag !== base)) {
  const { file, text } = readCatalog(locale)
  errors.push(...duplicateMembers(file, text))
  let catalog
  try {
    catalog = messages(JSON.parse(text))
  } catch (error) {
    errors.push(`${file}: ${error.message}`)
    continue
  }
  const categories = new Set([
    ...new Intl.PluralRules(locale).resolvedOptions().pluralCategories,
    "*",
  ])
  for (const [key, value] of Object.entries(catalog)) {
    // An obsolete message is reported by i18n:report and does no harm here.
    if (!(key in source)) continue
    if (typeof value === "string" && !value.trim()) {
      errors.push(`${file}: ${key} is empty; remove it so the source message is used`)
      continue
    }
    if (typeof value !== "string" && !Array.isArray(value)) {
      errors.push(`${file}: ${key} is neither a pattern nor a list of variants`)
      continue
    }
    const translated = shape(value)
    if (translated.malformed) {
      errors.push(`${file}: malformed message ${key}`)
      continue
    }
    const expected = [...sourceShapes[key].inputs].sort().join(", ")
    const actual = [...translated.inputs].sort().join(", ")
    if (expected !== actual)
      errors.push(`${file}: ${key} uses {${actual}} but the source uses {${expected}}`)
    if (!Array.isArray(value)) continue
    const plural = pluralSelectors(value)
    let fallback = false
    for (const variant of translated.variants) {
      const parts = variant.selector.split(",").map((part) => part.trim().split("="))
      if (parts.every(([, category]) => category === "*")) fallback = true
      for (const [name, category] of parts)
        if (plural.has(name) && !categories.has(category))
          errors.push(
            `${file}: ${key} has plural category ${category}, which ${locale} does not use`,
          )
    }
    if (!fallback) errors.push(`${file}: ${key} has no fallback (*) variant`)
  }
}

for (const [key, files] of referencedMessages()) {
  if (!(key in source)) errors.push(`${files[0]}: unknown message ${key}`)
}
for (const key of Object.keys(context)) {
  if (key !== "$comment" && !(key in source))
    errors.push(`messages/context.json: context for unknown message ${key}`)
}

const registered = Object.keys(registry)
if (JSON.stringify([...registered].sort()) !== JSON.stringify([...settings.locales].sort()))
  errors.push("locales.json and project.inlang/settings.json list different locales")
for (const [locale, entry] of Object.entries(registry)) {
  if (entry.status !== "supported" && entry.status !== "preview")
    errors.push(`locales.json: ${locale} has unknown status ${entry.status}`)
  if (entry.direction !== "ltr" && entry.direction !== "rtl")
    errors.push(`locales.json: ${locale} needs a direction`)
}
if (registry[base]?.status !== "supported")
  errors.push(`locales.json: the source locale ${base} must be supported`)

const go = fs.readFileSync("../internal/preferences/preferences.go", "utf8")
const goList = (name) =>
  [
    ...(go.match(new RegExp(`${name}\\s*=\\s*\\[\\]string\\{([^}]*)\\}`))?.[1] ?? "").matchAll(
      /"([^"]+)"/g,
    ),
  ].map((match) => match[1])
const byStatus = (status) => registered.filter((locale) => registry[locale].status === status)
for (const [name, status] of [
  ["SupportedLocales", "supported"],
  ["PreviewLocales", "preview"],
])
  if (JSON.stringify(goList(name)) !== JSON.stringify(byStatus(status)))
    errors.push(
      `${name} in internal/preferences differs from the ${status} locales in locales.json`,
    )

if (errors.length) {
  console.error(errors.join("\n"))
  process.exitCode = 1
} else {
  console.log(
    `Validated ${Object.keys(source).length} messages across ${settings.locales.length} locale(s).`,
  )
}
