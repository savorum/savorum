import { For, Show, createMemo, createResource, createSignal } from "solid-js"
import { css } from "styled-system/css"

import type { BookSummary } from "@/api/client"
import { listFacets, listTags } from "@/api/organization"
import { suggestBooks, lookupBooks, lookupRecipes } from "@/api/selections"
import { LabelSwatch } from "@/components/label-swatch"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/disclosure"
import { CookingLog } from "@/features/recipe-organization/cooking-log"
import type { OrganizationController } from "@/features/recipe-organization/create-organization-controller"
import { RecipeRating } from "@/features/recipe-organization/organization-bar"
import { OrganizationEditor } from "@/features/recipe-organization/organization-editor"
import { facetGroups } from "@/lib/library-query"
import { formatNumber } from "@/lib/locale-format"
import type { Rational } from "@/lib/rational"

import { m } from "../../lib/messages.ts"

/** What a category is called where it is shown beside books and tags. */
const facetKind = (kind: string) => facetGroups.find((group) => group.kind === kind)?.label

export const OrganizationSummary = (props: {
  entryID: string
  controller: OrganizationController
  scale?: Rational
}) => {
  const [editing, setEditing] = createSignal(false)
  const [tags, { refetch: refetchTags }] = createResource(listTags)
  const [facets, { refetch: refetchFacets }] = createResource(() => listFacets())
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
  /** Everything this recipe has been filed under, including categories. */
  const labels = createMemo(() => {
    const staticCurrent = organization()
    return [
      ...(books.latest ?? [])
        .filter((book) => book.contains_entry)
        .map((book) => ({
          name: book.title,
          kind: m.organization_label_book(),
          color: undefined as string | undefined,
        })),
      ...(tags.latest ?? [])
        .filter((tag) => staticCurrent?.tag_ids?.includes(tag.id))
        .map((tag) => ({ name: tag.name, kind: m.organization_label_tag(), color: tag.color })),
      ...(facets.latest ?? [])
        .filter((facet) => staticCurrent?.facet_ids?.includes(facet.id))
        .map((facet) => ({ name: facet.value, kind: facetKind(facet.kind), color: undefined })),
    ]
  })
  return (
    <Show when={organization()}>
      <section class={styles.band} aria-label={m.organization_summary_your_library()}>
        <ChangeError error={props.controller.error()} onReload={props.controller.reload} />
        <div class={styles.utility}>
          <div class={styles.summary} aria-label={m.organization_summary_library_details()}>
            <Show
              when={labels().length}
              fallback={<span class={styles.note}>{m.organization_summary_not_filed_yet()}</span>}
            >
              <For each={labels().slice(0, 6)}>
                {(label) => (
                  <span class={styles.chip} title={label.kind}>
                    <LabelSwatch color={label.color} size="sm" />
                    {label.name}
                  </span>
                )}
              </For>
              <Show when={labels().length > 6}>
                <Disclosure
                  label={m.organization_summary_more({
                    value1: formatNumber(labels().length - 6),
                  })}
                >
                  <div
                    class={css({
                      maxHeight: "12rem",
                      overflowY: "auto",
                      display: "flex",
                      flexWrap: "wrap",
                      gap: "2",
                      paddingBlock: "2",
                    })}
                  >
                    <For each={labels().slice(6)}>
                      {(label) => (
                        <span class={styles.chip} title={label.kind}>
                          <LabelSwatch color={label.color} size="sm" />
                          {label.name}
                        </span>
                      )}
                    </For>
                  </div>
                </Disclosure>
              </Show>
            </Show>
          </div>
          <Button
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
        <RecipeRating controller={props.controller} />
        <CookingLog entryID={props.entryID} scale={props.scale} />
        <Show when={editing()}>
          <OrganizationEditor
            controller={props.controller}
            tags={tags.latest ?? []}
            facets={facets.latest ?? []}
            books={books.latest ?? []}
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
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    minHeight: "2rem",
  }),
  summary: css({
    alignItems: "center",
    display: "flex",
    flex: "1 1 14rem",
    flexWrap: "wrap",
    gap: "1.5",
    minWidth: "0",
  }),
  chip: css({
    alignItems: "center",
    background: "bg.muted",
    borderRadius: "l1",
    color: "fg.muted",
    display: "inline-flex",
    fontSize: "xs",
    gap: "1.5",
    paddingBlock: "0.5",
    paddingInline: "2",
  }),
  note: css({
    color: "fg.muted",
    fontSize: "xs",
    maxWidth: "40rem",
  }),
}
