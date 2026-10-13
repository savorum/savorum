import { For, Show, type JSX } from "solid-js"

import type { BookSummary, Facet, FacetKind, Tag } from "@/api/client"
import { HeldLabels, LabelChooser } from "@/components/label-choices"
import type { LibraryVocabulary } from "@/features/library/create-library-vocabulary"
import type { FilterValue } from "@/lib/filter-vocabulary"
import { type LibraryFilters, facetGroups } from "@/lib/library-query"
import { type Diet, dietName } from "@/lib/recipe-facts"

import { m } from "../../lib/messages.ts"
import { chip, styles } from "./filter-styles"

const statedDiets: Diet[] = ["vegan", "vegetarian", "gluten_free"]

interface FilterGroupProps {
  label: string
  /** What the picker does, such as "Choose a tag". */
  action: string
  /** What the group holds, so a search prompt reads as a sentence. */
  searchNoun: string
  exhausted: string
  values: FilterValue[]
  selected: string[]
  onToggle: (id: string) => void
}

/**
 * One filter group: what the library is currently narrowed to, and a named way
 * to narrow it further.
 */
const FilterGroup = (props: FilterGroupProps) => {
  const chosen = () => new Set(props.selected)
  const held = () => props.values.filter((value) => chosen().has(value.id))
  const offered = () => props.values.filter((value) => !chosen().has(value.id))
  return (
    <Show when={props.values.length > 0}>
      <div class={styles.group}>
        <p class={styles.groupTitle}>{props.label}</p>
        <HeldLabels
          values={held()}
          describe={(value) => m.library_filters_stop_narrowing({ value1: value.label })}
          onRemove={props.onToggle}
        />
        <LabelChooser
          action={props.action}
          searchLabel={m.search_category({ category: props.searchNoun })}
          values={offered()}
          describe={(value) => m.library_filters_narrow_to({ value1: value.label })}
          exhausted={props.exhausted}
          onChoose={props.onToggle}
        />
      </div>
    </Show>
  )
}

export interface FilterPanelProps {
  tools?: JSX.Element
  filters: LibraryFilters
  vocabulary: LibraryVocabulary
  onChange: (filters: LibraryFilters) => void
}

export const FilterPanel = (props: FilterPanelProps) => {
  const toggle = (
    key: "tags" | "cuisine" | "course" | "dietary" | "method" | "book" | "source",
    value: string,
  ) => {
    const current = props.filters[key]
    props.onChange({
      ...props.filters,
      [key]: current.includes(value)
        ? current.filter((entry) => entry !== value)
        : [...current, value],
    })
  }

  const facetValues = (kind: FacetKind) =>
    props.vocabulary
      .facets()
      .filter((facet: Facet) => facet.kind === kind)
      .map((facet) => ({ id: facet.id, label: facet.value }))
  const publications = () =>
    props.vocabulary
      .books()
      .filter((book: BookSummary) => book.kind === "publication")
      .map((book) => ({ id: book.id, label: book.title }))

  const shortcuts = () => [
    {
      get label() {
        return m.library_filters_favorites()
      },
      on: Boolean(props.filters.favorite),
      toggle: () => ({
        ...props.filters,
        favorite: props.filters.favorite ? undefined : true,
      }),
    },
    {
      get label() {
        return m.library_filters_not_cooked_yet()
      },
      on: Boolean(props.filters.unmade),
      toggle: () => ({ ...props.filters, unmade: props.filters.unmade ? undefined : true }),
    },
    {
      get label() {
        return m.library_filters_4_stars()
      },
      on: props.filters.rating === 4,
      toggle: () => ({ ...props.filters, rating: props.filters.rating === 4 ? undefined : 4 }),
    },
    {
      get label() {
        return m.library_filters_needs_ingredient_review()
      },
      on: Boolean(props.filters.review),
      toggle: () => ({ ...props.filters, review: props.filters.review ? undefined : true }),
    },
  ]

  return (
    <div class={styles.panel}>
      <div class={styles.group}>
        <p class={styles.groupTitle}>{m.library_filters_only_show()}</p>
        <div class={styles.chips}>
          <For each={shortcuts()}>
            {(shortcut) => (
              <button
                type="button"
                class={chip({ selected: shortcut.on })}
                aria-pressed={shortcut.on}
                onClick={() => props.onChange(shortcut.toggle())}
              >
                {shortcut.label}
              </button>
            )}
          </For>
        </div>
      </div>
      {props.tools}
      <div class={styles.group}>
        <p class={styles.groupTitle}>{m.library_filters_stated_by_the_recipe()}</p>
        <div class={styles.chips}>
          <For each={statedDiets}>
            {(diet) => {
              const on = () => (props.filters.stated ?? []).includes(diet)
              return (
                <button
                  type="button"
                  class={chip({ selected: on() })}
                  aria-pressed={on()}
                  onClick={() =>
                    props.onChange({
                      ...props.filters,
                      stated: on()
                        ? props.filters.stated?.filter((entry) => entry !== diet)
                        : [...(props.filters.stated ?? []), diet],
                    })
                  }
                >
                  {dietName(diet)}
                </button>
              )
            }}
          </For>
        </div>
        <p class={styles.hint}>{m.library_filters_stated_hint()}</p>
      </div>
      <FilterGroup
        label={m.library_filters_tags()}
        action={m.library_filters_choose_a_tag()}
        searchNoun={m.search_tags_noun()}
        exhausted={m.library_filters_every_one_chosen()}
        values={props.vocabulary
          .tags()
          .map((tag: Tag) => ({ id: tag.id, label: tag.name, color: tag.color }))}
        selected={props.filters.tags}
        onToggle={(value) => toggle("tags", value)}
      />
      <For each={facetGroups}>
        {(group) => (
          <FilterGroup
            label={group.label}
            action={m.library_filters_choose_a_category({ value1: group.singular })}
            searchNoun={group.plural}
            exhausted={m.library_filters_every_one_chosen()}
            values={facetValues(group.kind)}
            selected={props.filters[group.key]}
            onToggle={(value) => toggle(group.key, value)}
          />
        )}
      </For>
      <FilterGroup
        label={m.library_filters_in_a_book()}
        action={m.library_filters_choose_a_book()}
        searchNoun={m.search_books_noun()}
        exhausted={m.library_filters_every_one_chosen()}
        values={props.vocabulary
          .books()
          .map((book: BookSummary) => ({ id: book.id, label: book.title }))}
        selected={props.filters.book}
        onToggle={(value) => toggle("book", value)}
      />
      <FilterGroup
        label={m.library_filters_taken_from_a_cookbook()}
        action={m.library_filters_choose_a_cookbook()}
        searchNoun={m.search_cookbooks_noun()}
        exhausted={m.library_filters_every_one_chosen()}
        values={publications()}
        selected={props.filters.source}
        onToggle={(value) => toggle("source", value)}
      />
    </div>
  )
}
