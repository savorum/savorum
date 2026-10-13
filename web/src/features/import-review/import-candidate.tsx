import { Link } from "@tanstack/solid-router"
import { For, Show } from "solid-js"
import { css, cva } from "styled-system/css"

import type { ImportPreviewItem } from "@/api/import-previews"
import { ImportThumbnail } from "@/components/import-thumbnail"
import { Callout } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/disclosure"
import { Select } from "@/components/ui/select"
import {
  type CandidateStanding,
  candidateFacts,
  candidateStanding,
  matchFacts,
  standingLabels,
} from "@/lib/import-candidate"
import { previewDiagnostics } from "@/lib/import-diagnostics"

import { m } from "../../lib/messages.ts"

const standingStyle = cva({
  base: { fontSize: "xs", fontWeight: "semibold", marginTop: "1" },
  variants: {
    standing: {
      ready: { color: "fg.muted" },
      decision: { color: "accent.default" },
      review: { color: "accent.default" },
      blocked: { color: "danger.default" },
      "already-here": { color: "fg.muted" },
    } satisfies Record<CandidateStanding, object>,
  },
})

const styles = {
  singleRow: css({ paddingBlock: "2" }),
  singleTitle: css({ fontFamily: "editorial", fontSize: "2xl", fontWeight: "normal", margin: "0" }),
  row: css({
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    padding: "4",
  }),
  heading: css({
    alignItems: "start",
    display: "grid",
    gap: "3",
    gridTemplateColumns: "auto 5rem minmax(0, 1fr)",
  }),
  headingSingle: css({ display: "flex", gap: "3", alignItems: "start" }),
  caption: css({ whiteSpace: "pre-wrap", overflowWrap: "anywhere", lineHeight: "1.7" }),
  headingAlone: css({
    alignItems: "start",
    display: "grid",
    gap: "3",
    gridTemplateColumns: "5rem minmax(0, 1fr)",
  }),
  checkbox: css({ accentColor: "accent.default", height: "4", marginTop: "1", width: "4" }),
  thumbnail: css({
    alignItems: "center",
    aspectRatio: "4 / 3",
    background: "bg.subtle",
    borderRadius: "l1",
    color: "fg.muted",
    display: "flex",
    fontSize: "xs",
    justifyContent: "center",
    objectFit: "cover",
    overflow: "hidden",
    width: "5rem",
  }),
  body: css({ minWidth: "0" }),
  title: css({ fontSize: "md", fontWeight: "semibold" }),
  source: css({ color: "fg.muted", fontSize: "xs", marginTop: "1" }),
  retention: css({
    alignItems: "start",
    color: "fg.default",
    display: "flex",
    fontSize: "sm",
    gap: "2",
    marginTop: "3",
  }),
  retentionHint: css({
    color: "fg.muted",
    fontSize: "xs",
    margin: "0",
    marginTop: "1",
    marginInlineStart: "6",
  }),
  // A definition list of separate facts.
  facts: css({
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "xs",
    rowGap: "1",
    columnGap: "4",
    margin: "0",
    marginTop: "2",
  }),
  fact: css({ display: "flex", gap: "1" }),
  // Muted colour rather than opacity, which fails 4.5:1 contrast at 12px.
  factLabel: css({ color: "fg.muted" }),
  factValue: css({ color: "fg.default" }),
  notes: css({
    display: "grid",
    gap: "1",
    listStyle: "none",
    margin: "0",
    marginTop: "3",
    padding: "0",
  }),
  note: css({
    borderInlineStartWidth: "2px",
    color: "fg.muted",
    fontSize: "sm",
    lineHeight: "1.5",
    paddingInlineStart: "3",
  }),
  noteReview: css({ borderColor: "accent.default" }),
  noteProblem: css({ borderColor: "danger.default", color: "danger.default" }),
  noteQuiet: css({ borderColor: "border.subtle" }),
  matches: css({
    background: "bg.subtle",
    borderRadius: "l1",
    display: "grid",
    gap: "2",
    marginTop: "3",
    padding: "3",
  }),
  matchesTitle: css({ fontSize: "sm", fontWeight: "semibold", margin: "0" }),
  matchList: css({ display: "grid", gap: "1", listStyle: "none", margin: "0", padding: "0" }),
  match: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    rowGap: "2",
    columnGap: "3",
    justifyContent: "space-between",
    paddingBlock: "1",
  }),
  matchBody: css({ display: "grid", gap: "0.5", minWidth: "0" }),
  matchLink: css({ color: "accent.default", fontSize: "sm", fontWeight: "semibold" }),
  matchFacts: css({
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "xs",
    rowGap: "1",
    columnGap: "3",
    margin: "0",
    minWidth: "0",
  }),
  matchFact: css({ minWidth: "0", overflowWrap: "anywhere" }),
  resolution: css({ display: "grid", gap: "1", marginTop: "3", maxWidth: "22rem" }),
  resolutionLabel: css({ color: "fg.muted", fontSize: "xs", fontWeight: "semibold" }),
  details: css({ marginTop: "3" }),
  detailsSummary: css({ color: "accent.default", cursor: "pointer", fontSize: "sm" }),
  recipe: css({ display: "grid", gap: "5", marginTop: "4", maxWidth: "58rem" }),
  hero: css({ borderRadius: "l2", maxHeight: "24rem", objectFit: "cover", width: "100%" }),
  heading2: css({ fontFamily: "editorial", fontSize: "lg" }),
  section: css({ display: "grid", gap: "1" }),
  list: css({ margin: "0", marginTop: "1", paddingInlineStart: "5" }),
}

export type ImportChoice = {
  action: "separate" | "update" | "skip"
  target_entry_id?: string
}

export interface ImportCandidateProps {
  item: ImportPreviewItem
  selected: boolean
  choice?: ImportChoice
  onSelect: (selected: boolean, range: boolean) => void
  onChoose: (choice: ImportChoice) => void
  retainSource: boolean
  onRetainSource: (retained: boolean) => void
  onEditText: () => void
  hideFilename?: boolean
  /** Whether this is the only recipe under review. */
  sole?: boolean
  onCompare?: (leftEntryID: string, rightEntryID: string) => void
}

/** One recipe in a review queue, as every import surface presents it. */
export function ImportCandidate(props: ImportCandidateProps) {
  const standing = () => candidateStanding(props.item)
  const diagnostics = () => previewDiagnostics(props.item)
  /**
   * Whether this is a sole recipe with no possible duplicate, which needs no
   * selection checkbox.
   */
  const implied = () => Boolean(props.sole) && props.item.analysis.matches.length === 0
  const title = () =>
    props.item.recipe.title || props.item.source.filename || m.import_candidate_unreadable_recipe()
  let shift = false

  return (
    <li class={props.sole ? styles.singleRow : styles.row}>
      <div
        class={props.sole ? styles.headingSingle : implied() ? styles.headingAlone : styles.heading}
      >
        <Show when={!props.sole && !implied()}>
          <input
            class={styles.checkbox}
            type="checkbox"
            aria-label={m.import_candidate_import({ value1: title() })}
            checked={props.selected}
            disabled={!props.item.can_commit}
            onPointerDown={(event) => {
              shift = event.shiftKey
            }}
            onInput={(event) => {
              props.onSelect(event.currentTarget.checked, shift)
              shift = false
            }}
          />
        </Show>
        <Show when={!props.sole}>
          <ImportThumbnail class={styles.thumbnail} source={props.item.assets.preview_url} />
        </Show>
        <div class={styles.body}>
          <h3 class={props.sole ? styles.singleTitle : styles.title}>{title()}</h3>
          <Show when={!props.sole}>
            <p class={standingStyle({ standing: standing() })}>{standingLabels[standing()]}</p>
          </Show>
          <p class={styles.source}>
            {props.item.source.url
              ? new URL(props.item.source.url).hostname
              : props.item.source.format_name}
            {!props.hideFilename && props.item.source.filename
              ? ` · ${props.item.source.filename}`
              : ""}
          </p>
          <Show
            when={
              !props.sole ||
              props.item.recipe.ingredient_count > 0 ||
              props.item.recipe.step_count > 0
            }
          >
            <dl class={styles.facts}>
              <For each={candidateFacts(props.item)}>
                {(fact) => (
                  <div class={styles.fact}>
                    <dt class={styles.factLabel}>{fact.label}</dt>
                    <dd class={styles.factValue}>{fact.value}</dd>
                  </div>
                )}
              </For>
            </dl>
          </Show>
          <Show when={props.item.source.can_retain}>
            <label class={styles.retention}>
              <input
                type="checkbox"
                aria-label={m.import_candidate_keep_original({
                  value1: props.item.source.filename || m.import_candidate_source_file(),
                })}
                checked={props.retainSource}
                onChange={(event) => props.onRetainSource(event.currentTarget.checked)}
              />
              <span>
                {" "}
                {m.import_candidate_keep_the_original_file()}{" "}
                <span class={styles.retentionHint}>
                  {m.import_candidate_optional_available_in_recipe_history()}
                </span>
              </span>
            </label>
          </Show>
          <Show when={props.item.source.extracted_text}>
            <Button size="xs" variant="plain" onClick={() => props.onEditText?.()}>
              {" "}
              {m.import_candidate_edit_recipe_text()}{" "}
            </Button>
          </Show>
        </div>
      </div>

      <Show when={diagnostics().attention.length > 0}>
        <div class={styles.notes}>
          <For each={diagnostics().attention}>
            {(diagnostic) => (
              <Callout tone={diagnostic.class === "blocking" ? "danger" : "warning"}>
                {diagnostic.message}
              </Callout>
            )}
          </For>
        </div>
      </Show>
      <Show when={props.item.analysis.matches.length > 0}>
        <div class={styles.matches}>
          <p class={styles.matchesTitle}>
            {props.item.analysis.status === "definite-match"
              ? m.import_candidate_same_source_already_in_the_library()
              : props.item.analysis.status === "likely-duplicate"
                ? m.import_candidate_possible_duplicate()
                : m.import_candidate_several_source_matches()}
          </p>
          <ul class={styles.matchList}>
            <For each={props.item.analysis.matches}>
              {(match, index) => (
                <li class={styles.match}>
                  <div class={styles.matchBody}>
                    <Link
                      class={styles.matchLink}
                      to="/recipes/$recipeId"
                      params={{ recipeId: match.entry_id }}
                    >
                      {match.title}
                    </Link>
                    <p class={styles.matchFacts}>
                      <For each={matchFacts(match)}>
                        {(fact) => <span class={styles.matchFact}>{fact}</span>}
                      </For>
                    </p>
                  </div>
                  <Show when={index() > 0 && props.onCompare}>
                    <Button
                      size="xs"
                      variant="outline"
                      aria-label={m.import_candidate_compare_with({
                        value1: props.item.analysis.matches[0].title,
                        value2: match.title,
                      })}
                      onClick={() =>
                        props.onCompare?.(props.item.analysis.matches[0].entry_id, match.entry_id)
                      }
                    >
                      {m.import_candidate_compare()}
                    </Button>
                  </Show>
                </li>
              )}
            </For>
          </ul>
        </div>
      </Show>

      <Show when={props.item.analysis.matches.length > 0 && props.item.can_commit}>
        <label class={styles.resolution}>
          <span class={styles.resolutionLabel}>
            {m.import_candidate_when_this_recipe_is_selected()}
          </span>
          <Select
            aria-label={m.import_candidate_reconcile({ value1: props.item.recipe.title })}
            value={props.choice?.action ?? "separate"}
            onValueChange={(value) => {
              const action = value as ImportChoice["action"]
              const target = props.item.analysis.matches[0]?.entry_id
              props.onSelect(true, false)
              props.onChoose({
                action,
                target_entry_id: action === "separate" ? undefined : target,
              })
            }}
            items={[
              {
                value: "separate",
                label: m.import_candidate_import_as_a_separate_recipe(),
              },
              ...(props.item.analysis.status === "definite-match"
                ? [
                    {
                      value: "update",
                      label: m.import_candidate_update_the_existing_recipe(),
                    },
                  ]
                : []),
              { value: "skip", label: m.import_candidate_keep_the_existing_recipe() },
            ]}
          />
        </label>
      </Show>

      <Show when={props.item.can_commit}>
        <Show
          when={props.sole}
          fallback={
            <Disclosure class={styles.details} label={m.common_preview_recipe()}>
              <CandidateRecipe item={props.item} />
            </Disclosure>
          }
        >
          <CandidateRecipe item={props.item} />
        </Show>
      </Show>
      <Show when={diagnostics().details.length > 0}>
        <Disclosure class={styles.details} label={m.import_job_results_import_details()}>
          <ul class={styles.notes}>
            <For each={diagnostics().details}>{(diagnostic) => <li>{diagnostic.message}</li>}</For>
          </ul>
        </Disclosure>
      </Show>
    </li>
  )
}

function CandidateRecipe(props: { item: ImportPreviewItem }) {
  return (
    <div class={styles.recipe}>
      <Show when={props.item.assets.preview_url}>
        <ImportThumbnail class={styles.hero} source={props.item.assets.preview_url} />
      </Show>
      <Show
        when={
          !props.item.recipe.ingredient_count &&
          !props.item.recipe.step_count &&
          props.item.source.extracted_text
        }
      >
        <p class={styles.caption}>{props.item.source.extracted_text}</p>
      </Show>
      <Show when={props.item.recipe.description}>
        <section>
          <h4 class={styles.heading2}>{m.import_candidate_description()}</h4>
          <p>{props.item.recipe.description}</p>
        </section>
      </Show>
      <Show when={props.item.recipe.yield}>
        <section>
          <h4 class={styles.heading2}>{m.import_candidate_yield()}</h4>
          <p>{props.item.recipe.yield}</p>
        </section>
      </Show>
      <Show when={(props.item.recipe.ingredients?.length ?? 0) > 0}>
        <section>
          <h4 class={styles.heading2}>{m.import_candidate_ingredients_label()}</h4>
          <For each={props.item.recipe.ingredients ?? []}>
            {(section) => (
              <div class={styles.section}>
                <Show when={section.title}>
                  <strong>{section.title}</strong>
                </Show>
                <ul class={styles.list}>
                  <For each={section.items}>{(ingredient) => <li>{ingredient}</li>}</For>
                </ul>
              </div>
            )}
          </For>
        </section>
      </Show>
      <Show when={(props.item.recipe.instructions?.length ?? 0) > 0}>
        <section>
          <h4 class={styles.heading2}>{m.import_candidate_directions()}</h4>
          <For each={props.item.recipe.instructions ?? []}>
            {(section) => (
              <div class={styles.section}>
                <Show when={section.title}>
                  <strong>{section.title}</strong>
                </Show>
                <ol class={styles.list}>
                  <For each={section.items}>{(step) => <li>{step}</li>}</For>
                </ol>
              </div>
            )}
          </For>
        </section>
      </Show>
    </div>
  )
}
