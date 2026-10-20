import { Link } from "@tanstack/solid-router"
import { For, Show, createEffect, untrack } from "solid-js"
import { css } from "styled-system/css"

import { ImportMenu } from "@/components/import-menu"
import { RecipeCard } from "@/components/recipe-card"
import { Loading, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { emptyFilters } from "@/lib/library-query"

import { m } from "../../lib/messages.ts"
import type { LibraryQuery } from "./create-library-query"
import type { LibrarySelection } from "./create-library-selection"
import { type UpdateLibrarySearch, filterSearch } from "./library-search"

/** The two ways to begin, offered where a library has nothing else to show. */
export interface StartChoices {
  busy: boolean
  onFiles: () => void
  onURL: () => void
  onText: () => void
  onHistory: () => void
}

export function LibraryResults(props: {
  query: LibraryQuery
  selection: LibrarySelection
  update: UpdateLibrarySearch
  start: StartChoices
}) {
  const { page, refetch, entries, narrowed } = untrack(() => props.query)
  const { selecting, isSelected, toggleSelected } = untrack(() => props.selection)
  let region: HTMLElement | undefined
  createEffect(() => {
    const target = props.query.focusOffset()
    if (
      target === undefined ||
      page.state !== "ready" ||
      target < props.query.shownOffset() ||
      target >= props.query.shownOffset() + entries().length
    )
      return
    props.query.setFocusOffset(undefined)
    if (document.activeElement?.matches("input, textarea, [contenteditable=true]")) return
    if (target === props.query.shownOffset()) {
      region?.focus({ preventScroll: true })
      region?.scrollIntoView({ block: "start", behavior: "instant" })
    } else {
      const card =
        region?.querySelectorAll<HTMLElement>("li > a, li > button")[
          target - props.query.shownOffset()
        ]
      card?.focus({ preventScroll: true })
      card?.scrollIntoView({ block: "nearest", behavior: "instant" })
    }
  })
  return (
    <section
      ref={(element) => {
        region = element
      }}
      id="library-results"
      class={styles.results}
      tabIndex={-1}
      aria-label={m.library_page_recipes()}
      aria-busy={props.query.loading()}
    >
      <ResourceState
        resource={page}
        loading={<Loading>{m.library_results_loading_recipes()}</Loading>}
        fallback={m.recipes_the_recipe_library_could_not_be_loaded()}
        onRetry={() => void refetch()}
        isEmpty={() => entries().length === 0}
        empty={
          <Show when={narrowed()} fallback={<StartLibrary start={props.start} />}>
            <div class={styles.empty}>
              <p class={styles.emptyMessage}>{m.library_results_nothing_matches_that_search()}</p>
              <div class={styles.emptyAction}>
                <Button
                  variant="outline"
                  colorPalette="accent"
                  onClick={() =>
                    props.update({
                      q: undefined,
                      view: undefined,
                      sort: undefined,
                      ...filterSearch(emptyFilters),
                    })
                  }
                >
                  {m.library_results_show_all_recipes()}
                </Button>
              </div>
            </div>
          </Show>
        }
      >
        {() => (
          <ul class={styles.grid}>
            <For each={entries()}>
              {(entry, index) => (
                <li>
                  <RecipeCard
                    recipe={entry}
                    priority={index() === 0}
                    selecting={selecting()}
                    selected={isSelected(entry.id)}
                    onSelect={toggleSelected}
                  />
                </li>
              )}
            </For>
          </ul>
        )}
      </ResourceState>
    </section>
  )
}

/** An empty library offers writing a recipe and importing, at equal weight. */
function StartLibrary(props: { start: StartChoices }) {
  return (
    <section class={styles.start} aria-labelledby="library-start">
      <h2 id="library-start" class={styles.startTitle}>
        {m.library_results_start_title()}
      </h2>
      <p class={styles.startHint}>{m.library_results_start_hint()}</p>
      <div class={styles.startActions}>
        <Link class={buttonLink({ colorPalette: "accent" })} to="/recipes/new">
          {m.library_results_start_write()}
        </Link>
        <ImportMenu
          busy={props.start.busy}
          label={m.library_results_start_import()}
          onFiles={props.start.onFiles}
          onURL={props.start.onURL}
          onText={props.start.onText}
          onHistory={props.start.onHistory}
        />
      </div>
    </section>
  )
}

const styles = {
  results: css({ scrollMarginBlockStart: "5", outline: "none" }),
  grid: css({
    display: "grid",
    "& > li": { minWidth: "0" },
    gap: { base: "5", md: "7" },
    gridTemplateColumns: {
      base: "repeat(auto-fill, minmax(min(100%, 9.5rem), 1fr))",
      sm: "repeat(auto-fill, minmax(min(100%, 13rem), 1fr))",
    },
    listStyle: "none",
    margin: "0",
    marginTop: "7",
    padding: "0",
  }),
  empty: css({
    color: "fg.muted",
    fontFamily: "editorial",
    fontSize: "lg",
    paddingBlock: "16",
    textAlign: "center",
  }),
  emptyMessage: css({ margin: "0" }),
  emptyAction: css({ marginTop: "5" }),
  start: css({
    marginInline: "auto",
    maxWidth: "34rem",
    paddingBlock: { base: "12", md: "16" },
    textAlign: "center",
  }),
  startTitle: css({
    fontFamily: "editorial",
    fontSize: { base: "2xl", md: "3xl" },
    fontWeight: "normal",
    margin: "0",
  }),
  startHint: css({ color: "fg.muted", margin: "0", marginBlockStart: "3" }),
  startActions: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    justifyContent: "center",
    marginTop: "7",
  }),
}
