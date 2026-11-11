import { For, Show, createMemo, createSignal, type JSX } from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { type LibraryEntry, type Recipe, assetURL, fallbackToOriginalImage } from "@/api/client"
import type { RecipeMergePreview, SourceRefreshPreview } from "@/api/recipe-reconciliation"
import { RecipeArticle } from "@/components/recipe-article"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/disclosure"
import { isolate } from "@/lib/direction"
import { formatNumber } from "@/lib/locale-format"
import { areaName, fieldName, type RecipeArea } from "@/lib/recipe-areas"
import { asWritten, yieldLine } from "@/lib/recipe-format"
import { safeSourceURL } from "@/lib/source-url"
import { timeSummaryLine } from "@/lib/timing-summary"

import { m } from "../../lib/messages.ts"
import {
  type DuplicateEvidence,
  areaMatches,
  areaReading,
  comparableAreas,
  conflictAreas,
  conflictLines,
  duplicateEvidence,
  linesOnlyHere,
  photoAssets,
} from "./recipe-values"

const shell = pageShell()

const styles = {
  panel: css({ display: "grid", gap: "5", maxWidth: "60rem" }),
  conflict: css({
    borderWidth: "1px",
    borderColor: "border.default",
    borderRadius: "l2",
    padding: "4",
  }),
  choices: css({ display: "flex", flexWrap: "wrap", gap: "4", marginTop: "3" }),
  summary: css({ color: "fg.muted", fontSize: "sm", margin: "0" }),
  verdict: css({ display: "grid", gap: "1" }),
  verdictTitle: css({ fontSize: "lg", fontWeight: "semibold", margin: "0" }),
  heads: css({
    display: "grid",
    gap: "4",
    gridTemplateColumns: { base: "1fr", md: "repeat(2, minmax(0, 1fr))" },
  }),
  head: css({ display: "grid", gap: "1", alignContent: "start" }),
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
  columns: css({
    display: "grid",
    gap: "4",
    gridTemplateColumns: { base: "1fr", md: "repeat(2, minmax(0, 1fr))" },
  }),
  agreed: css({ color: "fg.muted" }),
  reading: css({ display: "grid", gap: "2", alignContent: "start", minWidth: "0" }),
  readingLabel: css({ color: "fg.muted", fontSize: "xs", margin: "0" }),
  columnLabel: css({
    fontSize: "sm",
    fontWeight: "semibold",
    margin: "0",
    md: { srOnly: true },
  }),
  lines: css({
    display: "grid",
    gap: "1",
    listStyle: "none",
    margin: "0",
    padding: "0",
    overflowWrap: "anywhere",
    whiteSpace: "pre-wrap",
  }),
  onlyHere: css({
    borderRadius: "l1",
    background: "accent.subtle.bg",
    color: "accent.subtle.fg",
    marginInline: "-1",
    paddingInline: "1",
  }),
  link: css({
    textDecoration: "underline",
    textDecorationColor: "border.default",
    textUnderlineOffset: "3px",
    _hover: { color: "accent.default" },
  }),
  visuallyHidden: css({ srOnly: true }),
  missing: css({ color: "fg.muted", fontSize: "sm", fontStyle: "italic", margin: "0" }),
  photos: css({
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    listStyle: "none",
    margin: "0",
    padding: "0",
  }),
  photo: css({ display: "grid", gap: "1", width: "6rem" }),
  thumbnail: css({
    aspectRatio: "4 / 3",
    borderRadius: "l1",
    objectFit: "cover",
    width: "full",
    background: "bg.muted",
  }),
  photoLabel: css({ color: "fg.muted", fontSize: "xs", overflowWrap: "anywhere" }),
  fieldset: css({ border: "none", display: "grid", gap: "3", margin: "0", padding: "0" }),
  legend: css({ display: "grid", gap: "1", padding: "0", marginBottom: "3" }),
  legendTitle: css({ fontSize: "md", fontWeight: "semibold", margin: "0" }),
  options: css({
    display: "grid",
    gap: "3",
    gridTemplateColumns: { base: "1fr", md: "repeat(2, minmax(0, 1fr))" },
  }),
  option: css({
    alignContent: "start",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    cursor: "pointer",
    display: "grid",
    gap: "2",
    gridTemplateColumns: "auto minmax(0, 1fr)",
    minHeight: "11",
    padding: "3",
    _hover: { borderColor: "border.emphasized" },
    "&:has(input:checked)": { borderColor: "accent.default", background: "bg.subtle" },
    "&:has(input:focus-visible)": { outline: "2px solid", outlineColor: "accent.default" },
  }),
  optionInput: css({ alignSelf: "start", marginTop: "1", accentColor: "accent.default" }),
  optionBody: css({ display: "grid", gap: "2", minWidth: "0" }),
  optionTitle: css({ fontWeight: "semibold" }),
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
  actions: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "3" }),
}

/** How a recipe identifies itself beside another one: what it makes and how long it takes. */
function recipeNote(recipe: Recipe): string {
  return [yieldLine(recipe.yields?.[0], asWritten), timeSummaryLine(recipe), recipe.source?.name]
    .filter(Boolean)
    .join(" · ")
}

function LineText(props: { line: string }) {
  const parts = () => props.line.split(/(https?:\/\/\S+)/)
  return (
    <For each={parts()}>
      {(part) => {
        const url = safeSourceURL(part)
        return url ? (
          <a class={styles.link} href={url} target="_blank" rel="noreferrer">
            {part}
          </a>
        ) : (
          part
        )
      }}
    </For>
  )
}

function Lines(props: { lines: string[]; others?: string[] }) {
  const onlyHere = () => {
    const marked = props.others ? linesOnlyHere(props.lines, props.others) : new Set<number>()
    return marked.size < props.lines.length ? marked : new Set<number>()
  }
  return (
    <Show
      when={props.lines.length > 0}
      fallback={<p class={styles.missing}>{m.recipe_compare_this_recipe_has_none()}</p>}
    >
      <ul class={styles.lines}>
        <For each={props.lines}>
          {(line, index) => (
            <li>
              <Show when={onlyHere().has(index())} fallback={<LineText line={line} />}>
                <span class={styles.onlyHere}>
                  <LineText line={line} />
                  <span class={styles.visuallyHidden}>
                    {" "}
                    ({m.recipe_compare_only_in_this_recipe()})
                  </span>
                </span>
              </Show>
            </li>
          )}
        </For>
      </ul>
    </Show>
  )
}

function Photos(props: { entry: LibraryEntry }) {
  return (
    <ul class={styles.photos}>
      <For each={photoAssets(props.entry.recipe)}>
        {(photo) => (
          <li class={styles.photo}>
            <img
              class={styles.thumbnail}
              src={assetURL(`sha256:${photo.asset.sha256}`, { recipeID: props.entry.id }, "thumb")}
              onError={(event) => fallbackToOriginalImage(event.currentTarget)}
              alt={photo.label}
              loading="lazy"
              decoding="async"
            />
            <Show when={photo.main}>
              <span class={styles.photoLabel}>{m.photos_cover_photo()}</span>
            </Show>
          </li>
        )}
      </For>
    </ul>
  )
}

function AreaReading(props: { entry: LibraryEntry; other?: LibraryEntry; area: RecipeArea }) {
  const readings = createMemo(() => areaReading(props.entry.recipe, props.area))
  const others = createMemo(() =>
    props.other
      ? areaReading(props.other.recipe, props.area).flatMap((reading) => reading.lines)
      : undefined,
  )
  return (
    <Show
      when={readings().length > 0}
      fallback={<p class={styles.missing}>{m.recipe_compare_this_recipe_has_none()}</p>}
    >
      <Show when={props.area !== "photos"} fallback={<Photos entry={props.entry} />}>
        <For each={readings()}>
          {(reading) => (
            <div class={styles.reading}>
              <Show when={readings().length > 1}>
                <p class={styles.readingLabel}>{reading.label}</p>
              </Show>
              <Lines lines={reading.lines} others={others()} />
            </div>
          )}
        </For>
      </Show>
    </Show>
  )
}

function verdictText(evidence: DuplicateEvidence): string {
  switch (evidence.verdict) {
    case "likely":
      return m.recipe_compare_verdict_likely()
    case "possible":
      return m.recipe_compare_verdict_possible()
    default:
      return m.recipe_compare_verdict_different()
  }
}

function evidenceText(evidence: DuplicateEvidence): string {
  return [
    evidence.sameSource === true ? m.recipe_compare_same_source() : "",
    evidence.sameSource === false ? m.recipe_compare_different_sources() : "",
    evidence.sameTitle ? m.recipe_compare_same_title() : "",
    evidence.totalIngredients > 0
      ? m.recipe_compare_ingredients_in_common({
          shared: formatNumber(evidence.sharedIngredients),
          total: formatNumber(evidence.totalIngredients),
        })
      : "",
  ]
    .filter(Boolean)
    .join(" · ")
}

export function RecipeComparison(props: {
  left: LibraryEntry
  right: LibraryEntry
  onCombine: () => void
  onKeepBoth: () => void
  toolbar?: JSX.Element
}) {
  const areas = createMemo(() =>
    comparableAreas(props.left.recipe, props.right.recipe).filter((area) => area !== "title"),
  )
  const differing = createMemo(() =>
    areas().filter((area) => !areaMatches(props.left.recipe, props.right.recipe, area)),
  )
  const evidence = createMemo(() => duplicateEvidence(props.left.recipe, props.right.recipe))
  return (
    <section class={styles.panel} aria-labelledby="recipe-comparison-title">
      {props.toolbar}
      <h1 id="recipe-comparison-title" class={shell.title} tabIndex={-1}>
        {m.recipe_compare_compare_two_recipes()}
      </h1>
      <div class={styles.verdict}>
        <p class={styles.verdictTitle}>{verdictText(evidence())}</p>
        <p class={styles.summary}>
          <Show when={evidenceText(evidence())}>{(text) => <>{text()}. </>}</Show>
          <Show
            when={differing().length > 0}
            fallback={m.recipe_compare_these_two_recipes_read_alike()}
          >
            <Show
              when={differing().length > 3}
              fallback={m.recipe_compare_differs_in({
                areas: isolate(differing().map(areaName).join(", ")),
              })}
            >
              {m.recipe_compare_parts_differ({
                count: formatNumber(differing().length),
                total: formatNumber(areas().length),
              })}
            </Show>
          </Show>{" "}
          {m.recipe_compare_nothing_changes_until_you_combine()}
        </p>
      </div>
      <div class={styles.heads}>
        <div class={styles.head}>
          <h2 class={styles.headTitle}>{props.left.recipe.title}</h2>
          <p class={styles.headNote}>{recipeNote(props.left.recipe)}</p>
        </div>
        <div class={styles.head}>
          <h2 class={styles.headTitle}>{props.right.recipe.title}</h2>
          <p class={styles.headNote}>{recipeNote(props.right.recipe)}</p>
        </div>
      </div>
      <For each={areas()}>
        {(area) => {
          const same = () => areaMatches(props.left.recipe, props.right.recipe, area)
          return (
            <section class={styles.area} aria-labelledby={`compare-area-${area}`}>
              <h3 id={`compare-area-${area}`} class={styles.areaHeading}>
                {areaName(area)}
                <Show when={same()}>
                  {" "}
                  <span class={styles.badgeSame}>{m.recipe_compare_the_same_in_both()}</span>
                </Show>
              </h3>
              <Show
                when={same()}
                fallback={
                  <div class={styles.columns}>
                    <div class={styles.reading}>
                      <p class={styles.columnLabel}>{props.left.recipe.title}</p>
                      <AreaReading entry={props.left} other={props.right} area={area} />
                    </div>
                    <div class={styles.reading}>
                      <p class={styles.columnLabel}>{props.right.recipe.title}</p>
                      <AreaReading entry={props.right} other={props.left} area={area} />
                    </div>
                  </div>
                }
              >
                <div class={styles.agreed}>
                  <AreaReading entry={props.left} area={area} />
                </div>
              </Show>
            </section>
          )
        }}
      </For>
      <div class={styles.actions}>
        <Button
          colorPalette="accent"
          variant={evidence().verdict === "different" ? "outline" : "solid"}
          onClick={props.onCombine}
        >
          {m.recipe_compare_combine_these_recipes()}
        </Button>
        <Button
          variant={evidence().verdict === "different" ? "solid" : "outline"}
          onClick={props.onKeepBoth}
        >
          {m.recipe_compare_keep_both()}
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
    <fieldset class={styles.choices}>
      <legend>{props.name}</legend>
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

function ConflictSide(props: { title: string; lines: string[] }) {
  return (
    <div class={styles.reading}>
      <p class={styles.readingLabel}>{props.title}</p>
      <Lines lines={props.lines} />
    </div>
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

type Side = "left" | "right"

function Option(props: {
  name: string
  checked: boolean
  onSelect: () => void
  title: string
  children?: JSX.Element
}) {
  return (
    <label class={styles.option}>
      <input
        class={styles.optionInput}
        type="radio"
        name={props.name}
        checked={props.checked}
        onChange={() => props.onSelect()}
      />
      <span class={styles.optionBody}>
        <span class={styles.optionTitle}>{props.title}</span>
        {props.children}
      </span>
    </label>
  )
}

/** Merge choices default to the recipe being kept. */
export function RecipeCombine(props: {
  preview: RecipeMergePreview
  survivor: Side
  onSurvivorChange: (survivor: Side) => void
  onMerge: (recipe: RecipeMergePreview["suggested"]) => void
  toolbar?: JSX.Element
  busy?: boolean
}) {
  const [overrides, setOverrides] = createSignal<Partial<Record<RecipeArea, Side>>>({})
  const entry = (side: Side) => (side === "left" ? props.preview.left : props.preview.right)
  const other = (side: Side): Side => (side === "left" ? "right" : "left")
  const groups = createMemo(() => conflictAreas(props.preview.conflicts))
  const sideFor = (area: RecipeArea) => overrides()[area] ?? props.survivor
  const conflict = (path: string) =>
    props.preview.conflicts.find((candidate) => candidate.path === path)!
  const selected = () => {
    const result = structuredClone(props.preview.suggested) as Record<string, unknown>
    for (const group of groups()) {
      const source = entry(sideFor(group.area)).recipe as Record<string, unknown>
      for (const path of group.paths) result[path] = structuredClone(source[path])
    }
    return result as RecipeMergePreview["suggested"]
  }
  const keptTitle = () =>
    (selected().title as string | undefined) ?? entry(props.survivor).recipe.title
  const taken = () => groups().filter((group) => sideFor(group.area) !== props.survivor)
  const choose = (area: RecipeArea, side: Side) =>
    setOverrides((current) => {
      const next = { ...current }
      if (side === props.survivor) delete next[area]
      else next[area] = side
      return next
    })
  const keep = (side: Side) => {
    setOverrides({})
    props.onSurvivorChange(side)
  }
  const sideLines = (side: Side, path: string) =>
    conflictLines(entry(side).recipe, path, conflict(path)[side])
  return (
    <section class={styles.panel} aria-labelledby="recipe-combine-title">
      {props.toolbar}
      <h1 id="recipe-combine-title" class={shell.title} tabIndex={-1}>
        {m.recipe_compare_combine_into_one_recipe()}
      </h1>
      <fieldset class={styles.fieldset}>
        <legend class={styles.legend}>
          <h2 class={styles.legendTitle}>{m.recipe_compare_which_recipe_do_you_keep()}</h2>
          <span class={styles.summary}>{m.recipe_compare_kept_recipe_keeps_its_place()}</span>
        </legend>
        <div class={styles.options}>
          <For each={["left", "right"] as Side[]}>
            {(side) => (
              <Option
                name="survivor"
                checked={props.survivor === side}
                onSelect={() => keep(side)}
                title={entry(side).recipe.title}
              >
                <span class={styles.headNote}>{recipeNote(entry(side).recipe)}</span>
              </Option>
            )}
          </For>
        </div>
      </fieldset>
      <Show when={groups().length > 0}>
        <p class={styles.summary}>{m.recipe_compare_whole_parts_only()}</p>
        <For each={groups()}>
          {(group) => (
            <fieldset class={`${styles.fieldset} ${styles.area}`}>
              <legend class={styles.legend}>
                <h2 class={styles.legendTitle}>{areaName(group.area)}</h2>
              </legend>
              <div class={styles.options}>
                <For each={["left", "right"] as Side[]}>
                  {(side) => (
                    <Option
                      name={`part-${group.area}`}
                      checked={sideFor(group.area) === side}
                      onSelect={() => choose(group.area, side)}
                      title={entry(side).recipe.title}
                    >
                      <Show when={group.area !== "title"}>
                        <Show
                          when={group.area !== "photos"}
                          fallback={<Photos entry={entry(side)} />}
                        >
                          <For each={group.paths}>
                            {(path) => (
                              <Lines
                                lines={sideLines(side, path)}
                                others={sideLines(other(side), path)}
                              />
                            )}
                          </For>
                        </Show>
                      </Show>
                    </Option>
                  )}
                </For>
              </div>
            </fieldset>
          )}
        </For>
      </Show>
      <div class={styles.outcome}>
        <h2 class={styles.outcomeTitle}>{m.recipe_compare_what_combining_does()}</h2>
        <ul class={styles.outcomeList}>
          <li>{m.recipe_compare_the_library_keeps({ value1: isolate(keptTitle()) })}</li>
          <For each={taken()}>
            {(group) => (
              <li>
                {m.recipe_compare_part_comes_from({
                  value1: isolate(areaName(group.area)),
                  value2: isolate(entry(other(props.survivor)).recipe.title),
                })}
              </li>
            )}
          </For>
          <li>
            {m.recipe_compare_the_other_goes_to_trash({
              value1: isolate(entry(other(props.survivor)).recipe.title),
            })}
          </li>
        </ul>
        <Disclosure label={m.recipe_compare_the_combined_recipe()}>
          <RecipeArticle recipe={selected()} readOnly />
        </Disclosure>
      </div>
      <div class={styles.actions}>
        <Button
          colorPalette="accent"
          disabled={props.busy}
          onClick={() => props.onMerge(selected())}
        >
          {props.busy ? m.recipe_compare_merging() : m.recipe_compare_combine_recipes()}
        </Button>
        <Show when={taken().length > 0}>
          <Button variant="outline" onClick={() => setOverrides({})}>
            {m.recipe_compare_use_kept_recipe_for_every_part()}
          </Button>
        </Show>
      </div>
    </section>
  )
}
