import { Link } from "@tanstack/solid-router"
import { For, type JSX, Show } from "solid-js"
import { css, cva } from "styled-system/css"

import type { Recipe } from "@/api/client"
import type { ImportPreviewItem } from "@/api/import-previews"
import { ImportThumbnail } from "@/components/import-thumbnail"
import { RecipeArticle } from "@/components/recipe-article"
import { Callout } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Disclosure } from "@/components/ui/disclosure"
import { Select } from "@/components/ui/select"
import {
  type CandidateStanding,
  candidateFacts,
  candidateStanding,
  matchFacts,
  standingLabels,
} from "@/lib/import-candidate"
import { type PreviewNotice, previewDiagnostics, previewNotices } from "@/lib/import-diagnostics"
import { safeSourceURL } from "@/lib/source-url"

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
  // Prevent the recipe article from shrinking or receiving duplicate page padding.
  sole: css({ display: "block", marginInline: { base: "-5", md: "-8", lg: "-10" } }),
  soleNotes: css({
    display: "grid",
    gap: "3",
    marginBottom: "2",
    marginInline: "auto",
    maxWidth: "84rem",
    paddingInline: { base: "5", md: "8", lg: "10" },
    width: "100%",
  }),
  provenance: css({
    alignItems: "center",
    color: "fg.muted",
    columnGap: "3",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    margin: "0",
    rowGap: "1",
  }),
  link: css({
    color: "accent.default",
    textDecoration: "underline",
    textUnderlineOffset: "3px",
  }),
  noticeList: css({ display: "grid", gap: "1", margin: "0", paddingInlineStart: "5" }),
  noticeLink: css({ marginInlineStart: "2" }),
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
  caption: css({ whiteSpace: "pre-wrap", overflowWrap: "anywhere", lineHeight: "1.7" }),
  checkbox: css({ marginTop: "0.5" }),
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
  matches: css({
    background: "bg.subtle",
    borderRadius: "l1",
    display: "grid",
    gap: "2",
    marginTop: "3",
    padding: "3",
  }),
  matchesSole: css({ marginTop: "0" }),
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
  photo: css({ background: "bg.subtle", overflow: "hidden" }),
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
  decisions?: JSX.Element
}

export function ImportCandidate(props: ImportCandidateProps) {
  return (
    <Show when={props.sole} fallback={<CandidateRow {...props} />}>
      <SoleCandidate {...props} />
    </Show>
  )
}

function SoleCandidate(props: ImportCandidateProps) {
  const sorted = () => previewNotices(props.item, { measurementsShown: Boolean(props.decisions) })
  const post = () =>
    sorted().notices.some((notice) => notice.post) ? undefined : safeSourceURL(sorted().post)
  const hasLead = () =>
    Boolean(sorted().provenance || post() || props.item.source.extracted_text) ||
    Boolean(props.item.source.can_retain)
  return (
    <li class={styles.sole}>
      <div class={styles.soleNotes}>
        <Show when={hasLead()}>
          <p class={styles.provenance}>
            <Show when={sorted().provenance}>{(text) => <span>{text()}</span>}</Show>
            <Show when={post()}>
              {(href) => (
                <a class={styles.link} href={href()} target="_blank" rel="noreferrer">
                  {m.import_notice_view_post()}
                </a>
              )}
            </Show>
            <Show when={props.item.source.extracted_text}>
              <Button size="xs" variant="plain" onClick={() => props.onEditText()}>
                {m.import_candidate_edit_recipe_text()}
              </Button>
            </Show>
          </p>
          <RetainSource {...props} />
        </Show>
        <Notices notices={sorted().notices} />
        {props.decisions}
        <Show when={props.item.analysis.matches.length > 0}>
          <Matches item={props.item} sole onCompare={props.onCompare} />
        </Show>
      </div>
      <PreviewArticle
        item={props.item}
        footer={<Details details={sorted().details.map((detail) => detail.message)} />}
      />
    </li>
  )
}

function Notices(props: { notices: PreviewNotice[] }) {
  const tone = () =>
    props.notices.some((notice) => notice.tone === "danger") ? "danger" : "warning"
  const line = (notice: PreviewNotice) => (
    <>
      {notice.text}
      <Show when={safeSourceURL(notice.post)}>
        {(href) => (
          <a
            class={`${styles.link} ${styles.noticeLink}`}
            href={href()}
            target="_blank"
            rel="noreferrer"
          >
            {m.import_notice_view_post()}
          </a>
        )}
      </Show>
    </>
  )
  return (
    <Show when={props.notices.length > 0}>
      <Show
        when={props.notices.length > 1}
        fallback={<Callout tone={tone()}>{line(props.notices[0])}</Callout>}
      >
        <Callout tone={tone()} title={m.import_notice_check_title()}>
          <ul class={styles.noticeList}>
            <For each={props.notices}>{(notice) => <li>{line(notice)}</li>}</For>
          </ul>
        </Callout>
      </Show>
    </Show>
  )
}

function Details(props: { details: string[] }) {
  return (
    <Show when={props.details.length > 0}>
      <Disclosure class={styles.details} label={m.import_job_results_import_details()}>
        <ul class={styles.notes}>
          <For each={props.details}>{(detail) => <li>{detail}</li>}</For>
        </ul>
      </Disclosure>
    </Show>
  )
}

function RetainSource(props: ImportCandidateProps) {
  return (
    <Show when={props.item.source.can_retain}>
      <label class={styles.retention}>
        <Checkbox
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
  )
}

function Matches(props: {
  item: ImportPreviewItem
  sole?: boolean
  onCompare?: (leftEntryID: string, rightEntryID: string) => void
}) {
  return (
    <div class={props.sole ? `${styles.matches} ${styles.matchesSole}` : styles.matches}>
      <p class={styles.matchesTitle}>
        {props.item.analysis.status === "definite-match"
          ? m.import_candidate_already_here()
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
  )
}

function CandidateRow(props: ImportCandidateProps) {
  const standing = () => candidateStanding(props.item)
  const diagnostics = () => previewDiagnostics(props.item)
  const notices = () => previewNotices(props.item)
  const title = () =>
    props.item.recipe.title || props.item.source.filename || m.import_candidate_unreadable_recipe()
  let shift = false

  return (
    <li class={styles.row}>
      <div class={styles.heading}>
        <Checkbox
          class={styles.checkbox}
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
        <ImportThumbnail class={styles.thumbnail} source={props.item.assets.preview_url} />
        <div class={styles.body}>
          <h3 class={styles.title}>{title()}</h3>
          <p class={standingStyle({ standing: standing() })}>{standingLabels[standing()]}</p>
          <p class={styles.source}>
            {props.item.source.url
              ? new URL(props.item.source.url).hostname
              : props.item.source.format_name}
            {!props.hideFilename && props.item.source.filename
              ? ` · ${props.item.source.filename}`
              : ""}
          </p>
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
          <RetainSource {...props} />
          <Show when={props.item.source.extracted_text}>
            <Button size="xs" variant="plain" onClick={() => props.onEditText?.()}>
              {m.import_candidate_edit_recipe_text()}
            </Button>
          </Show>
        </div>
      </div>
      <Show when={diagnostics().attention.length > 0}>
        <div class={styles.notes}>
          <For each={notices().notices}>
            {(notice) => <Callout tone={notice.tone}>{notice.text}</Callout>}
          </For>
        </div>
      </Show>
      <Show when={props.item.analysis.matches.length > 0}>
        <Matches item={props.item} onCompare={props.onCompare} />
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
        <Disclosure class={styles.details} label={m.common_preview_recipe()}>
          <PreviewArticle item={props.item} />
        </Disclosure>
      </Show>
      <Details details={notices().details.map((detail) => detail.message)} />
    </li>
  )
}

function PreviewArticle(props: { item: ImportPreviewItem; footer?: JSX.Element }) {
  const recipe = (): Recipe => {
    const document = props.item.document
    const title = document?.title || props.item.recipe.title
    if (document) return { ...document, title }
    return {
      id: props.item.id,
      schema_version: 1,
      title,
      description: props.item.recipe.description,
      ingredient_sections: [],
      instruction_sections: [],
    }
  }
  const empty = () =>
    !props.item.document &&
    props.item.recipe.ingredient_count === 0 &&
    props.item.recipe.step_count === 0
  return (
    <RecipeArticle
      recipe={recipe()}
      readOnly
      image={
        props.item.assets.preview_url
          ? (className) => (
              <ImportThumbnail
                class={`${className} ${styles.photo}`}
                source={props.item.assets.preview_url}
              />
            )
          : undefined
      }
      footer={
        <>
          <Show when={empty() && props.item.source.extracted_text}>
            <p class={styles.caption} dir="auto">
              {props.item.source.extracted_text}
            </p>
          </Show>
          {props.footer}
        </>
      }
    />
  )
}
