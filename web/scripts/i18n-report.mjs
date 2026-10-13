// Reports translation progress without failing a development build:
// messages missing from each locale, translations the source no longer has,
// and source messages nothing in the application reads.
//
//   pnpm run i18n:report                 summary for every locale
//   pnpm run i18n:report -- --locale ar  also list what ar lacks and no longer needs
//   pnpm run i18n:report -- --supported  fail when a supported locale is incomplete
import { messages, readCatalog, referencedMessages, registry, settings } from "./i18n-catalog.mjs"

const argumentsList = process.argv.slice(2)
const detailed = argumentsList.includes("--locale")
  ? argumentsList[argumentsList.indexOf("--locale") + 1]
  : undefined
const enforceSupported = argumentsList.includes("--supported")

const base = settings.baseLocale
const source = messages(JSON.parse(readCatalog(base).text))
const sourceKeys = Object.keys(source)
const referenced = referencedMessages()
const unused = sourceKeys.filter((key) => !referenced.has(key))
let incompleteSupported = false

console.log(
  `Source ${base}: ${sourceKeys.length} messages, ${unused.length} not read by the application.`,
)
for (const key of unused) console.log(`  unused  ${key}`)

for (const locale of settings.locales.filter((tag) => tag !== base)) {
  const catalog = messages(JSON.parse(readCatalog(locale).text))
  const missing = sourceKeys.filter((key) => !(key in catalog))
  const obsolete = Object.keys(catalog).filter((key) => !(key in source))
  const translated = sourceKeys.length - missing.length
  const percent = Math.floor((translated / sourceKeys.length) * 1000) / 10
  const status = registry[locale]?.status ?? "unregistered"
  console.log(
    `\n${locale} (${status}): ${translated}/${sourceKeys.length} translated (${percent}%), ` +
      `${missing.length} missing, ${obsolete.length} obsolete.`,
  )
  if (status === "supported" && missing.length > 0) incompleteSupported = true
  if (locale === detailed) {
    for (const key of missing) console.log(`  missing   ${key}: ${JSON.stringify(source[key])}`)
    for (const key of obsolete) console.log(`  obsolete  ${key}`)
  }
}

if (enforceSupported && incompleteSupported) {
  console.error(
    "\nA supported locale is missing messages. Translate them or mark the locale preview.",
  )
  process.exitCode = 1
}
