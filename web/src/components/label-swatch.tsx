import { Show } from "solid-js"
import { css } from "styled-system/css"

/** The dot that carries a tag's colour wherever the tag appears. */
export const LabelSwatch = (props: { color?: string; size?: "sm" | "md" }) => (
  <Show when={props.color}>
    <span
      class={props.size === "sm" ? styles.small : styles.swatch}
      style={{ background: props.color }}
      aria-hidden="true"
    />
  </Show>
)

const styles = {
  swatch: css({ borderRadius: "full", flexShrink: "0", height: "2", width: "2" }),
  small: css({ borderRadius: "full", flexShrink: "0", height: "1.5", width: "1.5" }),
}
