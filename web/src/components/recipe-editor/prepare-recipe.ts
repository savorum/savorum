import { unwrap } from "solid-js/store"

import type { Recipe } from "@/api/client"
import { durationStated } from "@/lib/duration"
import { cleanRecipe, parseDurationInput } from "@/lib/recipe-draft"

/** Snapshot and normalize only the submission; the editing draft remains untouched. */
export function prepareRecipe(draft: Recipe): Recipe {
  const value = structuredClone(unwrap(draft))
  if (value.times) {
    for (const field of ["prep", "cook", "additional", "total"] as const) {
      const entry = value.times[field]
      if (!entry) continue
      // Opaque wording is what the recipe said and has nothing to normalize.
      if (entry.text) continue
      value.times[field] = {
        ...entry,
        value: entry.value ? (parseDurationInput(entry.value) ?? entry.value) : undefined,
        min: entry.min ? (parseDurationInput(entry.min) ?? entry.min) : undefined,
        max: entry.max ? (parseDurationInput(entry.max) ?? entry.max) : undefined,
      }
    }
  }
  if ((value.times?.assertions ?? []).length > 0) {
    // A blank row is only progressive-disclosure UI, never recipe data.
    const assertions = (value.times?.assertions ?? [])
      .map((assertion) => ({
        ...assertion,
        duration: assertion.duration.text
          ? assertion.duration
          : {
              ...assertion.duration,
              value: assertion.duration.value
                ? (parseDurationInput(assertion.duration.value) ?? assertion.duration.value)
                : undefined,
              min: assertion.duration.min
                ? (parseDurationInput(assertion.duration.min) ?? assertion.duration.min)
                : undefined,
              max: assertion.duration.max
                ? (parseDurationInput(assertion.duration.max) ?? assertion.duration.max)
                : undefined,
            },
      }))
      .filter((assertion) => durationStated(assertion.duration))
    value.times!.assertions = assertions.length > 0 ? assertions : undefined
    // A reference to a row that was only ever an empty box has to go too.
    const kept = new Set(assertions.map((assertion) => assertion.id))
    for (const section of value.instruction_sections ?? [])
      for (const step of section.steps ?? [])
        if (step.timing_refs) step.timing_refs = step.timing_refs.filter((ref) => kept.has(ref))
  }
  cleanFacts(value)
  return cleanRecipe(value)
}

const filled = (text?: string) => (text?.trim() ? text.trim() : undefined)

/** Drops rows the editor offered but nobody filled in. */
function cleanFacts(value: Recipe) {
  const yields = (value.yields ?? [])
    .map((item) => ({ ...item, text: filled(item.text), label: filled(item.label) }))
    .filter((item) => item.text || item.quantity)
  value.yields = yields.length > 0 ? yields : undefined

  const nutrition = (value.nutrition ?? [])
    .map((statement) => ({
      ...statement,
      note: filled(statement.note),
      basis: { ...statement.basis, text: filled(statement.basis.text) },
      nutrients: (statement.nutrients ?? [])
        .map((nutrient) => ({
          ...nutrient,
          label: filled(nutrient.label),
          text: filled(nutrient.text),
        }))
        .filter((nutrient) => nutrient.amount || nutrient.text),
    }))
    .filter((statement) => statement.nutrients.length > 0)
  value.nutrition = nutrition.length > 0 ? nutrition : undefined

  const claims = (value.dietary_claims ?? [])
    .map((claim) => ({ ...claim, condition: filled(claim.condition), text: claim.text.trim() }))
    .filter((claim) => claim.text)
  value.dietary_claims = claims.length > 0 ? claims : undefined

  const declarations = (value.allergen_declarations ?? [])
    .map((item) => ({
      ...item,
      condition: filled(item.condition),
      text: item.text.trim() || item.substance.trim(),
      substance: item.substance.trim(),
    }))
    .filter((item) => item.substance)
  value.allergen_declarations = declarations.length > 0 ? declarations : undefined

  for (const asset of value.assets ?? []) {
    for (const field of ["creator", "credit", "rights", "source_url"] as const)
      asset[field] = filled(asset[field])
    const name = filled(asset.license?.name)
    const url = filled(asset.license?.url)
    asset.license = name || url ? { name, url } : undefined
  }
}
