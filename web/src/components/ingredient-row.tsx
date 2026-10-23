import { GripVertical } from "lucide-solid"
import { Show, createEffect, createSignal, untrack } from "solid-js"
import type { JSX } from "solid-js"
import { css } from "styled-system/css"

import { type LineReading, readIngredientLine } from "@/api/ingredient-lines"
import type { components } from "@/api/schema"
import { useIngredientReading } from "@/components/ingredient-reading"
import { RowActions } from "@/components/recipe-editor/row-actions"
import { Input } from "@/components/ui/input"
import { trackReading } from "@/lib/line-readings"
import { formatNumber } from "@/lib/locale-format"
import { asWritten, showQuantity, writtenLine } from "@/lib/recipe-format"
import type { MeasurementStyle } from "@/lib/unit-choices"

import { m } from "../lib/messages.ts"
import { IngredientDetails } from "./recipe-editor/ingredient-details"

type Ingredient = components["schemas"]["Ingredient"]
export interface IngredientRowProps {
  onAddAlternative?: (ingredient: Ingredient) => void
  ingredient: Ingredient
  position: number
  style: MeasurementStyle
  first: boolean
  last: boolean
  onChange: (ingredient: Ingredient) => void
  onMove: (offset: number) => void
  onRemove: () => void
  removable: boolean
  /** Makes the row draggable by a pointer; the move menu remains for everyone else. */
  dragHandleProps?: JSX.HTMLAttributes<HTMLSpanElement>
}

/** The row with a line read into it. What the line does not say is kept. */
function withReading(current: Ingredient, read: Ingredient, line: string): Ingredient {
  const next: Ingredient = {
    ...current,
    name: read.name ?? "",
    quantity: read.quantity,
    note: read.note,
    source_text: line,
    normalization: read.normalization,
    package_size: read.package_size,
    alternatives: read.alternatives?.map((branch) => ({
      ...branch,
      id: `${current.id}-${branch.id}`,
    })),
    relation: read.relation,
  }
  if (read.optional) next.optional = true
  return next
}

/** The row when the line could not be read at all: what was typed is its name. */
function asWrittenOnly(current: Ingredient, line: string): Ingredient {
  return {
    ...current,
    name: line,
    quantity: undefined,
    note: undefined,
    source_text: line,
    normalization: undefined,
    package_size: undefined,
    alternatives: undefined,
    relation: undefined,
  }
}

/**
 * Reads a typed line into the row it makes: the server's reading, or, when the
 * server cannot be asked, the line as the row's name. Resolves to nothing when
 * the line has been typed over by the time it is read.
 */
async function readRow(
  line: string,
  reading: LineReading,
  current: () => Ingredient,
  typedOver: () => boolean,
): Promise<Ingredient | undefined> {
  let next: Ingredient
  try {
    const read = await readIngredientLine(line, reading)
    next = withReading(current(), read, line)
  } catch {
    next = asWrittenOnly(current(), line)
  }
  return typedOver() ? undefined : next
}

export const IngredientRow = (props: IngredientRowProps) => {
  const [advanced, setAdvanced] = createSignal(false)
  const reading = useIngredientReading()

  // The box keeps exactly what is typed until it loses focus, so a half-typed
  // amount is never rewritten mid-keystroke.
  const [typed, setTyped] = createSignal(untrack(() => writtenLine(props.ingredient)))
  // Named by what is in the field until the line has been read.
  const itemName = () =>
    props.ingredient.name ||
    typed().trim() ||
    m.editor_ingredient_position({ position: formatNumber(props.position) })
  let input: HTMLInputElement | undefined
  let requested: { line: string } | undefined
  createEffect(() => {
    const next = writtenLine(props.ingredient)
    if (!requested && (!input || document.activeElement !== input)) setTyped(next)
  })

  const commit = () => {
    const line = untrack(typed).trim()
    if (line === writtenLine(props.ingredient) || line === requested?.line) return
    if (line === "") {
      requested = undefined
      props.onChange({
        ...props.ingredient,
        name: "",
        quantity: undefined,
        note: undefined,
        source_text: undefined,
        normalization: undefined,
      })
      return
    }
    const request = { line }
    requested = request
    const onChange = untrack(() => props.onChange)
    const row = readRow(
      line,
      { ...reading() },
      // The answer is applied to the row as it is when it arrives, and dropped
      // if the field has been typed over since the line was sent.
      () => untrack(() => props.ingredient),
      () => untrack(typed).trim() !== line,
    )
    void trackReading(row).then((next) => {
      if (requested !== request) return
      requested = undefined
      if (next) onChange(next)
    })
  }

  return (
    <>
      <div class={styles.row} data-editor-kind="ingredient" data-editor-id={props.ingredient.id}>
        <span
          class={styles.grip}
          aria-hidden="true"
          data-drag-handle={props.dragHandleProps ? "" : undefined}
          title={props.dragHandleProps ? m.instructions_drag_to_reorder() : undefined}
          {...props.dragHandleProps}
        >
          <Show when={props.dragHandleProps}>
            <GripVertical aria-hidden="true" size={15} />
          </Show>
        </span>
        <div class={styles.line}>
          <Input
            size="sm"
            ref={(node) => {
              input = node
            }}
            required
            aria-label={m.ingredient_row_ingredient()}
            data-focus="primary"
            data-add-row-on-enter=""
            placeholder={
              props.position === 1 ? m.ingredient_row_line_example() : m.ingredient_row_ingredient()
            }
            value={typed()}
            onInput={(event) => setTyped(event.currentTarget.value)}
            onBlur={commit}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.isComposing) commit()
            }}
          />
          <Show when={props.ingredient.optional}>
            <span class={styles.flag}>{m.editor_optional()}</span>
          </Show>
        </div>
        <div class={styles.actions}>
          <RowActions
            label={m.ingredient_row_more_options_for({ value1: itemName() })}
            actions={[
              {
                label: m.ingredient_row_details(),
                context: itemName(),
                run: () => requestAnimationFrame(() => setAdvanced(true)),
              },
              {
                label: m.ingredient_row_move_up(),
                context: itemName(),
                disabled: props.first,
                run: () => props.onMove(-1),
              },
              {
                label: m.ingredient_row_move_down(),
                context: itemName(),
                disabled: props.last,
                run: () => props.onMove(1),
              },
              {
                label: m.ingredient_row_remove(),
                context: itemName(),
                disabled: !props.removable,
                separated: true,
                run: props.onRemove,
              },
            ]}
          />
        </div>
        {/* A line naming a measure that could not be identified is kept as written. */}
        <Show when={props.ingredient.normalization?.method === "unresolved"}>
          <p class={styles.summary}>{m.body_amount_as_written()}</p>
        </Show>
        <Show when={props.ingredient.package_size}>
          <p class={styles.summary}>
            {m.editor_package_size({
              value: showQuantity(props.ingredient.package_size, asWritten)?.text ?? "",
            })}
          </p>
        </Show>
      </div>

      <Show when={advanced()}>
        <IngredientDetails
          ingredient={props.ingredient}
          style={props.style}
          onChange={props.onChange}
          onAddAlternative={props.onAddAlternative}
          onClose={() => setAdvanced(false)}
        />
      </Show>
    </>
  )
}

const styles = {
  row: css({
    alignItems: "center",
    display: "grid",
    gap: "2",
    gridTemplateColumns: {
      base: "minmax(0, 1fr) 2.25rem",
      sm: "1rem minmax(0, 1fr) 2.25rem",
    },
    paddingBlock: "1",
  }),
  // A pointer's handle, in a column of its own.
  grip: css({
    color: "fg.subtle",
    cursor: "grab",
    display: { base: "none", sm: "flex" },
    _active: { cursor: "grabbing" },
    _hover: { color: "fg.muted" },
  }),
  line: css({ alignItems: "center", display: "flex", gap: "2", minWidth: "0" }),
  flag: css({
    borderColor: "border.subtle",
    borderRadius: "l1",
    borderWidth: "1px",
    color: "fg.muted",
    flexShrink: "0",
    fontSize: "xs",
    lineHeight: "1.4",
    paddingBlock: "0.5",
    paddingInline: "1.5",
    whiteSpace: "nowrap",
  }),
  actions: css({ alignItems: "center", display: "flex" }),
  summary: css({
    color: "fg.muted",
    fontSize: "xs",
    gridColumn: { base: "1 / -1", sm: "2 / -1" },
    margin: "0",
  }),
}
