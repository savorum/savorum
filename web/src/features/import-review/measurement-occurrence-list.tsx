import { For } from "solid-js"
import { css } from "styled-system/css"

import type { MeasurementResolutionOccurrence } from "@/api/import-previews"
import { Button } from "@/components/ui/button"

import { m } from "../../lib/messages.ts"

export function MeasurementOccurrenceList(props: {
  occurrences: readonly MeasurementResolutionOccurrence[]
  label?: string
  isIncluded: (id: string) => boolean
  onIncludedChange: (id: string, included: boolean) => void
  onExcludeRecipe: (id: string) => void
}) {
  return (
    <ul class={styles.resolutionMatches} aria-label={props.label}>
      <For each={props.occurrences}>
        {(match) => (
          <li class={styles.resolutionMatch}>
            <input
              type="checkbox"
              aria-label={m.measurement_occurrence_list_include_from({
                value1: match.source_text,
                value2: match.recipe_title,
              })}
              checked={props.isIncluded(match.id)}
              onChange={(event) => props.onIncludedChange(match.id, event.currentTarget.checked)}
            />
            <div>
              <div class={styles.resolutionMatchTitle}>{match.recipe_title}</div>
              <div class={styles.resolutionMatchText}>{match.source_text}</div>
              <Button
                size="xs"
                variant="plain"
                onClick={() => props.onExcludeRecipe(match.item_id)}
              >
                {m.measurement_occurrence_list_exclude_this_recipe()}
              </Button>
            </div>
          </li>
        )}
      </For>
    </ul>
  )
}
const styles = {
  resolutionMatches: css({
    display: "grid",
    gap: "2",
    listStyle: "none",
    marginBlock: "4",
    maxHeight: "22rem",
    overflowY: "auto",
    padding: "0",
  }),
  resolutionMatch: css({
    alignItems: "start",
    borderBottomColor: "border.default",
    borderBottomWidth: "1px",
    display: "grid",
    gap: "2",
    gridTemplateColumns: "auto 1fr",
    paddingBlock: "2",
  }),
  resolutionMatchTitle: css({ fontSize: "sm", fontWeight: "medium" }),
  resolutionMatchText: css({ color: "fg.muted", fontSize: "sm" }),
}
