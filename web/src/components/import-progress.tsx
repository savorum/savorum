import { Show } from "solid-js"
import { css } from "styled-system/css"

import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { type ImportPhase, describeImportPhase } from "@/lib/import-progress"

import { m } from "../lib/messages.ts"

const styles = {
  panel: css({
    alignItems: "center",
    background: "bg.subtle",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "flex",
    gap: "4",
    justifyContent: "space-between",
    marginTop: "4",
    paddingBlock: "3",
    paddingInline: "4",
  }),
  actions: css({ flexShrink: "0" }),
}

export interface ImportProgressProps {
  phase: ImportPhase
  /** Offered only where stopping is actually possible. */
  onCancel?: () => void
  cancelLabel?: string
}

/**
 * One presentation for every phase of an import: which phase, and how far
 * along it is, or an indeterminate bar when that is unknown.
 */
export function ImportProgress(props: ImportProgressProps) {
  const report = () => describeImportPhase(props.phase)
  return (
    <Show when={report()}>
      {(current) => (
        <div class={styles.panel} role="status" aria-live="polite">
          <Progress
            label={current().label}
            // Undefined means unmeasurable, which Ark renders as indeterminate.
            value={current().fraction === undefined ? null : Math.round(current().fraction! * 100)}
            valueText={current().detail}
            running
          />
          <Show when={props.onCancel}>
            {(cancel) => (
              <div class={styles.actions}>
                <Button size="sm" variant="plain" onClick={() => cancel()()}>
                  {props.cancelLabel ?? m.common_cancel()}
                </Button>
              </div>
            )}
          </Show>
        </div>
      )}
    </Show>
  )
}
