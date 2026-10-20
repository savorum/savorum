import { X } from "lucide-solid"
import { For, Show, createMemo, onCleanup, type JSX } from "solid-js"

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
  onEmpty?: () => void
  actions?: JSX.Element
}

/** Shows what is currently narrowing the library, each removable. */
export const FilterSummary = (props: FilterSummaryProps) => {
  const entries = createMemo(() => {
    const result: { id: string; label: string; color?: string; remove: () => LibraryFilters }[] = []
    const tagColors = new Map(props.vocabulary.tags().map((tag) => [tag.id, tag.color]))
    const drop = (key: keyof LibraryFilters, value: string) => () => ({
      ...props.filters,
      [key]: (props.filters[key] as string[]).filter((entry) => entry !== value),
    })
    for (const tag of props.filters.tags) {
      result.push({
        id: `tags:${tag}`,
        label: props.vocabulary.label(tag),
        color: tagColors.get(tag),
        remove: drop("tags", tag),
      })
    }
    for (const group of facetGroups) {
      for (const value of props.filters[group.key]) {
        result.push({
          id: `${group.key}:${value}`,
          label: props.vocabulary.label(value),
          remove: drop(group.key, value),
        })
      }
    }
    for (const book of props.filters.book) {
      result.push({
        id: `book:${book}`,
        label: props.vocabulary.label(book),
        remove: drop("book", book),
      })
    }
    for (const book of props.filters.source) {
      result.push({
        id: `source:${book}`,
        get label() {
          return m.library_filters_from({ value1: props.vocabulary.label(book) })
        },
        remove: drop("source", book),
      })
    }
    for (const author of props.filters.author ?? []) {
      result.push({
        id: `author:${author}`,
        get label() {
          return `${m.list_fields_author()}: ${author}`
        },
        remove: drop("author", author),
      })
    }
    for (const ingredient of props.filters.ingredient ?? []) {
      result.push({
        id: `ingredient:${ingredient}`,
        get label() {
          return `${m.ingredient_row_ingredient()}: ${ingredient}`
        },
        remove: drop("ingredient", ingredient),
      })
    }
    if (props.filters.favorite) {
      result.push({
        id: "favorite",
        get label() {
          return m.library_filters_favorites()
        },
        remove: () => ({ ...props.filters, favorite: undefined }),
      })
    }
    if (props.filters.rating) {
      result.push({
        id: "rating",
        get label() {
          return m.common_rating_stars_min({ value1: formatNumber(props.filters.rating ?? 0) })
        },
        remove: () => ({ ...props.filters, rating: undefined }),
      })
    }
    for (const diet of props.filters.stated ?? []) {
      result.push({
        id: `stated:${diet}`,
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
        id: `free:${substance}`,
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
        id: "unmade",
        get label() {
          return m.library_filters_not_cooked_yet()
        },
        remove: () => ({ ...props.filters, unmade: undefined }),
      })
    }
    if (props.filters.review) {
      result.push({
        id: "review",
        get label() {
          return m.library_filters_needs_ingredient_review()
        },
        remove: () => ({ ...props.filters, review: undefined }),
      })
    }
    // An import scope is shown and removable like any other filter.
    if (props.filters.imported) {
      result.push({
        id: "imported",
        get label() {
          return m.library_filters_from_one_import()
        },
        remove: () => ({ ...props.filters, imported: undefined }),
      })
    }
    return result
  })

  const buttons = new Map<string, HTMLButtonElement>()
  const ids = createMemo(() => entries().map((entry) => entry.id))
  const entryFor = (id: string) => entries().find((entry) => entry.id === id)!
  const remove = (id: string) => {
    const current = ids()
    const index = current.indexOf(id)
    const next = current[index + 1] ?? current[index - 1]
    if (next) buttons.get(next)?.focus()
    props.onChange(entryFor(id).remove())
    if (!next) props.onEmpty?.()
  }
  return (
    <Show when={entries().length > 0 || props.actions}>
      <div class={styles.summary}>
        <For each={ids()}>
          {(id) => {
            onCleanup(() => buttons.delete(id))
            return (
              <button
                type="button"
                class={chip({ selected: true })}
                ref={(element) => buttons.set(id, element)}
                onClick={() => remove(id)}
              >
                <LabelSwatch color={entryFor(id).color} size="sm" />
                {entryFor(id).label}
                <X aria-hidden="true" size={12} />
              </button>
            )
          }}
        </For>
        <div class={styles.summaryActions}>
          <Show when={entries().length > 0}>
            <button
              type="button"
              class={styles.clear}
              onClick={() => {
                props.onChange(emptyFilters)
                props.onEmpty?.()
              }}
            >
              {m.library_filters_clear_all()}
            </button>
          </Show>
          {props.actions}
        </div>
      </div>
    </Show>
  )
}
