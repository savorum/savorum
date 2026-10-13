/** Immutable list updates for editable rows. */
export function replaceAt<T>(values: readonly T[], index: number, value: T): T[] {
  return values.map((entry, at) => (at === index ? value : entry))
}

export function removeAt<T>(values: readonly T[], index: number): T[] {
  return values.filter((_, at) => at !== index)
}
