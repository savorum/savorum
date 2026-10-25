import {
  CircleCheck,
  CircleDashed,
  CircleMinus,
  CircleSlash,
  CircleX,
  LibraryBig,
  TriangleAlert,
} from "lucide-solid"
import { For, Match, Show, Switch } from "solid-js"
import { Dynamic } from "solid-js/web"
import { css, cx } from "styled-system/css"

import { Spinner } from "@/components/ui/spinner"
import type { ImportJobOutcome, ImportJobSegment } from "@/lib/import-job-presentation"

/** Hidden from assistive technology: the words beside it say the same. */
export function ImportOutcomeMark(props: { outcome: ImportJobOutcome; class?: string }) {
  return (
    <span class={cx(styles.mark, props.class)} data-outcome={props.outcome} aria-hidden="true">
      <Switch>
        <Match when={props.outcome === "unchanged"}>
          <LibraryBig size={18} />
        </Match>
        <Match when={props.outcome === "running"}>
          <Spinner size="sm" />
        </Match>
        <Match when={props.outcome === "complete"}>
          <CircleCheck size={18} />
        </Match>
        <Match when={props.outcome === "partial"}>
          <TriangleAlert size={18} />
        </Match>
        <Match when={props.outcome === "failed"}>
          <CircleX size={18} />
        </Match>
        <Match when={props.outcome === "stopped"}>
          <CircleSlash size={18} />
        </Match>
      </Switch>
    </span>
  )
}

export const outcomeText = css({
  color: "fg.muted",
  '&[data-outcome="partial"]': { color: "amber.default", fontWeight: "medium" },
  '&[data-outcome="failed"]': { color: "danger.default", fontWeight: "medium" },
})

const segmentIcons = {
  imported: CircleCheck,
  skipped: LibraryBig,
  failed: CircleX,
  cancelled: CircleMinus,
  remaining: CircleDashed,
}

export function ImportOutcomeShares(props: { segments: ImportJobSegment[]; bar: boolean }) {
  return (
    <div class={styles.shares}>
      <Show when={props.bar}>
        <div class={styles.bar} aria-hidden="true">
          <For each={props.segments}>
            {(segment) => (
              <span
                class={styles.segment}
                data-kind={segment.kind}
                style={{ "flex-grow": segment.count, "flex-basis": "0" }}
              />
            )}
          </For>
        </div>
      </Show>
      <ul class={styles.legend}>
        <For each={props.segments}>
          {(segment) => (
            <li class={styles.share} data-kind={segment.kind}>
              <Dynamic component={segmentIcons[segment.kind]} aria-hidden="true" size={15} />
              {segment.label}
            </li>
          )}
        </For>
      </ul>
    </div>
  )
}

const styles = {
  shares: css({ display: "grid", gap: "3" }),
  bar: css({
    background: "bg.muted",
    borderRadius: "l1",
    display: "flex",
    gap: "0.5",
    height: "2.5",
    overflow: "hidden",
  }),
  segment: css({
    background: "accent.default",
    minWidth: "1",
    '&[data-kind="skipped"]': { background: "border.emphasized" },
    '&[data-kind="failed"]': { background: "danger.default" },
    '&[data-kind="cancelled"]': { background: "amber.default" },
    '&[data-kind="remaining"]': { background: "transparent" },
  }),
  legend: css({
    color: "fg.muted",
    columnGap: "4",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    listStyle: "none",
    margin: "0",
    padding: "0",
    rowGap: "1",
  }),
  share: css({
    alignItems: "center",
    display: "inline-flex",
    gap: "1.5",
    "& svg": { flexShrink: "0" },
    '&[data-kind="imported"] svg': { color: "accent.default" },
    '&[data-kind="failed"]': { color: "danger.default", fontWeight: "medium" },
    '&[data-kind="cancelled"] svg': { color: "amber.default" },
  }),
  mark: css({
    alignItems: "center",
    background: "bg.muted",
    borderRadius: "full",
    color: "fg.muted",
    display: "inline-flex",
    flexShrink: "0",
    height: "9",
    justifyContent: "center",
    width: "9",
    '&[data-outcome="running"]': { background: "accent.subtle.bg", color: "accent.default" },
    '&[data-outcome="partial"]': { background: "amber.subtle.bg", color: "amber.default" },
    '&[data-outcome="failed"]': { background: "danger.muted", color: "danger.default" },
  }),
}
