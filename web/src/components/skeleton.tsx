import { createSignal, For, type JSX, onCleanup, onMount, Show } from "solid-js"
import { css, cx } from "styled-system/css"

import { m } from "../lib/messages.ts"

/** One status for assistive technology; the shapes inside are hidden from it. */
export function SkeletonGroup(props: { class?: string; label?: string; children: JSX.Element }) {
  const [slow, setSlow] = createSignal(false)
  onMount(() => {
    const timer = window.setTimeout(() => setSlow(true), 10000)
    onCleanup(() => window.clearTimeout(timer))
  })
  return (
    <div class={props.class} role="status" aria-live="polite">
      <span class={styles.label}>{props.label ?? m.common_loading()}</span>
      <Show when={slow()}>
        <p class={styles.slow}>{m.status_loading_slow()}</p>
      </Show>
      <div aria-hidden="true" class={styles.shapes}>
        {props.children}
      </div>
    </div>
  )
}

export const SkeletonLine = (props: { width?: string; size?: "sm" | "md" | "lg" }) => (
  <span class={cx(styles.shape, styles.line[props.size ?? "md"])} style={{ width: props.width }} />
)

export const SkeletonBlock = (props: { aspect?: string; height?: string; class?: string }) => (
  <span
    class={cx(styles.shape, styles.block, props.class)}
    style={{ "aspect-ratio": props.aspect, height: props.height }}
  />
)

export function SkeletonRows(props: { count?: number; label?: string; class?: string }) {
  return (
    <SkeletonGroup label={props.label} class={cx(styles.rows, props.class)}>
      <For each={Array.from({ length: props.count ?? 5 }, (_, index) => index)}>
        {(index) => (
          <span class={styles.row}>
            <SkeletonLine width={`${[62, 48, 74, 55, 68][index % 5]}%`} />
            <SkeletonLine width={`${[28, 36, 22, 31, 25][index % 5]}%`} size="sm" />
          </span>
        )}
      </For>
    </SkeletonGroup>
  )
}

export function SkeletonCards(props: {
  count?: number
  aspect?: string
  label?: string
  class?: string
}) {
  return (
    <SkeletonGroup label={props.label} class={cx(styles.cards, props.class)}>
      <For each={Array.from({ length: props.count ?? 8 }, (_, index) => index)}>
        {(index) => (
          <span class={styles.card}>
            <SkeletonBlock aspect={props.aspect ?? "4 / 3"} />
            <SkeletonLine width={`${[78, 64, 86, 70][index % 4]}%`} size="lg" />
            <SkeletonLine width="40%" size="sm" />
          </span>
        )}
      </For>
    </SkeletonGroup>
  )
}

export function SkeletonPage(props: { label?: string; class?: string }) {
  return (
    <SkeletonGroup label={props.label} class={cx(styles.page, props.class)}>
      <SkeletonLine width="38%" size="lg" />
      <span class={styles.rows}>
        <SkeletonLine width="92%" />
        <SkeletonLine width="84%" />
        <SkeletonLine width="61%" />
      </span>
    </SkeletonGroup>
  )
}

const styles = {
  label: css({ srOnly: true }),
  slow: css({ color: "fg.muted", fontSize: "sm", marginBlockEnd: "3" }),
  shapes: css({ display: "contents" }),
  shape: css({
    animation: "skeleton-pulse 1.6s ease-in-out infinite",
    background: "bg.muted",
    borderRadius: "l1",
    display: "block",
    _motionReduce: { animation: "none" },
  }),
  line: {
    sm: css({ height: "0.75rem" }),
    md: css({ height: "0.95rem" }),
    lg: css({ height: "1.25rem" }),
  },
  block: css({ borderRadius: "l2", width: "100%" }),
  rows: css({ display: "grid", gap: "5" }),
  row: css({ display: "grid", gap: "2" }),
  cards: css({
    display: "grid",
    gap: "6",
    gridTemplateColumns: "repeat(auto-fill, minmax(min(100%, 12rem), 1fr))",
  }),
  card: css({ display: "grid", gap: "2.5" }),
  page: css({ display: "grid", gap: "6", maxWidth: "46rem", paddingBlock: "8" }),
}
