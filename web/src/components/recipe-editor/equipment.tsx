import { Plus, X } from "lucide-solid"
import { Index, Show } from "solid-js"
import { reconcile, type SetStoreFunction } from "solid-js/store"
import { css } from "styled-system/css"

import type { Recipe } from "@/api/client"
import { QuantityField } from "@/components/quantity-field"
import { Button } from "@/components/ui/button"
import { FieldGroup, FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { removeAt } from "@/lib/lists"
import { formatNumber } from "@/lib/locale-format"
import type { MeasurementStyle } from "@/lib/unit-choices"

import { m } from "../../lib/messages.ts"

const styles = {
  row: css({
    alignItems: "end",
    display: "grid",
    gap: "2",
    gridTemplateColumns: {
      base: "minmax(0, 1fr) auto",
      sm: "minmax(16rem, 20rem) minmax(0, 1fr) auto",
    },
  }),
  quantity: css({
    minWidth: "0",
    gridColumn: { base: "1 / -1", sm: "1" },
    gridRow: { base: "2", sm: "1" },
    "& > fieldset": { minWidth: "0" },
  }),
  name: css({ minWidth: "0", gridColumn: { base: "1", sm: "2" }, gridRow: "1" }),
  remove: css({ gridColumn: { base: "2", sm: "3" }, gridRow: "1", alignSelf: "end" }),
  note: css({ fieldSizing: "content", gridColumn: "1 / -1", minHeight: "2.5rem" }),
  list: css({ display: "grid", gap: "4" }),
  // Sits beside the other optional lists: only a button until it has entries.
  compact: css({ flex: "0 0 auto" }),
  full: css({ flex: "1 1 100%", minWidth: "0" }),
  hint: css({ color: "fg.muted", fontSize: "xs", margin: "0" }),
}

export function RecipeEquipmentEditor(props: {
  draft: Recipe
  style: MeasurementStyle
  setDraft: SetStoreFunction<Recipe>
}) {
  const values = () => props.draft.equipment ?? []
  const add = () => (
    <Button
      size="sm"
      variant="outline"
      onClick={() => props.setDraft("equipment", [...values(), { name: "" }])}
    >
      <Plus aria-hidden="true" size={14} />
      {m.equipment_add_equipment()}
    </Button>
  )
  return (
    <Show when={values().length > 0} fallback={<div class={styles.compact}>{add()}</div>}>
      <div class={styles.full}>
        <FieldGroup label={m.equipment_equipment()}>
          <div class={styles.list}>
            <Show when={values().length > 0}>
              <p class={styles.hint}>
                {m.equipment_equipment_is_what_this_recipe_expects_not_a_record_of_w()}
              </p>
              {/* Index preserves focus, caret, and composition while a row is edited. */}
              <Index each={values()}>
                {(entry, index) => {
                  return (
                    <div class={styles.row}>
                      <div class={styles.quantity}>
                        <FieldGroup label={m.equipment_quantity()}>
                          <QuantityField
                            value={entry().quantity}
                            label={m.editor_equipment_position_quantity({
                              position: formatNumber(index + 1),
                            })}
                            style={props.style}
                            compact
                            onChange={(quantity) =>
                              props.setDraft("equipment", index, "quantity", reconcile(quantity))
                            }
                          />
                        </FieldGroup>
                      </div>
                      <div class={styles.name}>
                        <FormField label={m.equipment_name()} required>
                          <Input
                            size="sm"
                            required
                            maxLength={1000}
                            aria-label={m.editor_equipment_position_name({
                              position: formatNumber(index + 1),
                            })}
                            placeholder={m.equipment_baking_sheet()}
                            value={entry().name}
                            onInput={(event) =>
                              props.setDraft("equipment", index, "name", event.currentTarget.value)
                            }
                          />
                        </FormField>
                      </div>
                      <Textarea
                        class={styles.note}
                        rows={1}
                        maxLength={1_000_000}
                        aria-label={m.editor_equipment_position_note({
                          position: formatNumber(index + 1),
                        })}
                        placeholder={m.equipment_note_optional()}
                        value={entry().note ?? ""}
                        onInput={(event) =>
                          props.setDraft("equipment", index, "note", event.currentTarget.value)
                        }
                      />
                      <IconButton
                        class={styles.remove}
                        aria-label={m.equipment_remove_equipment({
                          value1: formatNumber(index + 1),
                        })}
                        size="xs"
                        variant="plain"
                        onClick={() => props.setDraft("equipment", removeAt(values(), index))}
                      >
                        <X aria-hidden="true" size={15} />
                      </IconButton>
                    </div>
                  )
                }}
              </Index>
            </Show>
            <div>{add()}</div>
          </div>
        </FieldGroup>
      </div>
    </Show>
  )
}
