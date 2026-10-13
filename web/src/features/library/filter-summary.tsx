import { X } from "lucide-solid"
import { For, Show, createMemo } from "solid-js"

import { LabelSwatch } from "@/components/label-swatch"
import type { LibraryVocabulary } from "@/features/library/create-library-vocabulary"
import { type LibraryFilters, emptyFilters, facetGroups } from "@/lib/library-query"
import { formatNumber } from "@/lib/locale-format"
import { type Diet, dietName } from "@/lib/recipe-facts"

import { m } from "../../lib/messages.ts"
import { chip, styles } from "./filter-styles"

export interface FilterSummaryProps {
  filters: LibraryFilters
  vocabulary: LibraryVocabulary
  onChange: (filters: LibraryFilters) => void
}

/** Shows what is currently narrowing the library, each removable. */
export const FilterSummary = (props: FilterSummaryProps) => {
  const entries = createMemo(() => {
    const result: { label: string; color?: string; remove: () => LibraryFilters }[] = []
    const tagColors = new Map(props.vocabulary.tags().map((tag) => [tag.id, tag.color]))
    const drop = (key: keyof LibraryFilters, value: string) => () => ({
      ...props.filters,
      [key]: (props.filters[key] as string[]).filter((entry) => entry !== value),
    })
    for (const tag of props.filters.tags) {
      result.push({
        label: props.vocabulary.label(tag),
        color: tagColors.get(tag),
        remove: drop("tags", tag),
      })
    }
    for (const group of facetGroups) {
      for (const value of props.filters[group.key]) {
        result.push({ label: props.vocabulary.label(value), remove: drop(group.key, value) })
      }
    }
    for (const book of props.filters.book) {
      result.push({ label: props.vocabulary.label(book), remove: drop("book", book) })
    }
    for (const book of props.filters.source) {
      result.push({
        get label() {
          return m.library_filters_from({ value1: props.vocabulary.label(book) })
        },
        remove: drop("source", book),
      })
    }
    for (const author of props.filters.author ?? []) {
      result.push({
        get label() {
          return `${m.list_fields_author()}: ${author}`
        },
        remove: drop("author", author),
      })
    }
    for (const ingredient of props.filters.ingredient ?? []) {
      result.push({
        get label() {
          return `${m.ingredient_row_ingredient()}: ${ingredient}`
        },
        remove: drop("ingredient", ingredient),
      })
    }
    if (props.filters.favorite) {
      result.push({
        get label() {
          return m.library_filters_favorites()
        },
        remove: () => ({ ...props.filters, favorite: undefined }),
      })
    }
    if (props.filters.rating) {
      result.push({
        get label() {
          return m.common_rating_stars_min({ value1: formatNumber(props.filters.rating ?? 0) })
        },
        remove: () => ({ ...props.filters, rating: undefined }),
      })
    }
    for (const diet of props.filters.stated ?? []) {
      result.push({
        get label() {
          return m.library_query_stated_diet({ diet: dietName(diet as Diet) })
        },
        remove: () => ({
          ...props.filters,
          stated: props.filters.stated?.filter((entry) => entry !== diet),
        }),
      })
    }
    for (const substance of props.filters.free ?? []) {
      result.push({
        get label() {
          return m.library_query_stated_free_from({ substance })
        },
        remove: () => ({
          ...props.filters,
          free: props.filters.free?.filter((entry) => entry !== substance),
        }),
      })
    }
    if (props.filters.unmade) {
      result.push({
        get label() {
          return m.library_filters_not_cooked_yet()
        },
        remove: () => ({ ...props.filters, unmade: undefined }),
      })
    }
    if (props.filters.review) {
      result.push({
        get label() {
          return m.library_filters_needs_ingredient_review()
        },
        remove: () => ({ ...props.filters, review: undefined }),
      })
    }
    // An import scope is shown and removable like any other filter.
    if (props.filters.imported) {
      result.push({
        get label() {
          return m.library_filters_from_one_import()
        },
        remove: () => ({ ...props.filters, imported: undefined }),
      })
    }
    return result
  })

  return (
    <Show when={entries().length > 0}>
      <div class={styles.summary}>
        <For each={entries()}>
          {(entry) => (
            <button
              type="button"
              class={chip({ selected: true })}
              onClick={() => props.onChange(entry.remove())}
            >
              <LabelSwatch color={entry.color} size="sm" />
              {entry.label}
              <X aria-hidden="true" size={12} />
            </button>
          )}
        </For>
        <button type="button" class={styles.clear} onClick={() => props.onChange(emptyFilters)}>
          {" "}
          {m.library_filters_clear_all()}{" "}
        </button>
      </div>
    </Show>
  )
}
