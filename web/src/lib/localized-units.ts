import { presentationContext } from "./locale-format.ts"
import { type Rational, toNumber } from "./rational.ts"
import { unitLabelLoaders } from "./unit-locales.generated.ts"

type Label = { forms: Partial<Record<Intl.LDMLPluralRule, string>>; tight?: boolean }
const catalogs = new Map<string, Record<string, Label>>()
const pluralRules = new Map<string, Intl.PluralRules>()

export async function loadUnitLabels(locale: string): Promise<void> {
  if (catalogs.has(locale) || !(locale in unitLabelLoaders)) return
  const catalog = await unitLabelLoaders[locale as keyof typeof unitLabelLoaders]()
  catalogs.set(locale, catalog.default)
}

export function localizedUnitLabel(unit: string, amount?: Rational): string | undefined {
  const locale = presentationContext().locale
  const label = catalogs.get(locale)?.[unit]
  if (!label) return undefined
  let rules = pluralRules.get(locale)
  if (!rules) {
    rules = new Intl.PluralRules(locale, { maximumFractionDigits: 6 })
    pluralRules.set(locale, rules)
  }
  const category = amount ? rules.select(toNumber(amount)) : "other"
  return label.forms[category] ?? label.forms.other
}

export function localizedUnitTight(unit: string): boolean | undefined {
  const label = catalogs.get(presentationContext().locale)?.[unit]
  return label ? Boolean(label.tight) : undefined
}
