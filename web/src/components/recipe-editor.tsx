import { Show, createEffect, createMemo, createSignal, createUniqueId, on, untrack } from "solid-js"
import { createStore } from "solid-js/store"
import { css } from "styled-system/css"

import type { Recipe } from "@/api/client"
import type { IngredientConvention } from "@/api/ingredient-lines"
import { RecipeEquipmentEditor } from "@/components/recipe-editor/equipment"
import { RecipeClaimsEditor } from "@/components/recipe-editor/facts"
import { RecipeIngredientsEditor } from "@/components/recipe-editor/ingredients"
import { RecipeInstructionsEditor } from "@/components/recipe-editor/instructions"
import { RecipeNutritionEditor } from "@/components/recipe-editor/nutrition"
import { RecipePhotosEditor, attachRecipeImages } from "@/components/recipe-editor/photos"
import { StepDetails } from "@/components/recipe-editor/step-details"
import { Callout, ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { isolate } from "@/lib/direction"
import { readingsPending, readingsSettled } from "@/lib/line-readings"
import { formatNumber } from "@/lib/locale-format"
import { emptyIngredient, emptyStep } from "@/lib/recipe-draft"
import { withNote, writtenLine } from "@/lib/recipe-format"
import { type MeasurementStyle, styleOf } from "@/lib/unit-choices"

import { m } from "../lib/messages.ts"
import {
  RecipeAuthoredLabels,
  RecipeEditorPreferences,
  RecipeLanguageField,
  RecipeNotesEditor,
  RecipeSourceEditor,
} from "./recipe-editor/metadata"
import { prepareRecipe } from "./recipe-editor/prepare-recipe"
import { RecipeYieldTiming } from "./recipe-editor/yield-timing"
export interface RecipeEditorProps {
  initial: Recipe
  /**
   * What `initial` is measured against. Recovered entry opens the form already
   * changed, so unsaved work is still announced and still blocks leaving.
   */
  baseline?: Recipe
  entryID?: string
  submitLabel: string
  pending?: boolean
  error?: unknown
  onSubmit: (recipe: Recipe) => void
  onCancel: () => void
  onDirtyChange?: (dirty: boolean) => void
  /** The entry as it stands, or nothing when it is back to the baseline. */
  onDraftChange?: (recipe: Recipe | undefined) => void
  onReload?: () => void
}

export const RecipeEditor = (props: RecipeEditorProps) => {
  let form: HTMLFormElement | undefined
  let errorSummary: HTMLDivElement | undefined
  const errorID = `recipe-editor-error-${createUniqueId()}`
  const [announcement, setAnnouncement] = createSignal("")
  const [draft, setDraft] = createStore<Recipe>(structuredClone(props.initial))
  const initialSnapshot = untrack(() => JSON.stringify(props.baseline ?? props.initial))
  const [uncommitted, setUncommitted] = createSignal(false)
  const [uploads, setUploads] = createSignal(0)
  const attachImages = async (files: FileList | null) => {
    setUploads((count) => count + 1)
    try {
      return await attachRecipeImages(setDraft, files)
    } finally {
      setUploads((count) => count - 1)
    }
  }
  const [validation, setValidation] = createSignal(false)
  createEffect(() => {
    const changed = JSON.stringify(draft) !== initialSnapshot
    props.onDirtyChange?.(uncommitted() || changed || readingsPending() > 0 || uploads() > 0)
    props.onDraftChange?.(changed ? draft : undefined)
  })

  const focusItem = (kind: "ingredient" | "step", id: string, control = "primary") => {
    requestAnimationFrame(() => {
      const row = Array.from(
        form?.querySelectorAll<HTMLElement>(`[data-editor-kind="${kind}"]`) ?? [],
      ).find((candidate) => candidate.dataset.editorId === id)
      row?.querySelector<HTMLElement>(`[data-focus="${control}"]`)?.focus()
    })
  }

  createEffect(
    on(
      () => props.error,
      (error, previous) => {
        if (!error || error === previous) return
        queueMicrotask(() => {
          const invalid = form?.querySelector<HTMLElement>(":invalid")
          ;(invalid ?? errorSummary)?.focus()
        })
      },
    ),
  )
  const [stepDetails, setStepDetails] = createSignal<[number, number] | undefined>()

  const [convention, setConvention] = createSignal<IngredientConvention>("")
  // Until a convention is chosen, the editor offers the measures the recipe is written in.
  const style = (): MeasurementStyle => {
    const chosen = convention()
    if (chosen === "us" || chosen === "au") return chosen
    if (chosen !== "") return "metric"
    const units = (draft.ingredient_sections ?? []).flatMap((section) =>
      (section.ingredients ?? []).map((ingredient) =>
        ingredient.quantity?.kind === "simple" ? (ingredient.quantity.unit ?? "") : "",
      ),
    )
    return styleOf(units, "metric")
  }
  const assets = () => draft.assets ?? []

  /**
   * The ingredients a step can be tied to, named by the amount and name the
   * editor holds rather than the authored source line.
   */
  const ingredientChoices = createMemo(() =>
    (draft.ingredient_sections ?? []).flatMap((section) =>
      (section.ingredients ?? []).flatMap((ingredient) => {
        // The row's reading, and a field label without the amount.
        const name =
          ingredient.name?.trim() ||
          ingredient.alternatives?.map((branch) => branch.name).join(" / ") ||
          m.recipe_editor_untitled_ingredient()
        const named = withNote(name, ingredient.note?.trim())
        const label = [section.title?.trim(), writtenLine(ingredient) || named]
          .filter(Boolean)
          .join(" · ")
        return [
          {
            id: ingredient.id,
            label,
            name: named,
            choice: Boolean(ingredient.alternatives?.length),
          },
          ...(ingredient.alternatives ?? []).map((branch) => ({
            id: branch.id,
            get label() {
              return m.recipe_editor_when_included({
                value1: isolate(writtenLine(branch) || (branch.name ?? "")),
              })
            },
            name: branch.name ?? "",
            choice: false,
          })),
        ]
      }),
    ),
  )

  const editedStep = () => {
    const position = stepDetails()
    if (!position) return undefined
    const sections = draft.instruction_sections ?? []
    const stage = sections[position[0]]
    const step = stage?.steps?.[position[1]]
    // The stage names the step only where there is more than one to tell
    // apart; a single unnamed stage is not a place a cook can be told about.
    return step
      ? {
          section: position[0],
          index: position[1],
          step,
          stageTitle: sections.length > 1 ? stage?.title : undefined,
        }
      : undefined
  }

  const submit = (event: SubmitEvent) => {
    event.preventDefault()
    if (uploads() > 0) return
    setValidation(false)
    // A line still being read into its amount and name is part of the recipe.
    void readingsSettled().then(() =>
      untrack(() => {
        if (uploads() === 0) props.onSubmit(prepareRecipe(draft))
      }),
    )
  }

  return (
    <form
      ref={(node) => {
        form = node
      }}
      class={styles.form}
      inert={props.pending}
      aria-busy={props.pending}
      onSubmit={submit}
      onInput={(event) => {
        if (!event.target.closest("[data-editor-preferences]")) setUncommitted(true)
      }}
      onFocusOut={() => queueMicrotask(() => setUncommitted(false))}
      onInvalid={(event) => {
        setValidation(true)
        let parent = event.target.parentElement
        while (parent && parent !== form) {
          if (parent instanceof HTMLDetailsElement) parent.open = true
          parent = parent.parentElement
        }
        queueMicrotask(() => form?.querySelector<HTMLElement>(":invalid")?.focus())
      }}
      onKeyDown={(event) => {
        if (event.defaultPrevented || event.key !== "Enter" || event.isComposing) return
        const target = event.target as HTMLElement
        const row = target.closest<HTMLElement>("[data-editor-kind][data-editor-id]")
        if (!row) return
        const shortcut = event.ctrlKey || event.metaKey
        if (
          !shortcut &&
          !(
            target instanceof HTMLInputElement &&
            target.dataset.addRowOnEnter !== undefined &&
            row.dataset.editorKind === "ingredient"
          )
        )
          return
        const kind = row.dataset.editorKind
        const id = row.dataset.editorId
        if (kind === "ingredient") {
          for (const [sectionIndex, section] of (draft.ingredient_sections ?? []).entries()) {
            const index = section.ingredients?.findIndex((item) => item.id === id) ?? -1
            if (index < 0) continue
            event.preventDefault()
            const item = emptyIngredient()
            setDraft("ingredient_sections", sectionIndex, "ingredients", (items) => [
              ...(items ?? []).slice(0, index + 1),
              item,
              ...(items ?? []).slice(index + 1),
            ])
            setAnnouncement(m.editor_ingredient_inserted({ position: formatNumber(index + 2) }))
            focusItem("ingredient", item.id)
            return
          }
        }
        if (kind === "step") {
          for (const [sectionIndex, section] of (draft.instruction_sections ?? []).entries()) {
            const index = section.steps?.findIndex((item) => item.id === id) ?? -1
            if (index < 0) continue
            event.preventDefault()
            const item = emptyStep()
            setDraft("instruction_sections", sectionIndex, "steps", (items) => [
              ...(items ?? []).slice(0, index + 1),
              item,
              ...(items ?? []).slice(index + 1),
            ])
            setAnnouncement(m.editor_step_inserted({ position: formatNumber(index + 2) }))
            focusItem("step", item.id)
            return
          }
        }
      }}
      aria-describedby={props.error ? errorID : undefined}
    >
      <div
        id={errorID}
        ref={(node) => {
          errorSummary = node
        }}
        tabIndex={-1}
      >
        <ChangeError error={props.error} onReload={props.onReload} />
      </div>
      <span class={styles.visuallyHidden} aria-live="polite">
        {announcement()}
      </span>

      <div class={styles.heading}>
        <FormField label={m.recipe_editor_title()} required>
          <Input
            data-recipe-title
            size="lg"
            required
            maxLength={1000}
            placeholder={m.recipe_editor_what_are_you_cooking()}
            value={draft.title}
            onInput={(event) => setDraft("title", event.currentTarget.value)}
          />
        </FormField>
        <RecipeLanguageField draft={draft} setDraft={setDraft} />
      </div>

      <FormField label={m.metadata_summary()}>
        <Textarea
          size="sm"
          placeholder={m.metadata_a_sentence_or_two_about_the_recipe()}
          value={draft.description ?? ""}
          onInput={(event) => setDraft("description", event.currentTarget.value)}
        />
      </FormField>

      <div>
        <RecipePhotosEditor
          onAttachPhotos={attachImages}
          draft={draft}
          setDraft={setDraft}
          entryID={props.entryID}
          onAnnounce={setAnnouncement}
        />
        <RecipeYieldTiming draft={draft} setDraft={setDraft} style={style()} />
      </div>

      <RecipeIngredientsEditor
        draft={draft}
        setDraft={setDraft}
        style={style()}
        convention={convention()}
        onAnnounce={setAnnouncement}
        onFocus={(id, control) => focusItem("ingredient", id, control ?? "primary")}
        headerExtra={
          <RecipeEditorPreferences convention={convention} onConventionChange={setConvention} />
        }
      />

      <RecipeInstructionsEditor
        draft={draft}
        setDraft={setDraft}
        onAnnounce={setAnnouncement}
        onFocus={(id, control) => focusItem("step", id, control ?? "primary")}
        onEditDetails={(section, step) =>
          requestAnimationFrame(() => setStepDetails([section, step]))
        }
      />

      <RecipeNotesEditor draft={draft} setDraft={setDraft}>
        <RecipeEquipmentEditor draft={draft} setDraft={setDraft} style={style()} />
      </RecipeNotesEditor>

      <div class={styles.more}>
        <h2 class={styles.moreTitle}>{m.editor_more_details()}</h2>
        <div>
          <RecipeNutritionEditor draft={draft} setDraft={setDraft} style={style()} />
          <RecipeClaimsEditor draft={draft} setDraft={setDraft} />
          <RecipeSourceEditor draft={draft} setDraft={setDraft} />
          <RecipeAuthoredLabels draft={draft} setDraft={setDraft} />
        </div>
      </div>

      <Show when={editedStep()} keyed>
        {(edited) => (
          <StepDetails
            step={edited.step}
            position={edited.index + 1}
            stageTitle={edited.stageTitle}
            recipeID={props.entryID ?? "new"}
            photos={assets()}
            ingredients={ingredientChoices()}
            style={style()}
            onAttachPhotos={attachImages}
            onChange={(step) =>
              setDraft("instruction_sections", edited.section, "steps", edited.index, step)
            }
            onClose={() => setStepDetails(undefined)}
          />
        )}
      </Show>

      <div class={styles.actions}>
        <Show when={validation()}>
          <Callout class={styles.validation} tone="warning">
            {m.editor_validation()}
          </Callout>
        </Show>
        {/* The refusal is at the top of a long form; this is the way to it. */}
        <Show when={props.error}>
          <Button variant="plain" onClick={() => errorSummary?.focus()}>
            {m.editor_see_the_problem()}
          </Button>
        </Show>
        <Button
          type="submit"
          disabled={uploads() > 0}
          colorPalette="accent"
          loading={props.pending}
        >
          {props.submitLabel}
        </Button>
        <Button variant="plain" onClick={props.onCancel}>
          {m.common_cancel()}
        </Button>
      </div>
    </form>
  )
}

const styles = {
  form: css({ display: "grid", gap: "8" }),
  heading: css({ display: "grid", gap: "3", position: "relative" }),
  more: css({ display: "grid", gap: "2" }),
  moreTitle: css({
    color: "fg.default",
    fontFamily: "editorial",
    fontSize: "2xl",
    fontWeight: "semibold",
    margin: "0",
  }),
  visuallyHidden: css({
    border: "0",
    clip: "rect(0 0 0 0)",
    height: "1px",
    margin: "-1px",
    overflow: "hidden",
    padding: "0",
    position: "absolute",
    whiteSpace: "nowrap",
    width: "1px",
  }),
  // The alert sits in the sticky bar beside the buttons, so it keeps the bar's
  // own spacing and takes only as much width as the sentence needs.
  validation: css({ marginBlock: "0", maxWidth: "28rem" }),
  actions: css({
    position: "sticky",
    bottom: "0",
    background: "bg.canvas",
    borderTopWidth: "1px",
    borderColor: "border.subtle",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    paddingBlock: "4",
    zIndex: "docked",
  }),
}
