import { Select } from "@/components/ui/select"
import { type MeasurementStyle, unitOptions } from "@/lib/unit-choices"

import { m } from "../../lib/messages.ts"

export function UnitSelect(props: {
  value: string | undefined
  label: string
  style: MeasurementStyle
  onChange: (unit: string | undefined) => void
}) {
  return (
    <Select
      size="sm"
      aria-label={props.label}
      value={props.value ?? ""}
      onValueChange={(next) => props.onChange(next || undefined)}
      items={unitOptions(props.style, props.value, m.ingredient_details_no_measure())}
    />
  )
}
