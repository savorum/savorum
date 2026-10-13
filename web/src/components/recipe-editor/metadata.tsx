import { For, Show, createSignal, untrack } from "solid-js"
import type { Accessor, JSX } from "solid-js"
import type { SetStoreFunction } from "solid-js/store"
import { css } from "styled-system/css"

import type { Recipe } from "@/api/client"
import type { IngredientConvention } from "@/api/ingredient-lines"
import { AuthorList, TextList, VariantList } from "@/components/recipe-editor/list-fields"
import { SourceDateField } from "@/components/recipe-editor/source-date"
import { Button } from "@/components/ui/button"
import { DetailDialog } from "@/components/ui/detail-dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { formatSourceDate } from "@/lib/date-format"
import { isolate } from "@/lib/direction"
import { recipeLanguageChoices } from "@/lib/languages"
import { fieldName } from "@/lib/recipe-areas"

import { m } from "../../lib/messages.ts"
/** Source locales offered by the editor. */
const sourceLocales: { value: string; label: string }[] = [
  {
    value: "",
    get label() {
      return m.metadata_not_set()
    },
  },
  {
    value: "en-AU",
    get label() {
      return m.metadata_australia()
    },
  },
  {
    value: "en-GB",
    get label() {
      return m.metadata_united_kingdom()
    },
  },
  {
    value: "en-US",
    get label() {
      return m.metadata_united_states()
    },
  },
  {
    value: "en-CA",
    get label() {
      return m.metadata_canada()
    },
  },
  {
    value: "en-NZ",
    get label() {
      return m.metadata_new_zealand()
    },
  },
  {
    value: "en-IE",
    get label() {
      return m.metadata_ireland()
    },
  },
  {
    value: "en-ZA",
    get label() {
      return m.metadata_south_africa()
    },
  },
]

const measurementConventions: { value: IngredientConvention; label: string }[] = [
  {
    value: "",
    get label() {
      return m.editor_measurements_unspecified()
    },
  },
  {
    value: "metric",
    get label() {
      return m.editor_measurements_metric()
    },
  },
  {
    value: "us",
    get label() {
      return m.editor_measurements_us()
    },
  },
  {
    value: "au",
    get label() {
      return m.editor_measurements_australian()
    },
  },
  {
    value: "imperial",
    get label() {
      return m.editor_measurements_imperial()
    },
  },
]

/**
 * Remarks about the recipe, and the other ways its author said it can be made.
 * Lists with nothing in them are only an add button, so the section stays one
 * line until it is used. Further optional lists, such as equipment, go inside.
 */
export function RecipeNotesEditor(props: {
  draft: Recipe
  setDraft: SetStoreFunction<Recipe>
  children?: JSX.Element
}) {
  const draft = untrack(() => props.draft)
  const setDraft = untrack(() => props.setDraft)
  return (
    <div class={styles.section}>
      <h2 class={styles.sectionTitle}>{m.metadata_notes_and_variations()}</h2>
      <div class={styles.extras}>
        <TextList
          multiline
          label={m.metadata_notes()}
          values={draft.notes ?? []}
          placeholder={m.metadata_something_worth_remembering()}
          addLabel={m.list_fields_add_note()}
          onChange={(notes) => setDraft("notes", notes)}
        />
        <VariantList
          values={draft.variants ?? []}
          onChange={(variants) => setDraft("variants", variants)}
        />
        {props.children}
      </div>
    </div>
  )
}

/**
 * The language the recipe is written in. It sits beside the title as a small
 * labelled choice rather than in a section of its own, because it is a fact
 * about the recipe as a whole and one control.
 */
export function RecipeLanguageField(props: { draft: Recipe; setDraft: SetStoreFunction<Recipe> }) {
  /** Preserves an imported language tag not offered by the editor. */
  const languageChoices = () => [
    { value: "", label: m.metadata_not_set() },
    ...recipeLanguageChoices(props.draft.language),
  ]

  return (
    <div class={styles.language}>
      <Select
        quiet
        size="sm"
        label={m.metadata_language()}
        value={props.draft.language ?? ""}
        onValueChange={(value) => props.setDraft("language", value)}
        items={languageChoices()}
      />
    </div>
  )
}

/** Where this recipe came from, and the record that travels with it. */
export function RecipeSourceEditor(props: { draft: Recipe; setDraft: SetStoreFunction<Recipe> }) {
  const draft = untrack(() => props.draft)
  const setDraft = untrack(() => props.setDraft)
  /** Preserves an imported locale not offered by the editor. */
  const localeChoices = () => {
    const current = draft.source_locale ?? ""
    if (current === "" || sourceLocales.some((locale) => locale.value === current)) {
      return sourceLocales
    }
    return [...sourceLocales, { value: current, label: current }]
  }

  return (
    <Disclosure
      label={m.metadata_source_and_attribution()}
      hint={[
        draft.source?.name,
        draft.authors?.map((author) => author.name).join(", "),
        draft.published ? formatSourceDate(draft.published) : "",
      ]
        .filter(Boolean)
        .join(" · ")}
    >
      <div class={styles.block}>
        <p class={styles.portableNote}>{m.metadata_source_travels_with_the_recipe()}</p>
        <div class={styles.pairs}>
          <FormField label={m.metadata_recipe_s_credited_source()} hint={m.metadata_credit_hint()}>
            <Input
              size="sm"
              placeholder={m.metadata_family_notebook()}
              value={draft.source?.name ?? ""}
              onInput={(event) =>
                setDraft("source", {
                  ...draft.source,
                  name: event.currentTarget.value,
                })
              }
            />
          </FormField>
          <FormField label={m.metadata_link()}>
            <Input
              size="sm"
              type="url"
              placeholder={m.metadata_https()}
              value={draft.source?.url ?? ""}
              onInput={(event) =>
                setDraft("source", {
                  ...draft.source,
                  url: event.currentTarget.value,
                })
              }
            />
          </FormField>
          <AuthorList
            values={draft.authors ?? []}
            onChange={(authors) => setDraft("authors", authors)}
          />
          <span />
          <SourceDateField
            label={m.recipe_fields_published()}
            value={draft.published}
            onChange={(value) => setDraft("published", value)}
          />
          <SourceDateField
            label={m.recipe_fields_modified()}
            value={draft.modified}
            onChange={(value) => setDraft("modified", value)}
          />
          <FormField label={m.metadata_written_for()} hint={m.metadata_written_for_hint()}>
            <Select
              size="sm"
              value={draft.source_locale ?? ""}
              onValueChange={(value) => setDraft("source_locale", value)}
              items={localeChoices()}
            />
          </FormField>
        </div>
        <div>
          <DetailDialog
            label={m.editor_source_record()}
            summary={draft.source?.application || draft.source?.source_id}
          >
            <div class={styles.grid}>
              <For each={["application", "source_id", "retrieved_at", "attribution"] as const}>
                {(field) => (
                  <FormField
                    label={
                      field === "application"
                        ? m.editor_source_application()
                        : field === "source_id"
                          ? m.editor_source_identifier()
                          : field === "retrieved_at"
                            ? m.editor_source_retrieved()
                            : m.editor_source_attribution()
                    }
                  >
                    <Input
                      size="sm"
                      value={draft.source?.[field] ?? ""}
                      onInput={(event) =>
                        setDraft("source", { ...draft.source, [field]: event.currentTarget.value })
                      }
                    />
                  </FormField>
                )}
              </For>
            </div>
          </DetailDialog>
        </div>
      </div>
    </Disclosure>
  )
}

/** The authored classification fields, in the order the recipe carries them. */
const authoredLabels = [
  {
    field: "categories",
    get placeholder() {
      return m.metadata_cake()
    },
  },
  {
    field: "courses",
    get placeholder() {
      return m.metadata_dessert()
    },
  },
  {
    field: "cuisines",
    get placeholder() {
      return m.metadata_thai()
    },
  },
  {
    field: "cooking_methods",
    get placeholder() {
      return m.metadata_baking()
    },
  },
  {
    field: "keywords",
    get placeholder() {
      return m.metadata_weeknight()
    },
  },
] as const

type AuthoredLabelField = (typeof authoredLabels)[number]["field"]

/**
 * What the recipe and its source call this dish; not the library's own tags,
 * categories and books.
 */
export function RecipeAuthoredLabels(props: { draft: Recipe; setDraft: SetStoreFunction<Recipe> }) {
  const draft = untrack(() => props.draft)
  const setDraft = untrack(() => props.setDraft)
  const values = (field: AuthoredLabelField) => draft[field] ?? []
  const [revealed, setRevealed] = createSignal<AuthoredLabelField[]>(
    untrack(() => authoredLabels.filter((entry) => values(entry.field).length > 0)).map(
      (entry) => entry.field,
    ),
  )
  const shown = (field: AuthoredLabelField) => revealed().includes(field)
  const hidden = () => authoredLabels.filter((entry) => !shown(entry.field))
  const summary = () =>
    authoredLabels
      .flatMap((entry) => values(entry.field))
      .filter(Boolean)
      .join(" · ")
  return (
    <Disclosure
      label={m.metadata_labels_from_the_recipe()}
      hint={summary() || m.metadata_none_recorded()}
    >
      <div class={styles.block}>
        <p class={styles.portableNote}>{m.metadata_authored_labels_are_not_your_filing()}</p>
        <div class={styles.grid}>
          <For each={authoredLabels.filter((entry) => shown(entry.field))}>
            {(entry) => (
              <TextList
                label={fieldName(entry.field)}
                values={values(entry.field)}
                placeholder={entry.placeholder}
                onChange={(next) => setDraft(entry.field, next)}
              />
            )}
          </For>
        </div>
        <Show when={hidden().length > 0}>
          <div class={styles.add}>
            <For each={hidden()}>
              {(entry) => (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => setRevealed((current) => [...current, entry.field])}
                >
                  {m.metadata_add_label_group({ value1: isolate(fieldName(entry.field)) })}
                </Button>
              )}
            </For>
          </div>
        </Show>
      </div>
    </Disclosure>
  )
}

const styles = {
  block: css({ display: "grid", gap: "4" }),
  section: css({ display: "grid", gap: "4" }),
  // Beside the title's own label, so it takes no line of its own.
  language: css({ insetInlineEnd: "0", position: "absolute", top: "-0.4rem" }),
  extras: css({ alignItems: "start", display: "flex", flexWrap: "wrap", gap: "4" }),
  sectionTitle: css({
    color: "fg.default",
    fontFamily: "editorial",
    fontSize: "2xl",
    fontWeight: "semibold",
    margin: "0",
  }),
  add: css({ display: "flex", flexWrap: "wrap", gap: "2" }),
  // Two even columns, so fields on one row line up whatever their labels say.
  pairs: css({
    columnGap: "5",
    display: "grid",
    gridTemplateColumns: { base: "1fr", sm: "repeat(2, minmax(0, 1fr))" },
    rowGap: "5",
    "& > span:empty": { display: { base: "none", sm: "block" } },
  }),
  grid: css({
    display: "grid",
    gap: "4",
    gridTemplateColumns: {
      base: "1fr",
      sm: "repeat(auto-fit, minmax(11rem, 1fr))",
    },
  }),
  portableNote: css({
    color: "fg.muted",
    fontSize: "xs",
    lineHeight: "1.5",
    margin: "0",
    maxWidth: "44rem",
  }),
}

export function RecipeEditorPreferences(props: {
  convention: Accessor<IngredientConvention>
  onConventionChange: (convention: IngredientConvention) => void
}) {
  return (
    <div data-editor-preferences>
      <Select
        quiet
        size="sm"
        label={m.editor_measurements()}
        value={props.convention()}
        onValueChange={(value) => props.onConventionChange(value as IngredientConvention)}
        items={measurementConventions}
      />
    </div>
  )
}
