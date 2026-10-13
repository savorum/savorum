import { For, type JSX, Show, createSignal, untrack } from "solid-js"
import { css } from "styled-system/css"

import type { BookSummary, Facet, FacetKind, OrganizationInput, Tag } from "@/api/client"
import { organizationInput } from "@/api/organization"
import { suggestBooks } from "@/api/selections"
import { HeldLabels, LabelChooser, type ChoiceLoader } from "@/components/label-choices"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { Textarea } from "@/components/ui/textarea"
import type { OrganizationController } from "@/features/recipe-organization/create-organization-controller"
import { SourceFields } from "@/features/recipe-organization/source-fields"
import type { FilterValue } from "@/lib/filter-vocabulary"

import { m } from "../../lib/messages.ts"
const categoryGroups: { kind: FacetKind; label: string }[] = [
  {
    kind: "cuisine",
    get label() {
      return m.organization_editor_cuisine()
    },
  },
  {
    kind: "course",
    get label() {
      return m.organization_editor_meal()
    },
  },
  {
    kind: "dietary",
    get label() {
      return m.organization_editor_dietary()
    },
  },
  {
    kind: "method",
    get label() {
      return m.organization_editor_method()
    },
  },
]

const OrganizationSection = (props: { title: string; hint?: string; children?: JSX.Element }) => (
  <section class={styles.section}>
    <h3 class={styles.sectionTitle}>{props.title}</h3>
    <Show when={props.hint}>
      <p class={styles.sectionHint}>{props.hint}</p>
    </Show>
    {props.children}
  </section>
)

/** What the recipe is filed under in one group, and a named way to add to it. */
const GroupMembership = (props: {
  load?: ChoiceLoader
  values: FilterValue[]
  held: string[]
  disabled: boolean
  /** What the add control does, such as "Add a tag". */
  action: string
  searchLabel: string
  /** What to say when the recipe is in none of this group. */
  empty: string
  exhausted: string
  describeHeld: (value: FilterValue) => string
  describeOffered: (value: FilterValue) => string
  onChange: (id: string, held: boolean) => void
}) => {
  const carried = () => new Set(props.held)
  const held = () => props.values.filter((value) => carried().has(value.id))
  const offered = () => props.values.filter((value) => !carried().has(value.id))
  return (
    <>
      <HeldLabels
        values={held()}
        disabled={props.disabled}
        describe={props.describeHeld}
        onRemove={(id) => props.onChange(id, false)}
      />
      <Show when={!held().length}>
        <p class={styles.sectionHint}>{props.empty}</p>
      </Show>
      <LabelChooser
        action={props.action}
        searchLabel={props.searchLabel}
        values={offered()}
        load={props.load}
        disabled={props.disabled}
        describe={props.describeOffered}
        exhausted={props.exhausted}
        onChoose={(id) => props.onChange(id, true)}
      />
    </>
  )
}

export function OrganizationEditor(props: {
  controller: OrganizationController
  tags: Tag[]
  facets: Facet[]
  books: BookSummary[]
  onClose: () => void
  onSaved: () => void
}) {
  const [draft, setDraft] = createSignal<OrganizationInput>(
    untrack(() => structuredClone(organizationInput(props.controller.value()!))),
  )
  // Keep book revisions and membership from the moment this editor opens.
  const initialBooks = untrack(() => props.books.map((book) => ({ ...book })))
  const [knownBooks, setKnownBooks] = createSignal(initialBooks)
  const loadBooks: ChoiceLoader = async (query, offset) => {
    const page = await suggestBooks(query, offset)
    setKnownBooks((current) => [
      ...current,
      ...page.items
        .filter((book) => !current.some((known) => known.id === book.id))
        .map((book) => ({
          ...book,
          kind: book.kind as BookSummary["kind"],
          entry_count: 0,
          updated_at: "",
        })),
    ])
    return {
      items: page.items
        .filter((book) => !selectedBooks().includes(book.id))
        .map((book) => ({ id: book.id, label: book.title })),
      has_more: page.has_more,
    }
  }
  const [selectedBooks, setSelectedBooks] = createSignal(
    initialBooks.filter((book) => book.contains_entry).map((book) => book.id),
  )
  const [error, setError] = createSignal<unknown>()
  const carry = (key: "tag_ids" | "facet_ids", id: string, held: boolean) =>
    setDraft((current) => ({
      ...current,
      [key]: held
        ? [...(current[key] ?? []).filter((value) => value !== id), id]
        : (current[key] ?? []).filter((value) => value !== id),
    }))
  const carryBook = (id: string, held: boolean) =>
    setSelectedBooks((current) =>
      held ? [...current.filter((book) => book !== id), id] : current.filter((book) => book !== id),
    )
  const bookValues = () => knownBooks().map((book) => ({ id: book.id, label: book.title }))
  const tagValues = () =>
    props.tags.map((tag) => ({ id: tag.id, label: tag.name, color: tag.color }))
  const save = async () => {
    setError(undefined)
    try {
      await props.controller.update(() =>
        untrack(() => ({
          ...draft(),
          book_membership: knownBooks()
            .filter((book) => Boolean(book.contains_entry) !== selectedBooks().includes(book.id))
            .map((book) => ({
              book_id: book.id,
              revision: book.revision,
              member: selectedBooks().includes(book.id),
            })),
        })),
      )
      props.onSaved()
      props.onClose()
    } catch (failure) {
      setError(failure)
    }
  }
  return (
    <Dialog
      open
      title={m.organization_editor_organize_recipe()}
      onOpenChange={(event) => !event.open && !props.controller.saving() && props.onClose()}
      closeOnEscape={!props.controller.saving()}
      closeOnInteractOutside={!props.controller.saving()}
      footer={
        <>
          <DialogClose>
            {(closeProps) => (
              <Button {...closeProps()} variant="outline" disabled={props.controller.saving()}>
                {m.common_cancel()}
              </Button>
            )}
          </DialogClose>
          <Button
            colorPalette="accent"
            loading={props.controller.saving()}
            onClick={() => void save()}
          >
            {m.common_save_changes()}
          </Button>
        </>
      }
    >
      <div
        class={css({
          display: "grid",
          gap: "6",
          maxHeight: "65vh",
          overflowY: "auto",
          paddingInline: "1",
        })}
      >
        <ChangeError
          error={error()}
          onReload={() => {
            props.controller.reload()
            props.onSaved()
            props.onClose()
          }}
        />
        <OrganizationSection title={m.organization_editor_books()}>
          <Show
            when={props.books.length}
            fallback={<a href="/books">{m.organization_editor_create_a_book()}</a>}
          >
            <GroupMembership
              values={bookValues()}
              load={loadBooks}
              held={selectedBooks()}
              disabled={props.controller.saving()}
              action={m.organization_editor_add_to_a_book()}
              searchLabel={m.search_category({ category: m.search_books_noun() })}
              empty={m.organization_editor_not_in_a_book()}
              exhausted={m.organization_editor_every_book_holds_it()}
              describeHeld={(value) =>
                m.organization_editor_take_out_of_book({ value1: value.label })
              }
              describeOffered={(value) =>
                m.organization_editor_put_in_book({ value1: value.label })
              }
              onChange={carryBook}
            />
          </Show>
        </OrganizationSection>
        <OrganizationSection title={m.organization_editor_tags()}>
          <Show
            when={props.tags.length}
            fallback={<a href="/tags">{m.organization_editor_manage_tags()}</a>}
          >
            <GroupMembership
              values={tagValues()}
              held={draft().tag_ids ?? []}
              disabled={props.controller.saving()}
              action={m.organization_editor_add_a_tag()}
              searchLabel={m.search_category({ category: m.search_tags_noun() })}
              empty={m.organization_editor_no_tags_on_it()}
              exhausted={m.organization_editor_every_tag_on_it()}
              describeHeld={(value) => m.organization_editor_remove_tag({ value1: value.label })}
              describeOffered={(value) => m.organization_editor_add_tag({ value1: value.label })}
              onChange={(id, held) => carry("tag_ids", id, held)}
            />
          </Show>
        </OrganizationSection>
        <For each={categoryGroups}>
          {(group) => {
            const values = () =>
              props.facets
                .filter((facet) => facet.kind === group.kind)
                .map((facet) => ({ id: facet.id, label: facet.value }))
            const noun = () => group.label.toLowerCase()
            return (
              <Show when={values().length}>
                <OrganizationSection title={group.label}>
                  <GroupMembership
                    values={values()}
                    held={draft().facet_ids ?? []}
                    disabled={props.controller.saving()}
                    action={m.organization_editor_add_a_category({ value1: noun() })}
                    searchLabel={m.search_category({
                      category: m.organization_editor_categories_count({ value1: noun() }),
                    })}
                    empty={m.organization_editor_none_chosen()}
                    exhausted={m.organization_editor_every_category_on_it({ value1: noun() })}
                    describeHeld={(value) =>
                      m.organization_editor_remove_category({ value1: value.label })
                    }
                    describeOffered={(value) =>
                      m.organization_editor_add_category({ value1: value.label })
                    }
                    onChange={(id, held) => carry("facet_ids", id, held)}
                  />
                </OrganizationSection>
              </Show>
            )
          }}
        </For>
        <Show when={!props.facets.length}>
          <OrganizationSection
            title={m.organization_editor_categories_label()}
            hint={m.organization_editor_shared_categories_hint()}
          >
            <a href="/tags">{m.organization_editor_manage_categories()}</a>
          </OrganizationSection>
        </Show>
        <Disclosure label={m.organization_editor_notes_and_sources()}>
          <FormField label={m.organization_editor_personal_note()}>
            <Textarea
              value={draft().user_state.personal_note ?? ""}
              onInput={(event) => {
                const value = event.currentTarget.value
                setDraft((current) => ({
                  ...current,
                  user_state: { ...current.user_state, personal_note: value },
                }))
              }}
            />
          </FormField>
          <Disclosure label={m.organization_editor_sources()}>
            <SourceFields value={draft()} books={props.books} onChange={setDraft} />
          </Disclosure>
        </Disclosure>
      </div>
    </Dialog>
  )
}

const styles = {
  section: css({ display: "grid", gap: "3" }),
  sectionTitle: css({
    color: "fg.default",
    fontSize: "sm",
    fontWeight: "semibold",
    margin: "0",
  }),
  sectionHint: css({
    color: "fg.muted",
    fontSize: "xs",
    lineHeight: "1.5",
    margin: "0",
    marginTop: "-2",
  }),
}
