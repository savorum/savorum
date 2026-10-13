import { For } from "solid-js"
import { css, cva } from "styled-system/css"

const styles = {
  root: css({
    background: "bg.muted",
    borderRadius: "l2",
    display: "inline-flex",
    flexWrap: "wrap",
    maxWidth: "full",
    padding: "0.5",
  }),
}

const option = cva({
  base: {
    background: "transparent",
    borderRadius: "l1",
    borderWidth: "0",
    color: "fg.muted",
    cursor: "pointer",
    fontSize: "xs",
    fontWeight: "medium",
    paddingBlock: "1.5",
    paddingInline: "2.5",
    transition: "colors",
    whiteSpace: "nowrap",
    _hover: { color: "fg.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "1px" },
  },
  variants: {
    size: {
      sm: {},
      md: { fontSize: "md", minHeight: "11", paddingInline: "4" },
    },
    selected: {
      true: { background: "bg.surface", color: "fg.default", boxShadow: "xs" },
      false: {},
    },
  },
})

export interface SegmentedProps<T extends string> {
  label: string
  size?: "sm" | "md"
  value: T
  options: { value: T; label: string }[]
  onChange: (value: T) => void
}

/** A compact either/or control for presentation choices. */
export function Segmented<T extends string>(props: SegmentedProps<T>) {
  return (
    <div class={styles.root} role="group" aria-label={props.label}>
      <For each={props.options}>
        {(entry) => (
          <button
            type="button"
            class={option({ selected: entry.value === props.value, size: props.size ?? "sm" })}
            aria-pressed={entry.value === props.value}
            onClick={() => props.onChange(entry.value)}
          >
            {entry.label}
          </button>
        )}
      </For>
    </div>
  )
}
