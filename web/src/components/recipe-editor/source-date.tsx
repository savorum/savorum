import { Show, createMemo, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { formatDate, formatSourceDateTime } from "@/lib/date-format"

import { m } from "../../lib/messages.ts"

const styles = {
  kept: css({ display: "grid", gap: "2", justifyItems: "start" }),
  value: css({ color: "fg.default", fontSize: "sm", margin: "0" }),
  note: css({ color: "fg.muted", fontSize: "xs", lineHeight: "1.5", margin: "0" }),
}

const dateOnly = /^\d{4}-\d{2}-\d{2}$/

/**
 * A date the recipe or its source carries, as a date control. A precise
 * timestamp from a source is kept as written unless deliberately replaced.
 */
export function SourceDateField(props: {
  label: string
  hint?: string
  value: string | undefined
  onChange: (value: string) => void
}) {
  const [replacing, setReplacing] = createSignal(false)
  const stored = () => props.value ?? ""
  const precise = createMemo(() => stored() !== "" && !dateOnly.test(stored()))
  const reading = () => {
    const value = stored()
    if (value === "") return ""
    return precise() ? formatSourceDateTime(value) : formatDate(value)
  }
  return (
    <Show
      when={!precise() || replacing()}
      fallback={
        <FormField label={props.label} hint={props.hint}>
          <div class={styles.kept}>
            <p class={styles.value}>{reading()}</p>
            <p class={styles.note}>{m.source_date_kept_as_the_source_wrote_it()}</p>
            <Button size="sm" variant="outline" onClick={() => setReplacing(true)}>
              {m.source_date_replace_with_a_date()}
            </Button>
          </div>
        </FormField>
      }
    >
      <FormField label={props.label} hint={props.hint}>
        <Input
          size="sm"
          type="date"
          value={dateOnly.test(stored()) ? stored() : ""}
          onInput={(event) => props.onChange(event.currentTarget.value)}
        />
      </FormField>
    </Show>
  )
}
