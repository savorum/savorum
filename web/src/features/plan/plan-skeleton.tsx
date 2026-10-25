import { For } from "solid-js"
import { css } from "styled-system/css"

import { SkeletonBlock, SkeletonGroup, SkeletonLine } from "@/components/skeleton"

const styles = {
  columns: css({ display: "grid", gap: "2", gridTemplateColumns: "repeat(7, minmax(0, 1fr))" }),
  column: css({ display: "grid", alignContent: "start", gap: "3", paddingInline: "3" }),
  rows: css({ display: "grid", gap: "6" }),
  row: css({ display: "grid", gap: "3", paddingInline: "3" }),
  meal: css({ alignItems: "center", display: "flex", gap: "3" }),
  thumb: css({ flexShrink: "0", width: "16" }),
  lines: css({ display: "grid", flex: "1", gap: "2" }),
  month: css({ display: "grid", gap: "1", gridTemplateColumns: "repeat(7, minmax(0, 1fr))" }),
}

const range = (count: number) => Array.from({ length: count }, (_, index) => index)

export function PlanSkeleton(props: { layout: "columns" | "rows" | "month" }) {
  return (
    <SkeletonGroup>
      {props.layout === "columns" ? (
        <span class={styles.columns}>
          <For each={range(7)}>
            {(index) => (
              <span class={styles.column}>
                <SkeletonLine width="70%" />
                <SkeletonBlock aspect="4 / 3" />
                <SkeletonLine width={`${[80, 60, 72][index % 3]}%`} size="sm" />
              </span>
            )}
          </For>
        </span>
      ) : props.layout === "month" ? (
        <span class={styles.month}>
          <For each={range(35)}>{() => <SkeletonBlock height="3.5rem" />}</For>
        </span>
      ) : (
        <span class={styles.rows}>
          <For each={range(4)}>
            {(index) => (
              <span class={styles.row}>
                <SkeletonLine width="40%" />
                <span class={styles.meal}>
                  <span class={styles.thumb}>
                    <SkeletonBlock aspect="1" />
                  </span>
                  <span class={styles.lines}>
                    <SkeletonLine width={`${[64, 52, 70, 58][index]}%`} />
                    <SkeletonLine width="30%" size="sm" />
                  </span>
                </span>
              </span>
            )}
          </For>
        </span>
      )}
    </SkeletonGroup>
  )
}
