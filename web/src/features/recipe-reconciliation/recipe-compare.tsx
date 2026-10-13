import { For, Show, createMemo, createSignal } from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import type { Recipe } from "@/api/client"
import type {
  RecipeMergeConflict,
  RecipeMergePreview,
  SourceRefreshPreview,
} from "@/api/recipe-reconciliation"
import { RecipeArticle } from "@/components/recipe-article"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/disclosure"
import { isolate } from "@/lib/direction"
import { areaName, fieldName, type RecipeArea } from "@/lib/recipe-areas"
import { asWritten, yieldLine } from "@/lib/recipe-format"
import { timeSummaryLine } from "@/lib/timing-summary"

import { m } from "../../lib/messages.ts"
import { areaMatches, areaReading, comparableAreas, conflictLines } from "./recipe-values"

const shell = pageShell()

const styles = {
  panel: css({ display: "grid", gap: "4", maxWidth: "60rem" }),
  conflict: css({
    borderWidth: "1px",
    borderColor: "border.default",
    borderRadius: "l2",
    padding: "4",
  }),
  choices: css({ display: "flex", flexWrap: "wrap", gap: "4", marginTop: "3" }),
  value: css({ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }),
  summary: css({ color: "fg.muted", fontSize: "sm" }),
  heads: css({
    display: "grid",
    gap: "4",
    gridTemplateColumns: { base: "1fr", md: "repeat(2, minmax(0, 1fr))" },
  }),
  head: css({ display: "grid", gap: "1" }),
  headTitle: css({ fontFamily: "editorial", fontSize: "xl", fontWeight: "normal", margin: "0" }),
  headNote: css({ color: "fg.muted", fontSize: "sm", margin: "0" }),
  area: css({
    borderTopWidth: "1px",
    borderColor: "border.subtle",
    display: "grid",
    gap: "3",
    paddingTop: "4",
  }),
  areaHeading: css({
    alignItems: "baseline",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    fontWeight: "semibold",
    gap: "2",
    margin: "0",
  }),
  badgeSame: css({ color: "fg.muted", fontSize: "xs", fontWeight: "normal" }),
  badgeDifferent: css({ color: "accent.default", fontSize: "xs", fontWeight: "semibold" }),
  columns: css({
    display: "grid",
    gap: "4",
    gridTemplateColumns: { base: "1fr", md: "repeat(2, minmax(0, 1fr))" },
  }),
  // What both recipes already agree on is still readable, but it is not what
  // a reader is here to weigh.
  agreed: css({ color: "fg.muted" }),
  reading: css({ display: "grid", gap: "2", minWidth: "0" }),
  readingLabel: css({ color: "fg.muted", fontSize: "xs", margin: "0" }),
  lines: css({
    display: "grid",
    gap: "1",
    listStyle: "none",
    margin: "0",
    padding: "0",
    overflowWrap: "anywhere",
    whiteSpace: "pre-wrap",
  }),
  missing: css({ color: "fg.muted", fontSize: "sm", fontStyle: "italic", margin: "0" }),
  outcome: css({
    borderWidth: "1px",
    borderColor: "border.default",
    borderRadius: "l2",
    display: "grid",
    gap: "2",
    padding: "4",
  }),
  outcomeTitle: css({ fontSize: "sm", fontWeight: "semibold", margin: "0" }),
  outcomeList: css({ display: "grid", gap: "1", margin: "0", paddingInlineStart: "5" }),
  actions: css({ display: "flex", flexWrap: "wrap", gap: "3" }),
}

/** How a recipe identifies itself beside another one: what it makes and how long it takes. */
function recipeNote(recipe: Recipe): string {
  return [yieldLine(recipe.yields?.[0], asWritten), timeSummaryLine(recipe), recipe.source?.name]
    .filter(Boolean)
    .join(" · ")
}

/** One side of a disagreement, read as the recipe reads it. */
function ConflictSide(props: { title: string; lines: string[] }) {
  return (
    <div class={styles.reading}>
      <p class={styles.readingLabel}>{props.title}</p>
      <Show
        when={props.lines.length > 0}
        fallback={<p class={styles.missing}>{m.recipe_compare_this_recipe_has_none()}</p>}
      >
        <ul class={styles.lines}>
          <For each={props.lines}>{(line) => <li>{line}</li>}</For>
        </ul>
      </Show>
    </div>
  )
}

function AreaReading(props: { recipe: Recipe; area: RecipeArea; label?: string }) {
  const readings = createMemo(() => areaReading(props.recipe, props.area))
  return (
    <div class={styles.reading}>
      <Show when={props.label}>
        <p class={styles.readingLabel}>{props.label}</p>
      </Show>
      <Show
        when={readings().length > 0}
        fallback={<p class={styles.missing}>{m.recipe_compare_this_recipe_has_none()}</p>}
      >
        <For each={readings()}>
          {(reading) => (
            <div class={styles.reading}>
              <Show when={readings().length > 1}>
                <p class={styles.readingLabel}>{reading.label}</p>
              </Show>
              <ul class={styles.lines}>
                <For each={reading.lines}>{(line) => <li>{line}</li>}</For>
              </ul>
            </div>
          )}
        </For>
      </Show>
    </div>
  )
}

/**
 * Two recipes read side by side, and nothing else. Combining is a decision of
 * its own on the next screen: this one only has to make the two legible.
 */
export function RecipeComparison(props: {
  left: Recipe
  right: Recipe
  onCombine: () => void
  children?: never
}) {
  const areas = createMemo(() => comparableAreas(props.left, props.right))
  const differing = createMemo(() =>
    areas().filter((area) => !areaMatches(props.left, props.right, area)),
  )
  return (
    <section class={styles.panel} aria-labelledby="recipe-comparison-title">
      <h1 id="recipe-comparison-title" class={shell.title}>
        {m.recipe_compare_compare_two_recipes()}
      </h1>
      <p class={styles.summary}>
        <Show
          when={differing().length > 0}
          fallback={m.recipe_compare_these_two_recipes_read_alike()}
        >
          {m.recipe_compare_differs_in({ areas: isolate(differing().map(areaName).join(", ")) })}
        </Show>{" "}
        {m.recipe_compare_nothing_changes_until_you_combine()}
      </p>
      <div class={styles.heads}>
        <div class={styles.head}>
          <h2 class={styles.headTitle}>{props.left.title}</h2>
          <p class={styles.headNote}>{recipeNote(props.left)}</p>
        </div>
        <div class={styles.head}>
          <h2 class={styles.headTitle}>{props.right.title}</h2>
          <p class={styles.headNote}>{recipeNote(props.right)}</p>
        </div>
      </div>
      <For each={areas()}>
        {(area) => {
          const same = () => areaMatches(props.left, props.right, area)
          return (
            <section class={styles.area} aria-label={areaName(area)}>
              <h3 class={styles.areaHeading}>
                {areaName(area)}
                <Show
                  when={same()}
                  fallback={
                    <span class={styles.badgeDifferent}>{m.recipe_compare_different()}</span>
                  }
                >
                  <span class={styles.badgeSame}>{m.recipe_compare_the_same_in_both()}</span>
                </Show>
              </h3>
              <Show
                when={same()}
                fallback={
                  <div class={styles.columns}>
                    <AreaReading recipe={props.left} area={area} label={props.left.title} />
                    <AreaReading recipe={props.right} area={area} label={props.right.title} />
                  </div>
                }
              >
                <div class={styles.agreed}>
                  <AreaReading recipe={props.left} area={area} />
                </div>
              </Show>
            </section>
          )
        }}
      </For>
      <div class={styles.actions}>
        <Button colorPalette="accent" onClick={props.onCombine}>
          {m.recipe_compare_combine_these_recipes()}
        </Button>
      </div>
    </section>
  )
}

function Choice(props: {
  path: string
  name: string
  leftLabel: string
  rightLabel: string
  onChange: (value: "local" | "remote") => void
}) {
  return (
    <fieldset
      class={styles.choices}
      aria-label={m.recipe_compare_choose_the_to_keep({ value1: props.name })}
    >
      {/* The question, not the noun again: the part is already named above
          these values, and a second copy of the word reads as a heading that
          lost its content. */}
      <legend>{m.recipe_compare_choose_the_to_keep({ value1: isolate(props.name) })}</legend>
      <label>
        <input
          type="radio"
          name={`choice-${props.path}`}
          value="local"
          onChange={() => props.onChange("local")}
        />{" "}
        {props.leftLabel}
      </label>
      <label>
        <input
          type="radio"
          name={`choice-${props.path}`}
          value="remote"
          onChange={() => props.onChange("remote")}
        />{" "}
        {props.rightLabel}
      </label>
    </fieldset>
  )
}

export function SourceRefreshReview(props: {
  preview: SourceRefreshPreview
  onCommit: (resolutions: { path: string; choice: "local" | "remote" }[]) => void
  busy?: boolean
}) {
  const [choices, setChoices] = createSignal<Record<string, "local" | "remote">>({})
  return (
    <section class={styles.panel} aria-labelledby="source-refresh-title">
      <h1 id="source-refresh-title" class={shell.title}>
        {m.recipe_compare_review_source_updates()}
      </h1>
      <Disclosure label={m.recipe_compare_updated_recipe()}>
        <RecipeArticle recipe={props.preview.merged} readOnly />
      </Disclosure>
      <Show
        when={props.preview.conflicts.length === 0}
        fallback={
          <For each={props.preview.conflicts}>
            {(conflict) => (
              <div class={styles.conflict}>
                {/* The part of the recipe, then the thing in it, and not the
                    same word twice when the part is the thing. */}
                <strong>{areaName(conflict.area as RecipeArea)}</strong>
                <Show when={fieldName(conflict.path) !== areaName(conflict.area as RecipeArea)}>
                  <p>{fieldName(conflict.path)}</p>
                </Show>
                <ConflictSide
                  title={m.recipe_compare_my_edit()}
                  lines={conflictLines(props.preview.local, conflict.path, conflict.local)}
                />
                <ConflictSide
                  title={m.recipe_compare_source()}
                  lines={conflictLines(props.preview.remote, conflict.path, conflict.remote)}
                />
                <Choice
                  path={conflict.path}
                  name={fieldName(conflict.path)}
                  leftLabel={m.recipe_compare_keep_my_edit()}
                  rightLabel={m.recipe_compare_use_source()}
                  onChange={(value) =>
                    setChoices((current) => ({ ...current, [conflict.path]: value }))
                  }
                />
              </div>
            )}
          </For>
        }
      >
        <p class={styles.summary}>{m.recipe_compare_no_conflicts_need_your_attention()}</p>
      </Show>
      <Button
        disabled={props.busy || Object.keys(choices()).length !== props.preview.conflicts.length}
        onClick={() =>
          props.onCommit(Object.entries(choices()).map(([path, choice]) => ({ path, choice })))
        }
      >
        {props.busy ? m.recipe_compare_applying() : m.recipe_compare_apply_reviewed_updates()}
      </Button>
    </section>
  )
}

/**
 * The decision, kept apart from the reading of it: which recipe survives, what
 * is taken from the other, and what the library is left holding.
 */
export function RecipeCombine(props: {
  preview: RecipeMergePreview
  survivor: "left" | "right"
  onSurvivorChange: (survivor: "left" | "right") => void
  onMerge: (recipe: RecipeMergePreview["suggested"]) => void
  onBack: () => void
  busy?: boolean
}) {
  const [choices, setChoices] = createSignal<Record<string, "local" | "remote">>({})
  const keptTitle = () =>
    props.survivor === "left" ? props.preview.left.recipe.title : props.preview.right.recipe.title
  const trashedTitle = () =>
    props.survivor === "left" ? props.preview.right.recipe.title : props.preview.left.recipe.title
  const selected = () => {
    const result = structuredClone(props.preview.suggested) as Record<string, unknown>
    const left = props.preview.left.recipe as Record<string, unknown>
    const right = props.preview.right.recipe as Record<string, unknown>
    for (const [path, choice] of Object.entries(choices())) {
      result[path] = structuredClone((choice === "local" ? left : right)[path])
    }
    return result as RecipeMergePreview["suggested"]
  }
  const decided = () => Object.keys(choices()).length === props.preview.conflicts.length
  const chosenTitle = (path: string) => {
    const choice = choices()[path]
    if (!choice) return undefined
    return choice === "local" ? props.preview.left.recipe.title : props.preview.right.recipe.title
  }
  return (
    <section class={styles.panel} aria-labelledby="recipe-combine-title">
      <h1 id="recipe-combine-title" class={shell.title}>
        {m.recipe_compare_combine_into_one_recipe()}
      </h1>
      <fieldset class={styles.choices} aria-label={m.recipe_compare_which_recipe_do_you_keep()}>
        <legend>{m.recipe_compare_which_recipe_do_you_keep()}</legend>
        <label>
          <input
            type="radio"
            name="survivor"
            value="left"
            checked={props.survivor === "left"}
            onChange={() => props.onSurvivorChange("left")}
          />{" "}
          {props.preview.left.recipe.title}
        </label>
        <label>
          <input
            type="radio"
            name="survivor"
            value="right"
            checked={props.survivor === "right"}
            onChange={() => props.onSurvivorChange("right")}
          />{" "}
          {props.preview.right.recipe.title}
        </label>
      </fieldset>
      <Show when={props.preview.conflicts.length > 0}>
        <p class={styles.summary}>{m.recipe_compare_whole_parts_only()}</p>
        <For each={props.preview.conflicts}>
          {(conflict: RecipeMergeConflict) => (
            <div class={styles.conflict}>
              <strong>{fieldName(conflict.path)}</strong>
              <ConflictSide
                title={props.preview.left.recipe.title}
                lines={conflictLines(props.preview.left.recipe, conflict.path, conflict.left)}
              />
              <ConflictSide
                title={props.preview.right.recipe.title}
                lines={conflictLines(props.preview.right.recipe, conflict.path, conflict.right)}
              />
              <Choice
                path={conflict.path}
                name={fieldName(conflict.path)}
                leftLabel={props.preview.left.recipe.title}
                rightLabel={props.preview.right.recipe.title}
                onChange={(value) =>
                  setChoices((current) => ({ ...current, [conflict.path]: value }))
                }
              />
            </div>
          )}
        </For>
      </Show>
      <div class={styles.outcome}>
        <h2 class={styles.outcomeTitle}>{m.recipe_compare_what_combining_does()}</h2>
        <ul class={styles.outcomeList}>
          <li>{m.recipe_compare_the_library_keeps({ value1: isolate(keptTitle()) })}</li>
          <For each={props.preview.conflicts}>
            {(conflict) => (
              <li>
                {chosenTitle(conflict.path)
                  ? m.recipe_compare_part_comes_from({
                      value1: isolate(fieldName(conflict.path)),
                      value2: isolate(chosenTitle(conflict.path)!),
                    })
                  : m.recipe_compare_part_not_chosen_yet({
                      value1: isolate(fieldName(conflict.path)),
                    })}
              </li>
            )}
          </For>
          <li>{m.recipe_compare_the_other_goes_to_trash({ value1: isolate(trashedTitle()) })}</li>
        </ul>
        <Disclosure label={m.recipe_compare_the_combined_recipe()}>
          <RecipeArticle recipe={selected()} readOnly />
        </Disclosure>
      </div>
      <div class={styles.actions}>
        <Button
          colorPalette="accent"
          disabled={props.busy || !decided()}
          onClick={() => props.onMerge(selected())}
        >
          {props.busy ? m.recipe_compare_merging() : m.recipe_compare_combine_recipes()}
        </Button>
        <Button variant="outline" onClick={props.onBack}>
          {m.recipe_compare_back_to_the_comparison()}
        </Button>
      </div>
    </section>
  )
}
