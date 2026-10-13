import { unwrap } from "solid-js/store"

import type { components } from "@/api/schema"

type Quantity = components["schemas"]["Quantity"]
type Simple = components["schemas"]["SimpleQuantity"]
type Option = components["schemas"]["AlternativeOption"]
export type QuantityShape = "simple" | "sum" | "alternatives"
/** An empty simple amount, the starting term or option of a composite quantity. */
export const blankQuantity = (): Simple => ({ kind: "simple", amount: {} })

export interface QuantityShapePolicy {
  /** The ingredient dialog always opens a sum with at least two editable terms. */
  minimumSumTerms?: number
  /** Generic quantity editors preserve metadata; ingredient shape changes retain amount/unit only. */
  preserveMetadata?: boolean
}

export function quantityWithShape(
  value: Quantity | undefined,
  shape: QuantityShape,
  policy: QuantityShapePolicy = {},
): Quantity {
  // Snapshot before nesting: a store setter can otherwise nest a live proxy
  // into itself while changing the quantity's shape.
  const current = value ? structuredClone(unwrap(value)) : undefined
  const simple = (value: Simple): Simple =>
    policy.preserveMetadata === false
      ? { kind: "simple", amount: value.amount ?? {}, unit: value.unit }
      : value
  const first = (quantity: Quantity | undefined): Simple => {
    if (quantity?.kind === "simple") return simple(quantity as Simple)
    if (quantity?.kind === "sum") return simple(quantity.terms?.[0] ?? blankQuantity())
    if (quantity?.kind === "alternatives") return first(quantity.options?.[0])
    return blankQuantity()
  }
  if (current?.kind === shape) return current
  if (shape === "simple") return first(current)
  if (shape === "sum") {
    const option = current?.kind === "alternatives" ? current.options?.[0] : undefined
    if (option?.kind === "sum" && (option.terms?.length ?? 0) >= (policy.minimumSumTerms ?? 0)) {
      return policy.preserveMetadata === false ? { kind: "sum", terms: option.terms ?? [] } : option
    }
    return { kind: "sum", terms: [first(current), blankQuantity()] }
  }
  return {
    kind: "alternatives",
    options: [
      current?.kind === "sum"
        ? policy.preserveMetadata === false
          ? { kind: "sum", terms: current.terms ?? [] }
          : (current as Option)
        : first(current),
      blankQuantity(),
    ],
  }
}
