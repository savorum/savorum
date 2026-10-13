import type { components } from "@/api/schema"

type Temperature = components["schemas"]["Temperature"]

/** Changing form is explicit; source wording moves to the field that form permits. */
export function temperatureWithFormat(
  current: Temperature,
  format: "text" | "numeric",
): Temperature {
  const { text, amount, unit, source_text, ...metadata } = current
  if (format === "text") return { ...metadata, text: text ?? source_text ?? "" }
  return {
    ...metadata,
    amount: amount ?? {},
    unit: unit ?? "temperature.celsius",
    source_text: text || source_text,
  }
}

export function hasTemperatureValue(temperature: Temperature): boolean {
  return (
    Boolean(temperature.text?.trim()) ||
    temperature.amount?.value !== undefined ||
    temperature.amount?.min !== undefined
  )
}
