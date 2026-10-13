import { GripVertical } from "lucide-solid"
import { Index, Show } from "solid-js"
import { css } from "styled-system/css"

import { IngredientRow, type IngredientRowProps } from "@/components/ingredient-row"
import { WhenSet } from "@/components/recipe-editor/when-set"
import { Button } from "@/components/ui/button"
import { ToggleField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { removeAt } from "@/lib/lists"
import { formatNumber } from "@/lib/locale-format"
import { emptyIngredient, moveItem, newID } from "@/lib/recipe-draft"

import { m } from "../lib/messages.ts"

const styles = {
  choice: css({
    borderInlineStartWidth: "2px",
    borderColor: "border.subtle",
    padding: "3",
    marginBlock: "2",
    display: "grid",
    gap: "2",
  }),
  actions: css({ display: "flex", gap: "2", flexWrap: "wrap" }),
  grip: css({
    color: "fg.subtle",
    cursor: "grab",
    display: { base: "none", sm: "inline-flex" },
    marginInlineEnd: "1",
    verticalAlign: "middle",
    _active: { cursor: "grabbing" },
    _hover: { color: "fg.muted" },
  }),
}

export function IngredientChoice(
  props: IngredientRowProps & {
    onAnnounce?: (message: string) => void
    onFocus?: (id: string) => void
  },
) {
  const branches = () => props.ingredient.alternatives ?? []
  const changeBranches = (alternatives: typeof props.ingredient.alternatives) =>
    props.onChange({ ...props.ingredient, alternatives })

  return (
    <Show
      when={branches().length > 0}
      fallback={
        <IngredientRow
          {...props}
          onAddAlternative={(current) =>
            props.onChange({
              // Existing step references and scoped metadata still name the original
              // ingredient. Give the surrounding requirement a new identity.
              id: newID("choice"),
              relation: "or",
              alternatives: [current, emptyIngredient()],
            })
          }
        />
      }
    >
      <fieldset
        class={styles.choice}
        data-editor-kind="ingredient"
        data-editor-id={props.ingredient.id}
      >
        <legend>
          <Show when={props.dragHandleProps}>
            <span
              class={styles.grip}
              aria-hidden="true"
              data-drag-handle
              title={m.instructions_drag_to_reorder()}
              {...props.dragHandleProps}
            >
              <GripVertical aria-hidden="true" size={15} />
            </span>
          </Show>
          {m.ingredient_choice_ingredient_choice({ value1: formatNumber(props.position) })}
        </legend>
        <Select
          aria-label={m.ingredient_choice_choice_wording()}
          data-focus="primary"
          value={props.ingredient.relation ?? "or"}
          onValueChange={(value) =>
            props.onChange({
              ...props.ingredient,
              relation: value as "or" | "and_or" | "preferred",
            })
          }
          items={[
            { value: "or", label: m.ingredient_choice_either_ingredient() },
            { value: "and_or", label: m.ingredient_choice_either_or_both() },
            {
              value: "preferred",
              label: m.ingredient_choice_first_preferred_others_may_substitute(),
            },
          ]}
        />
        {/* Index keeps each active field mounted while branch values change. */}
        <Index each={branches()}>
          {(branch, index) => (
            <IngredientRow
              ingredient={branch()}
              position={index + 1}
              style={props.style}
              first={index === 0}
              last={index === branches().length - 1}
              removable={branches().length > 2}
              onChange={(next) =>
                changeBranches(branches().map((item, at) => (at === index ? next : item)))
              }
              onMove={(offset) => {
                const id = branch().id
                changeBranches(moveItem(branches(), index, index + offset))
                props.onAnnounce?.(
                  m.editor_alternative_moved({ position: formatNumber(index + offset + 1) }),
                )
                props.onFocus?.(id)
              }}
              onRemove={() => {
                const survivor = branches()[index + 1] ?? branches()[index - 1]
                changeBranches(removeAt(branches(), index))
                props.onAnnounce?.(
                  m.editor_alternative_removed({ position: formatNumber(index + 1) }),
                )
                if (survivor) props.onFocus?.(survivor.id)
              }}
            />
          )}
        </Index>
        <Input
          aria-label={m.ingredient_choice_shared_choice_note()}
          placeholder={m.ingredient_choice_note_applying_to_the_whole_choice()}
          value={props.ingredient.note ?? ""}
          onInput={(event) =>
            props.onChange({ ...props.ingredient, note: event.currentTarget.value })
          }
        />
        <WhenSet value={props.ingredient.source_text}>
          <Input
            aria-label={m.ingredient_choice_authored_choice_wording()}
            placeholder={m.ingredient_choice_original_wording_or_substitution_condition()}
            value={props.ingredient.source_text ?? ""}
            onInput={(event) =>
              props.onChange({ ...props.ingredient, source_text: event.currentTarget.value })
            }
          />
        </WhenSet>
        <ToggleField
          label={m.ingredient_choice_the_entire_choice_is_optional()}
          checked={props.ingredient.optional ?? false}
          onChange={(checked) => props.onChange({ ...props.ingredient, optional: checked })}
        />
        <div class={styles.actions}>
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              const alternative = emptyIngredient()
              changeBranches([...branches(), alternative])
              props.onAnnounce?.(m.editor_alternative())
              props.onFocus?.(alternative.id)
            }}
          >
            {m.ingredient_choice_add_alternative()}
          </Button>
          <Button size="sm" variant="plain" disabled={props.first} onClick={() => props.onMove(-1)}>
            {m.ingredient_choice_move_choice_up()}
          </Button>
          <Button size="sm" variant="plain" disabled={props.last} onClick={() => props.onMove(1)}>
            {m.ingredient_choice_move_choice_down()}
          </Button>
          <Button size="sm" variant="plain" disabled={!props.removable} onClick={props.onRemove}>
            {m.ingredient_choice_remove_choice()}
          </Button>
        </div>
      </fieldset>
    </Show>
  )
}
