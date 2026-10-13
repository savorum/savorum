import { For, Show, createMemo, createSignal, untrack } from "solid-js"
import { css } from "styled-system/css"

import type { BookSummary, FacetKind } from "@/api/client"
import { getOrganization, replaceOrganization } from "@/api/organization"
import { lookupBooks, lookupRecipes } from "@/api/selections"
import { LabelChooser, type ChoiceLoader } from "@/components/label-choices"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import type { FilterValue } from "@/lib/filter-vocabulary"
import { formatNumber } from "@/lib/locale-format"

import { m } from "../../lib/messages.ts"
import {
  type BulkOrganizationPlan,
  type LabelIntent,
  type PlannedChange,
  emptyBulkPlan,
  planMembership,
  planOrganization,
  plannedChangeList,
  plannedChanges,
  withIntent,
  withoutIntent,
} from "./create-bulk-organization"
import type { LibraryVocabulary } from "./create-library-vocabulary"

const categoryGroups: { kind: FacetKind; label: string }[] = [
  {
    kind: "cuisine",
    get label() {
      return m.bulk_organize_dialog_cuisine()
    },
  },
  {
    kind: "course",
    get label() {
      return m.bulk_organize_dialog_meal()
    },
  },
  {
    kind: "dietary",
    get label() {
      return m.bulk_organize_dialog_dietary()
    },
  },
  {
    kind: "method",
    get label() {
      return m.bulk_organize_dialog_method()
    },
  },
]

/** One group's two operations, adding and removing labels, offered separately. */
const GroupPlan = (props: {
  load?: ChoiceLoader
  title: string
  values: FilterValue[]
  plan: Record<string, LabelIntent>
  searchNoun: string
  addAction: string
  removeAction: string
  exhausted: string
  describeAdd: (value: FilterValue) => string
  describeRemove: (value: FilterValue) => string
  disabled: boolean
  onChoose: (id: string, intent: LabelIntent) => void
}) => {
  // A label already named is not on offer again; it is in the summary above,
  // where it can be taken back out.
  const offered = () => props.values.filter((value) => !props.plan[value.id])
  return (
    <section class={styles.section}>
      <h3 class={styles.sectionTitle}>{props.title}</h3>
      <div class={styles.operations}>
        <LabelChooser
          action={props.addAction}
          searchLabel={m.bulk_organize_dialog_search_to_add({ value1: props.searchNoun })}
          values={offered()}
          load={props.load}
          disabled={props.disabled}
          describe={props.describeAdd}
          exhausted={props.exhausted}
          onChoose={(id) => props.onChoose(id, "add")}
        />
        <LabelChooser
          action={props.removeAction}
          searchLabel={m.bulk_organize_dialog_search_to_take_off({ value1: props.searchNoun })}
          values={offered()}
          load={props.load}
          disabled={props.disabled}
          describe={props.describeRemove}
          exhausted={props.exhausted}
          onChoose={(id) => props.onChoose(id, "remove")}
        />
      </div>
    </section>
  )
}

export function BulkOrganizeDialog(props: {
  recipeIDs: string[]
  vocabulary: LibraryVocabulary
  onClose: () => void
  onApplied: () => void
}) {
  const [plan, setPlan] = createSignal<BulkOrganizationPlan>(emptyBulkPlan)
  const [busy, setBusy] = createSignal(false)
  const [done, setDone] = createSignal(0)
  const [failed, setFailed] = createSignal<string[]>([])
  const [error, setError] = createSignal<unknown>()

  const choose = (group: keyof BulkOrganizationPlan, id: string, intent: LabelIntent) =>
    setPlan((current) => ({ ...current, [group]: withIntent(current[group], id, intent) }))
  const unchoose = (group: keyof BulkOrganizationPlan, id: string) =>
    setPlan((current) => ({ ...current, [group]: withoutIntent(current[group], id) }))

  /**
   * One recipe at a time, each read immediately before it is written, so a
   * partial failure keeps what was saved and concurrent edits are not lost.
   */
  const applyTo = async (ids: string[]) => {
    const chosen = untrack(plan)
    const failures: string[] = []
    setBusy(true)
    setDone(0)
    setError(undefined)
    for (const id of ids) {
      try {
        const current = await getOrganization(id)
        const membership = Object.keys(chosen.books).length
          ? planMembership(await membershipBooks(id, Object.keys(chosen.books)), chosen.books)
          : []
        await replaceOrganization(id, current.revision, {
          ...planOrganization(current.value, chosen),
          book_membership: membership,
        })
      } catch (failure) {
        failures.push(id)
        setError(failure)
      }
      setDone((count) => count + 1)
    }
    setBusy(false)
    setFailed(failures)
    props.onApplied()
    if (failures.length === 0) props.onClose()
  }

  const bookValues = () =>
    props.vocabulary.books().map((book: BookSummary) => ({ id: book.id, label: book.title }))
  const tagValues = () =>
    props.vocabulary.tags().map((tag) => ({ id: tag.id, label: tag.name, color: tag.color }))
  const facetValues = (kind: FacetKind) =>
    props.vocabulary
      .facets()
      .filter((facet) => facet.kind === kind)
      .map((facet) => ({ id: facet.id, label: facet.value }))
  const noun = () =>
    props.recipeIDs.length === 1
      ? m.bulk_organize_dialog_one_recipe()
      : m.bulk_organize_dialog_many_recipes({ value1: formatNumber(props.recipeIDs.length) })
  const changes = createMemo(() =>
    plannedChangeList(plan(), {
      books: bookValues(),
      tags: tagValues(),
      facets: props.vocabulary.facets().map((facet) => ({ id: facet.id, label: facet.value })),
    }),
  )
  /** The change as a sentence, which is what a reader has to be able to state. */
  const describe = (change: PlannedChange) => {
    if (change.group === "books") {
      return change.intent === "add"
        ? m.bulk_organize_dialog_put_in_book({ value1: noun(), value2: change.label })
        : m.bulk_organize_dialog_take_out_of_book({ value1: noun(), value2: change.label })
    }
    return change.intent === "add"
      ? m.bulk_organize_dialog_add_to({ value1: change.label, value2: noun() })
      : m.bulk_organize_dialog_take_off({ value1: change.label, value2: noun() })
  }

  return (
    <Dialog
      open
      title={m.bulk_organize_dialog_organize({ value1: noun() })}
      onOpenChange={(event) => !event.open && !busy() && props.onClose()}
      closeOnEscape={!busy()}
      closeOnInteractOutside={!busy()}
      footer={
        <>
          <DialogClose>
            {(closeProps) => (
              <Button {...closeProps()} variant="outline" disabled={busy()}>
                {m.common_cancel()}
              </Button>
            )}
          </DialogClose>
          <Button
            colorPalette="accent"
            loading={busy()}
            disabled={plannedChanges(plan()) === 0}
            onClick={() => void applyTo(failed().length > 0 ? failed() : props.recipeIDs)}
          >
            {failed().length > 0
              ? m.bulk_organize_dialog_try_again({ value1: failed().length })
              : m.bulk_organize_dialog_apply_to({ value1: noun() })}
          </Button>
        </>
      }
    >
      <div class={styles.body}>
        <ChangeError error={error()} />
        <section class={styles.summary} aria-label={m.bulk_organize_dialog_these_changes()}>
          <h3 class={styles.sectionTitle}>{m.bulk_organize_dialog_these_changes()}</h3>
          <Show
            when={changes().length}
            fallback={<p class={styles.hint}>{m.bulk_organize_dialog_nothing_chosen_yet()}</p>}
          >
            <ul class={styles.changes}>
              <For each={changes()}>
                {(change) => (
                  <li class={styles.change}>
                    <span>{describe(change)}</span>
                    <button
                      type="button"
                      class={styles.undo}
                      disabled={busy()}
                      aria-label={m.bulk_organize_dialog_undo_change({ value1: describe(change) })}
                      onClick={() => unchoose(change.group, change.id)}
                    >
                      {m.bulk_organize_dialog_undo()}
                    </button>
                  </li>
                )}
              </For>
            </ul>
          </Show>
          <p class={styles.hint}>{m.bulk_organize_dialog_everything_else_is_left_alone()}</p>
        </section>
        <Show when={busy()}>
          <p class={styles.hint} aria-live="polite">
            {m.bulk_organize_dialog_saving_of({
              value1: formatNumber(done()),
              value2: formatNumber(props.recipeIDs.length),
            })}
          </p>
        </Show>
        <Show when={!busy() && failed().length > 0}>
          <p class={styles.failed} aria-live="polite">
            {m.bulk_organize_dialog_of_could_not_be_saved_the_rest_were({
              value1: formatNumber(failed().length),
              value2: formatNumber(props.recipeIDs.length),
            })}
          </p>
        </Show>
        <Show
          when={props.vocabulary.books().length}
          fallback={
            <section class={styles.section}>
              <h3 class={styles.sectionTitle}>{m.bulk_organize_dialog_books()}</h3>
              <p class={styles.hint}>{m.bulk_organize_dialog_no_books_yet()}</p>
            </section>
          }
        >
          <GroupPlan
            title={m.bulk_organize_dialog_books()}
            values={bookValues()}
            load={
              props.vocabulary.searchBooks
                ? async (query, offset) => {
                    const page = await props.vocabulary.searchBooks!(query, offset)
                    return {
                      items: page.items
                        .filter((book) => !plan().books[book.id])
                        .map((book) => ({ id: book.id, label: book.title })),
                      has_more: page.has_more,
                    }
                  }
                : undefined
            }
            plan={plan().books}
            searchNoun={m.search_books_noun()}
            addAction={m.bulk_organize_dialog_add_to_a_book()}
            removeAction={m.bulk_organize_dialog_take_out_of_a_book()}
            exhausted={m.bulk_organize_dialog_every_book_is_named()}
            describeAdd={(value) =>
              m.bulk_organize_dialog_put_in_book({ value1: noun(), value2: value.label })
            }
            describeRemove={(value) =>
              m.bulk_organize_dialog_take_out_of_book({ value1: noun(), value2: value.label })
            }
            disabled={busy()}
            onChoose={(id, intent) => choose("books", id, intent)}
          />
        </Show>
        <Show
          when={props.vocabulary.tags().length}
          fallback={
            <section class={styles.section}>
              <h3 class={styles.sectionTitle}>{m.bulk_organize_dialog_tags()}</h3>
              <p class={styles.hint}>{m.bulk_organize_dialog_no_tags_yet()}</p>
            </section>
          }
        >
          <GroupPlan
            title={m.bulk_organize_dialog_tags()}
            values={tagValues()}
            plan={plan().tags}
            searchNoun={m.search_tags_noun()}
            addAction={m.bulk_organize_dialog_add_a_tag()}
            removeAction={m.bulk_organize_dialog_take_a_tag_off()}
            exhausted={m.bulk_organize_dialog_every_tag_is_named()}
            describeAdd={(value) =>
              m.bulk_organize_dialog_add_to({ value1: value.label, value2: noun() })
            }
            describeRemove={(value) =>
              m.bulk_organize_dialog_take_off({ value1: value.label, value2: noun() })
            }
            disabled={busy()}
            onChoose={(id, intent) => choose("tags", id, intent)}
          />
        </Show>
        <For each={categoryGroups}>
          {(group) => {
            const categoryNoun = () => group.label.toLowerCase()
            return (
              <Show when={facetValues(group.kind).length}>
                <GroupPlan
                  title={group.label}
                  values={facetValues(group.kind)}
                  plan={plan().facets}
                  searchNoun={m.bulk_organize_dialog_categories({ value1: categoryNoun() })}
                  addAction={m.bulk_organize_dialog_add_a_category({ value1: categoryNoun() })}
                  removeAction={m.bulk_organize_dialog_take_a_category_off({
                    value1: categoryNoun(),
                  })}
                  exhausted={m.bulk_organize_dialog_every_category_is_named({
                    value1: categoryNoun(),
                  })}
                  describeAdd={(value) =>
                    m.bulk_organize_dialog_add_to({ value1: value.label, value2: noun() })
                  }
                  describeRemove={(value) =>
                    m.bulk_organize_dialog_take_off({ value1: value.label, value2: noun() })
                  }
                  disabled={busy()}
                  onChoose={(id, intent) => choose("facets", id, intent)}
                />
              </Show>
            )
          }}
        </For>
      </div>
    </Dialog>
  )
}

const styles = {
  body: css({
    display: "grid",
    gap: "5",
    maxHeight: "65vh",
    overflowY: "auto",
    paddingInline: "1",
  }),
  section: css({ display: "grid", gap: "2" }),
  summary: css({
    background: "bg.subtle",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "2",
    padding: "3",
  }),
  sectionTitle: css({ color: "fg.default", fontSize: "sm", fontWeight: "semibold", margin: "0" }),
  operations: css({ display: "flex", flexWrap: "wrap", gap: "4" }),
  changes: css({ display: "grid", gap: "1.5", listStyle: "none", margin: "0", padding: "0" }),
  change: css({
    alignItems: "baseline",
    color: "fg.default",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    gap: "2",
  }),
  hint: css({ color: "fg.muted", fontSize: "xs", lineHeight: "1.5", margin: "0" }),
  failed: css({ color: "danger.default", fontSize: "xs", margin: "0" }),
  undo: css({
    background: "none",
    borderWidth: "0",
    color: "fg.muted",
    cursor: "pointer",
    fontSize: "xs",
    padding: "0",
    textDecoration: "underline",
    textUnderlineOffset: "3px",
    _hover: { color: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "2px" },
  }),
}

async function membershipBooks(entryID: string, ids: string[]): Promise<BookSummary[]> {
  const [recipe, books] = await Promise.all([lookupRecipes([entryID]), lookupBooks(ids)])
  const held = new Set((recipe.items[0]?.books ?? []).map((book) => book.id))
  return books.items.map((book) => ({
    ...book,
    kind: book.kind as BookSummary["kind"],
    contains_entry: held.has(book.id),
    entry_count: 0,
    updated_at: "",
  }))
}
