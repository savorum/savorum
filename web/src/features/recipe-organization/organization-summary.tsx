import { Link } from "@tanstack/solid-router"
import { For, Show, createMemo, createResource, createSignal } from "solid-js"
import { css } from "styled-system/css"

import type { BookSummary } from "@/api/client"
import { libraryRecipeTotal } from "@/api/library-total"
import { listFacets, listTags } from "@/api/organization"
import { suggestBooks, lookupBooks, lookupRecipes } from "@/api/selections"
import { LabelSwatch } from "@/components/label-swatch"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { CookingLog } from "@/features/recipe-organization/cooking-log"
import type { OrganizationController } from "@/features/recipe-organization/create-organization-controller"
import { RecipeRating } from "@/features/recipe-organization/organization-bar"
import { OrganizationEditor } from "@/features/recipe-organization/organization-editor"
import { rankLabels } from "@/lib/label-ranking"
import { facetGroups } from "@/lib/library-query"
import type { Rational } from "@/lib/rational"
import { latestUnlessFailed } from "@/lib/settled"

import { m } from "../../lib/messages.ts"

type Filing = {
  name: string
  color?: string
  bookID?: string
  search?: Record<string, string[]>
}

export const OrganizationSummary = (props: {
  entryID: string
  controller: OrganizationController
  scale?: Rational
}) => {
  const [editing, setEditing] = createSignal(false)
  const [tags, { refetch: refetchTags }] = createResource(listTags)
  const [facets, { refetch: refetchFacets }] = createResource(() => listFacets())
  const [total] = createResource(libraryRecipeTotal)
  const [books, { refetch: refetchBooks }] = createResource(
    () => props.entryID,
    async (id) => {
      const entry = (await lookupRecipes([id])).items[0]
      const memberships = (entry?.books ?? []).map((book) => book.id)
      const ids = [
        ...new Set([...memberships, ...(entry?.source_books ?? []).map((book) => book.id)]),
      ]
      const [page, selected] = await Promise.all([suggestBooks(), lookupBooks(ids)])
      return [...selected.items, ...page.items.filter((book) => !ids.includes(book.id))].map(
        (book) => ({
          ...book,
          contains_entry: memberships.includes(book.id),
          entry_count: 0,
          updated_at: "",
          kind: book.kind as BookSummary["kind"],
        }),
      )
    },
  )
  const organization = () => props.controller.value()
  const groups = createMemo(() => {
    const current = organization()
    const filed = [
      {
        label: m.organization_editor_books(),
        entries: (latestUnlessFailed(books) ?? [])
          .filter((book) => book.contains_entry)
          .map((book): Filing => ({ name: book.title, bookID: book.id })),
      },
      {
        label: m.organization_editor_tags(),
        entries: rankLabels(
          (latestUnlessFailed(tags) ?? [])
            .filter((tag) => current?.tag_ids?.includes(tag.id))
            .map((tag) => ({
              name: tag.name,
              kind: "tag",
              recipeCount: tag.recipe_count ?? 0,
              color: tag.color,
              search: { tags: [tag.id] },
            })),
          latestUnlessFailed(total),
        ).map(({ name, color, search }): Filing => ({ name, color, search })),
      },
      ...facetGroups.map((group) => ({
        label: group.label,
        entries: rankLabels(
          (latestUnlessFailed(facets) ?? [])
            .filter((facet) => facet.kind === group.kind && current?.facet_ids?.includes(facet.id))
            .map((facet) => ({
              name: facet.value,
              kind: facet.kind,
              recipeCount: facet.recipe_count ?? 0,
              search: { [group.key]: [facet.id] },
            })),
          latestUnlessFailed(total),
        ).map(({ name, search }): Filing => ({ name, search })),
      })),
    ]
    return filed.filter((group) => group.entries.length > 0)
  })
  return (
    <Show when={organization()}>
      <section class={styles.band} aria-label={m.organization_summary_your_library()}>
        <ChangeError error={props.controller.error()} onReload={props.controller.reload} />
        <div class={styles.utility}>
          <div class={styles.details}>
            <Show when={!groups().length}>
              <span class={styles.note}>{m.organization_summary_not_filed_yet()}</span>
            </Show>
            <dl class={styles.filing} aria-label={m.organization_summary_library_details()}>
              <For each={groups()}>
                {(group) => (
                  <>
                    <dt class={styles.term}>{group.label}</dt>
                    <dd class={styles.values}>
                      <For each={group.entries}>
                        {(entry) =>
                          entry.bookID ? (
                            <Link
                              class={styles.value}
                              to="/books/$bookId"
                              params={{ bookId: entry.bookID }}
                            >
                              {entry.name}
                            </Link>
                          ) : (
                            <Link class={styles.value} to="/" search={entry.search}>
                              <LabelSwatch color={entry.color} size="sm" />
                              {entry.name}
                            </Link>
                          )
                        }
                      </For>
                    </dd>
                  </>
                )}
              </For>
              <dt class={styles.term} aria-hidden="true">
                {m.organization_bar_your_rating()}
              </dt>
              <dd class={styles.values}>
                <RecipeRating controller={props.controller} />
              </dd>
            </dl>
          </div>
          <Button
            class={styles.organize}
            size="sm"
            variant="plain"
            disabled={
              props.controller.saving() ||
              books.loading ||
              tags.loading ||
              facets.loading ||
              Boolean(books.error || tags.error || facets.error)
            }
            onClick={() => setEditing(true)}
          >
            {m.organization_summary_organize()}
          </Button>
        </div>
        <ChangeError
          error={books.error || tags.error || facets.error}
          onReload={() => {
            void refetchBooks()
            void refetchTags()
            void refetchFacets()
          }}
        />
        <Show when={books.error || tags.error || facets.error}>
          <Button
            size="sm"
            variant="plain"
            onClick={() => {
              void refetchBooks()
              void refetchTags()
              void refetchFacets()
            }}
          >
            {m.organization_summary_try_again()}
          </Button>
        </Show>
        <Show when={organization()?.user_state.personal_note}>
          <p class={styles.note}>{organization()?.user_state.personal_note}</p>
        </Show>
        <CookingLog entryID={props.entryID} scale={props.scale} />
        <Show when={editing()}>
          <OrganizationEditor
            controller={props.controller}
            tags={latestUnlessFailed(tags) ?? []}
            facets={latestUnlessFailed(facets) ?? []}
            books={latestUnlessFailed(books) ?? []}
            onClose={() => setEditing(false)}
            onSaved={() => void refetchBooks()}
          />
        </Show>
      </section>
    </Show>
  )
}

const styles = {
  band: css({
    borderTopWidth: "1px",
    borderColor: "border.subtle",
    display: "grid",
    gap: "4",
    marginTop: "10",
    paddingTop: "3",
  }),
  utility: css({
    alignItems: "start",
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    justifyContent: "space-between",
    minHeight: "2rem",
  }),
  details: css({
    display: "grid",
    flex: { base: "1 1 100%", md: "1 1 20rem" },
    gap: "2",
    minWidth: "0",
    paddingTop: "1.5",
  }),
  filing: css({
    color: "fg.muted",
    columnGap: "4",
    display: "grid",
    fontSize: "xs",
    gridTemplateColumns: "max-content minmax(0, 1fr)",
    lineHeight: { base: "2", md: "1.55" },
    margin: "0",
    maxWidth: "46rem",
    rowGap: "1",
  }),
  term: css({ fontWeight: "semibold" }),
  values: css({
    columnGap: "3",
    display: "flex",
    flexWrap: "wrap",
    margin: "0",
    minWidth: "0",
    rowGap: "0.5",
  }),
  value: css({
    alignItems: "center",
    borderRadius: "l1",
    display: "inline-flex",
    gap: "1.5",
    textDecoration: "underline",
    textDecorationColor: "border.default",
    textUnderlineOffset: "3px",
    _hover: { color: "accent.default", textDecorationColor: "currentColor" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "2px" },
  }),
  organize: css({ marginInlineStart: { base: "-3", md: "0" } }),
  note: css({
    color: "fg.muted",
    fontSize: "xs",
    maxWidth: "40rem",
  }),
}
