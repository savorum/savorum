import { Plus, X } from "lucide-solid"
import { Index, Show, type Accessor, type JSX } from "solid-js"
import { css } from "styled-system/css"

import type { RecipeAuthor, Variant } from "@/api/client"
import { Button } from "@/components/ui/button"
import { FieldGroup } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { removeAt, replaceAt } from "@/lib/lists"
import { formatNumber } from "@/lib/locale-format"

import { m } from "../../lib/messages.ts"

const listRow = css({ alignItems: "center", display: "flex", gap: "2" })
const noteRow = css({ alignItems: "start", display: "flex", gap: "2" })
const list = css({ display: "grid", gap: "2" })
// One line to start, growing with the text where the browser supports it.
const growing = css({ fieldSizing: "content", minHeight: "2.5rem" })
const variantRow = css({
  alignItems: "start",
  display: "grid",
  gap: "2",
  gridTemplateColumns: { base: "minmax(0, 1fr) auto", sm: "11rem minmax(0, 1fr) auto" },
})
const variantName = css({ gridColumn: { base: "1 / -1", sm: "auto" } })
// An empty list is only its add button, so several sit on one line; a list
// with entries takes the full width.
const compact = css({ flex: "0 0 auto" })
const full = css({ flex: "1 1 100%", minWidth: "0" })

interface EditableRow<T> {
  entry: Accessor<T>
  /** The row's position as the reader's number, for accessible names. */
  position: string
  replace: (value: T) => void
  /** The control that drops this row, placed by the row itself. */
  remove: JSX.Element
}

/**
 * A list of authored values that the editor adds to, changes and shortens.
 * The group carries the heading and each control its own label, rather than
 * one Field.Root whose single control id every row would share.
 */
function EditableList<T>(props: {
  label: string
  hint?: string
  values: T[]
  /** A new entry, in whatever state an unfilled one is. */
  blank: () => T
  addLabel: string
  removeLabel: (position: string) => string
  onChange: (values: T[]) => void
  /** Shows only the add button until there is an entry. */
  compactWhenEmpty?: boolean
  children: (row: EditableRow<T>) => JSX.Element
}) {
  const add = () => (
    <Button
      size="sm"
      variant="outline"
      onClick={() => props.onChange([...props.values, props.blank()])}
    >
      <Plus aria-hidden="true" size={14} />
      {props.addLabel}
    </Button>
  )
  return (
    <Show
      when={!props.compactWhenEmpty || props.values.length > 0}
      fallback={<div class={compact}>{add()}</div>}
    >
      <div class={props.compactWhenEmpty ? full : undefined}>
        <FieldGroup label={props.label} hint={props.hint}>
          <div class={list}>
            {/* Index preserves the active input while its controlled value changes. */}
            <Index each={props.values}>
              {(entry, index) => {
                const position = formatNumber(index + 1)
                return props.children({
                  entry,
                  position,
                  replace: (value) => props.onChange(replaceAt(props.values, index, value)),
                  remove: (
                    <IconButton
                      aria-label={props.removeLabel(position)}
                      size="xs"
                      variant="plain"
                      onClick={() => props.onChange(removeAt(props.values, index))}
                    >
                      <X aria-hidden="true" size={15} />
                    </IconButton>
                  ),
                })
              }}
            </Index>
            <div>{add()}</div>
          </div>
        </FieldGroup>
      </div>
    </Show>
  )
}

/** A list of single-line values. */
export const TextList = (props: {
  label: string
  values: string[]
  placeholder?: string
  addLabel?: string
  /** Longer remarks: a text area that grows, and only its add button while empty. */
  multiline?: boolean
  onChange: (values: string[]) => void
}) => (
  <EditableList
    label={props.label}
    values={props.values}
    blank={() => ""}
    addLabel={props.addLabel ?? m.list_fields_add()}
    removeLabel={(position) => m.editor_remove_label_position({ label: props.label, position })}
    compactWhenEmpty={props.multiline}
    onChange={props.onChange}
  >
    {(row) => (
      <div class={props.multiline ? noteRow : listRow}>
        {props.multiline ? (
          <Textarea
            class={growing}
            aria-label={m.editor_label_position({ label: props.label, position: row.position })}
            size="sm"
            value={row.entry()}
            placeholder={props.placeholder}
            onInput={(event) => row.replace(event.currentTarget.value)}
          />
        ) : (
          <Input
            aria-label={m.editor_label_position({ label: props.label, position: row.position })}
            size="sm"
            value={row.entry()}
            placeholder={props.placeholder}
            onInput={(event) => row.replace(event.currentTarget.value)}
          />
        )}
        {row.remove}
      </div>
    )}
  </EditableList>
)

/** Authored variations of this recipe: preserved text and an optional name. */
export const VariantList = (props: {
  values: Variant[]
  onChange: (values: Variant[]) => void
}) => (
  <EditableList
    label={m.list_fields_variations()}
    values={props.values}
    blank={(): Variant => ({ text: "" })}
    addLabel={m.list_fields_add_variation()}
    removeLabel={(position) => m.list_fields_remove_variation({ value1: position })}
    compactWhenEmpty
    onChange={props.onChange}
  >
    {(row) => (
      <div class={variantRow}>
        <Input
          class={variantName}
          aria-label={m.editor_variation_position_name({ position: row.position })}
          size="sm"
          value={row.entry().title ?? ""}
          placeholder={m.list_fields_name_optional()}
          onInput={(event) => row.replace({ ...row.entry(), title: event.currentTarget.value })}
        />
        <Textarea
          class={growing}
          aria-label={m.editor_variation_position({ position: row.position })}
          size="sm"
          value={row.entry().text ?? ""}
          placeholder={m.list_fields_for_a_richer_version_add_100_ml_cream_with_the_stock()}
          onInput={(event) => row.replace({ ...row.entry(), text: event.currentTarget.value })}
        />
        {row.remove}
      </div>
    )}
  </EditableList>
)

export const AuthorList = (props: {
  values: RecipeAuthor[]
  onChange: (values: RecipeAuthor[]) => void
}) => (
  <EditableList
    label={m.list_fields_written_by()}
    values={props.values}
    blank={(): RecipeAuthor => ({ name: "" })}
    addLabel={m.list_fields_add_author()}
    removeLabel={(position) => m.list_fields_remove_author({ value1: position })}
    onChange={props.onChange}
  >
    {(row) => (
      <div class={listRow}>
        <Input
          aria-label={m.editor_author_position({ position: row.position })}
          size="sm"
          value={row.entry().name}
          placeholder={m.list_fields_author()}
          onInput={(event) => row.replace({ ...row.entry(), name: event.currentTarget.value })}
        />
        {row.remove}
      </div>
    )}
  </EditableList>
)
