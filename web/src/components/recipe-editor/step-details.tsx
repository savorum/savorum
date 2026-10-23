import { ImagePlus, Plus, X } from "lucide-solid"
import { For, Index, Show, createSignal, untrack } from "solid-js"
import { unwrap } from "solid-js/store"
import { css } from "styled-system/css"

import { type Recipe, assetURL, fallbackToOriginalImage } from "@/api/client"
import type { components } from "@/api/schema"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { FieldGroup, FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { amountFieldValue, needsAdvancedAmount, parseAmountField } from "@/lib/amount-field"
import { removeAt } from "@/lib/lists"
import { formatNumber } from "@/lib/locale-format"
import { formatAmountInput, parseAmount } from "@/lib/recipe-draft"
import { hasTemperatureValue, temperatureWithFormat } from "@/lib/temperature-field"
import { type MeasurementStyle, unitOptions } from "@/lib/unit-choices"

import { m } from "../../lib/messages.ts"

type Step = components["schemas"]["Step"]
type Temperature = components["schemas"]["Temperature"]
type IngredientUse = components["schemas"]["IngredientUse"]
type Quantity = components["schemas"]["Quantity"]

const styles = {
  dialogFields: css({
    display: "grid",
    gap: "4",
    marginTop: "5",
    maxHeight: "60vh",
    overflowY: "auto",
  }),
  stepDialog: css({
    width: "min(64rem, calc(100vw - 2rem))!",
    maxWidth: "64rem!",
  }),
  wording: css({
    borderInlineStartColor: "border.default",
    borderInlineStartWidth: "2px",
    color: "fg.default",
    display: "block",
    fontFamily: "editorial",
    maxHeight: "8rem",
    overflowY: "auto",
    paddingInlineStart: "3",
    whiteSpace: "pre-wrap",
  }),
  wordingHeading: css({ display: "block", fontWeight: "semibold" }),
  fileInput: css({ display: "none" }),
  relationshipChoices: css({
    display: "grid",
    gap: "1",
    gridTemplateColumns: { base: "1fr", sm: "repeat(2, minmax(0, 1fr))" },
  }),
  ingredientUseChoice: css({
    alignItems: "center",
    display: "grid",
    gap: "2",
    gridTemplateColumns: {
      base: "auto minmax(0, 1fr)",
      sm: "auto minmax(0, 1fr) 5.5rem 8rem",
    },
  }),
  ingredientUseAmount: css({ gridColumn: { base: "2", sm: "auto" } }),
  temperatureRow: css({
    alignItems: "center",
    display: "grid",
    gap: "2",
    gridTemplateColumns: {
      base: "minmax(0, 1fr) minmax(0, 1fr)",
      sm: "minmax(0, 1fr) minmax(0, 1.5fr) minmax(0, 1fr) minmax(0, 1.25fr) auto",
    },
  }),
  temperaturePurpose: css({ display: "grid", gap: "1" }),
  stepPhotos: css({
    display: "grid",
    gap: "2",
    gridTemplateColumns: "repeat(auto-fill, minmax(7rem, 1fr))",
  }),
  stepPhoto: css({
    aspectRatio: "4 / 3",
    borderColor: "transparent",
    borderRadius: "l2",
    borderWidth: "2px",
    cursor: "pointer",
    objectFit: "cover",
    padding: "0",
    width: "100%",
    _focusVisible: {
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "2px",
    },
  }),
  stepPhotoChosen: css({ borderColor: "accent.default" }),
  thumbnail: css({
    aspectRatio: "4 / 3",
    borderRadius: "l2",
    objectFit: "cover",
    width: "100%",
  }),
}

const temperatureUnits = [
  {
    value: "temperature.celsius",
    get label() {
      return m.step_details_celsius()
    },
  },
  {
    value: "temperature.fahrenheit",
    get label() {
      return m.step_details_fahrenheit()
    },
  },
]

const temperaturePurposes = [
  {
    value: "oven",
    get label() {
      return m.step_details_oven()
    },
  },
  {
    value: "internal",
    get label() {
      return m.step_details_internal_food_temperature()
    },
  },
  {
    value: "oil",
    get label() {
      return m.step_details_oil_or_cooking_fat()
    },
  },
  {
    value: "water",
    get label() {
      return m.step_details_water_or_cooking_liquid()
    },
  },
  {
    value: "surface",
    get label() {
      return m.step_details_surface()
    },
  },
  {
    value: "other",
    get label() {
      return m.step_details_other()
    },
  },
]

/** Edits the less common relationships and media for one step. */
export function StepDetails(props: {
  step: Step
  /** Where the step sits in its own stage, counted the way the editor counts. */
  position: number
  /** The stage the step belongs to, when the method is written in more than one. */
  stageTitle?: string
  recipeID: string
  photos: NonNullable<Recipe["assets"]>
  ingredients: { id: string; label: string; name: string; choice?: boolean }[]
  style: MeasurementStyle
  onChange: (step: Step) => void
  onAttachPhotos: (files: FileList | null) => Promise<string[]>
  onClose: () => void
}) {
  // Store nodes must be unwrapped before cloning.
  const [draft, setDraft] = createSignal<Step>(untrack(() => structuredClone(unwrap(props.step))))
  const temperatures = () => draft().temperatures ?? []
  const chosen = () => draft().image_refs ?? []
  const ingredientUses = () => draft().ingredient_uses ?? []
  /** The reader's rule: a heading the prose already opens with is not shown twice. */
  const visibleHeading = () => {
    const heading = props.step.title?.trim()
    const prose = props.step.text?.trim().toLocaleLowerCase() ?? ""
    if (!heading) return undefined
    const lowered = heading.toLocaleLowerCase()
    return prose === lowered || prose.startsWith(lowered) ? undefined : heading
  }
  const [attachError, setAttachError] = createSignal<unknown>()
  const [attaching, setAttaching] = createSignal(false)
  let stepPhotoInput: HTMLInputElement | undefined

  const attachPhotos = async (files: FileList | null) => {
    setAttachError(undefined)
    setAttaching(true)
    try {
      const ids = await props.onAttachPhotos(files)
      setDraft({
        ...draft(),
        image_refs: [...new Set([...chosen(), ...ids])],
      })
    } catch (error) {
      setAttachError(error)
    } finally {
      setAttaching(false)
      if (stepPhotoInput) stepPhotoInput.value = ""
    }
  }

  const setTemperatures = (next: Temperature[]) =>
    setDraft({ ...draft(), temperatures: next.length > 0 ? next : undefined })

  const setTemperature = (index: number, patch: Partial<Temperature>) =>
    setTemperatures(
      temperatures().map((entry, position) =>
        position === index ? { ...entry, ...patch } : entry,
      ),
    )

  const setTemperaturePurpose = (index: number, kind: string) => {
    const current = temperatures()[index]?.purpose
    setTemperature(index, {
      purpose: kind
        ? {
            kind: kind as NonNullable<Temperature["purpose"]>["kind"],
            ...(current?.kind === kind && current.label ? { label: current.label } : {}),
          }
        : undefined,
    })
  }

  const togglePhoto = (assetID: string) =>
    setDraft({
      ...draft(),
      image_refs: chosen().includes(assetID)
        ? chosen().filter((reference) => reference !== assetID)
        : [...chosen(), assetID],
    })

  const ingredientUse = (ingredientID: string) =>
    ingredientUses().find((use) => use.ingredient_ref === ingredientID)

  const setIngredientUses = (uses: IngredientUse[]) =>
    setDraft({
      ...draft(),
      ingredient_uses: uses.length > 0 ? uses : undefined,
    })

  const toggleIngredient = (ingredientID: string) =>
    setIngredientUses(
      ingredientUse(ingredientID)
        ? ingredientUses().filter((use) => use.ingredient_ref !== ingredientID)
        : [...ingredientUses(), { ingredient_ref: ingredientID }],
    )

  const setIngredientQuantity = (ingredientID: string, quantity: Quantity | undefined) =>
    setIngredientUses(
      ingredientUses().map((use) =>
        use.ingredient_ref === ingredientID ? { ...use, quantity } : use,
      ),
    )

  const setIngredientUnit = (ingredientID: string, unit: string) => {
    const quantity = ingredientUse(ingredientID)?.quantity
    if (!quantity || quantity.kind !== "simple") return
    setIngredientQuantity(ingredientID, {
      ...quantity,
      unit: unit || undefined,
    })
  }

  return (
    <Dialog
      open
      contentClass={styles.stepDialog}
      onClose={() => props.onClose()}
      title={[
        m.common_step_number({ value1: formatNumber(props.position) }),
        props.stageTitle?.trim(),
      ]
        .filter(Boolean)
        .join(` ${m.common_dot_separator()} `)}
      description={
        <span class={styles.wording}>
          <Show when={visibleHeading()}>
            {(heading) => <strong class={styles.wordingHeading}>{heading()}</strong>}
          </Show>
          <Show
            when={props.step.text?.trim()}
            fallback={m.step_details_this_step_has_no_wording_yet()}
          >
            {(text) => <>{text()}</>}
          </Show>
        </span>
      }
      footer={
        <>
          <DialogClose>
            {(closeProps) => (
              <Button {...closeProps()} variant="outline">
                {m.common_cancel()}
              </Button>
            )}
          </DialogClose>
          <Button
            colorPalette="accent"
            onClick={() => {
              const stated = temperatures().filter(hasTemperatureValue)
              props.onChange({
                ...draft(),
                temperatures: stated.length > 0 ? stated : undefined,
              })
              props.onClose()
            }}
          >
            {m.dialog_done()}
          </Button>
        </>
      }
    >
      <div class={styles.dialogFields}>
        <FormField
          label={m.step_details_ingredients_used_in_this_step()}
          hint={
            props.ingredients.length > 0
              ? m.step_details_choose_the_ingredients_a_cook_should_have_alongside_thi()
              : m.step_details_add_ingredients_to_the_recipe_first_then_associate_them()
          }
        >
          <Show when={props.ingredients.length > 0}>
            <div class={styles.relationshipChoices}>
              {/* Index preserves amount inputs while their controlled values change. */}
              <Index each={props.ingredients}>
                {(ingredient) => (
                  <div class={styles.ingredientUseChoice}>
                    <input
                      type="checkbox"
                      checked={ingredientUse(ingredient().id) !== undefined}
                      aria-label={m.editor_use_label({ label: ingredient().label })}
                      onChange={() => toggleIngredient(ingredient().id)}
                    />
                    <span>{ingredient().label}</span>
                    <Show when={ingredientUse(ingredient().id)}>
                      {(use) => {
                        const [typedAmount, setTypedAmount] = createSignal(
                          untrack(() => amountFieldValue(use().quantity)),
                        )
                        const enteredQuantity = () =>
                          parseAmountField(typedAmount(), use().quantity?.unit)
                        const commitAmount = () => {
                          if (typedAmount() !== amountFieldValue(use().quantity))
                            setIngredientQuantity(ingredient().id, enteredQuantity())
                        }
                        return (
                          <>
                            <Input
                              class={styles.ingredientUseAmount}
                              size="sm"
                              aria-label={m.step_details_amount_of_used({
                                value1: ingredient().name,
                              })}
                              placeholder={m.step_details_amount_optional()}
                              disabled={ingredient().choice || needsAdvancedAmount(use().quantity)}
                              value={
                                needsAdvancedAmount(use().quantity)
                                  ? m.ingredient_multiple_amounts()
                                  : typedAmount()
                              }
                              onInput={(event) => setTypedAmount(event.currentTarget.value)}
                              onBlur={commitAmount}
                              onKeyDown={(event) => {
                                if (event.key === "Enter" && !event.isComposing) commitAmount()
                              }}
                            />
                            <Select
                              size="sm"
                              aria-label={m.step_details_measure_for_used({
                                value1: ingredient().name,
                              })}
                              disabled={ingredient().choice || enteredQuantity()?.kind !== "simple"}
                              value={
                                use().quantity?.kind === "simple"
                                  ? (use().quantity?.unit ?? "")
                                  : ""
                              }
                              onValueChange={(value) => setIngredientUnit(ingredient().id, value)}
                              items={unitOptions(
                                props.style,
                                use().quantity?.kind === "simple"
                                  ? use().quantity?.unit
                                  : undefined,
                                m.step_details_no_measure(),
                              )}
                            />
                          </>
                        )
                      }}
                    </Show>
                  </div>
                )}
              </Index>
            </div>
          </Show>
        </FormField>

        <FieldGroup label={m.step_details_temperature()}>
          <div class={css({ display: "grid", gap: "2" })}>
            {/* Index preserves the active field while a temperature is updated. */}
            <Index each={temperatures()}>
              {(temperature, index) => (
                <div class={styles.temperatureRow}>
                  <Select
                    size="sm"
                    aria-label={m.editor_temperature_position_format({
                      position: formatNumber(index + 1),
                    })}
                    value={temperature().text !== undefined ? "text" : "numeric"}
                    onValueChange={(value) => {
                      const initialTemperature = temperature()
                      const next = temperatureWithFormat(
                        initialTemperature,
                        value as "text" | "numeric",
                      )
                      setTemperatures(
                        temperatures().map((entry, at) => (at === index ? next : entry)),
                      )
                    }}
                    items={[
                      { value: "numeric", label: m.step_details_number_and_scale() },
                      { value: "text", label: m.step_details_authored_wording() },
                    ]}
                  />
                  <Show
                    when={temperature().text !== undefined}
                    fallback={
                      <>
                        <Input
                          size="sm"
                          aria-label={m.step_details_temperature()}
                          placeholder="200"
                          value={formatAmountInput(temperature().amount?.value)}
                          onChange={(event) =>
                            setTemperature(index, {
                              amount: {
                                ...temperature().amount,
                                value: parseAmount(event.currentTarget.value, true),
                              },
                            })
                          }
                        />
                        <Select
                          size="sm"
                          aria-label={m.step_details_temperature_scale()}
                          value={temperature().unit ?? ""}
                          onValueChange={(value) => setTemperature(index, { unit: value })}
                          items={temperatureUnits}
                        />
                      </>
                    }
                  >
                    <Input
                      size="sm"
                      aria-label={m.editor_temperature_position_wording({
                        position: formatNumber(index + 1),
                      })}
                      placeholder={m.step_details_moderate_oven()}
                      required
                      maxLength={1000}
                      value={temperature().text ?? ""}
                      onInput={(event) =>
                        setTemperature(index, {
                          text: event.currentTarget.value,
                        })
                      }
                    />
                    <span />
                  </Show>
                  <div class={styles.temperaturePurpose}>
                    <Select
                      size="sm"
                      aria-label={m.step_details_applies_to_temperature({
                        value1: formatNumber(index + 1),
                      })}
                      value={temperature().purpose?.kind ?? ""}
                      onValueChange={(value) => setTemperaturePurpose(index, value)}
                      items={[
                        { value: "", label: m.step_details_applies_to() },
                        ...temperaturePurposes,
                      ]}
                    />
                    <Show when={temperature().purpose?.kind === "other"}>
                      <Input
                        size="sm"
                        aria-label={m.step_details_other_purpose_for_temperature({
                          value1: formatNumber(index + 1),
                        })}
                        placeholder={m.step_details_e_g_sugar_syrup()}
                        value={temperature().purpose?.label ?? ""}
                        onInput={(event) =>
                          setTemperature(index, {
                            purpose: {
                              kind: "other",
                              label: event.currentTarget.value,
                            },
                          })
                        }
                      />
                    </Show>
                  </div>
                  <IconButton
                    aria-label={m.step_details_remove_temperature({
                      value1: formatNumber(index + 1),
                    })}
                    size="xs"
                    variant="plain"
                    onClick={() => setTemperatures(removeAt(temperatures(), index))}
                  >
                    <X aria-hidden="true" size={15} />
                  </IconButton>
                </div>
              )}
            </Index>
            <div>
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  setTemperatures([...temperatures(), { amount: {}, unit: "temperature.celsius" }])
                }
              >
                <Plus aria-hidden="true" size={14} />
                {m.step_details_temperature()}
              </Button>
            </div>
          </div>
        </FieldGroup>

        <FormField
          label={m.step_details_photos_of_this_step()}
          hint={
            props.photos.length > 0
              ? m.step_details_choose_a_recipe_photo_or_attach_another_without_leaving()
              : m.step_details_attach_a_photo_here_and_it_will_also_be_available_to_th()
          }
        >
          <ChangeError error={attachError()} />
          <input
            ref={(node) => {
              stepPhotoInput = node
            }}
            class={styles.fileInput}
            type="file"
            accept="image/*"
            multiple
            onChange={(event) => {
              void attachPhotos(event.currentTarget.files)
            }}
          />
          <Button
            size="sm"
            variant="outline"
            loading={attaching()}
            onClick={() => stepPhotoInput?.click()}
          >
            <ImagePlus aria-hidden="true" size={14} /> {m.step_details_attach_step_photos()}
          </Button>
          <Show when={props.photos.length > 0}>
            <div class={styles.stepPhotos}>
              <For each={props.photos}>
                {(asset) => (
                  <button
                    type="button"
                    aria-pressed={chosen().includes(asset.id)}
                    aria-label={asset.alt || m.step_details_recipe_photo()}
                    class={`${styles.stepPhoto} ${chosen().includes(asset.id) ? styles.stepPhotoChosen : ""}`}
                    onClick={() => togglePhoto(asset.id)}
                  >
                    <img
                      onError={(event) => fallbackToOriginalImage(event.currentTarget)}
                      class={styles.thumbnail}
                      src={assetURL(
                        `sha256:${asset.sha256}`,
                        {
                          recipeID: props.recipeID,
                        },
                        "thumb",
                      )}
                      alt={asset.alt ?? ""}
                    />
                  </button>
                )}
              </For>
            </div>
          </Show>
        </FormField>
      </div>
    </Dialog>
  )
}
