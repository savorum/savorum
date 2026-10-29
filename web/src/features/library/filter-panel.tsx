import { For, untrack, Show } from "solid-js"

import type { BookSummary, Facet, FacetKind, Tag } from "@/api/client"
import type { ChoiceLoader } from "@/components/label-choices"
import { Disclosure } from "@/components/ui/disclosure"
import type { LibraryVocabulary } from "@/features/library/create-library-vocabulary"
import type { FilterValue } from "@/lib/filter-vocabulary"
import { topLabels } from "@/lib/label-ranking"
import { type LibraryFilters, facetGroups } from "@/lib/library-query"
import { type Diet, dietName } from "@/lib/recipe-facts"

import { m } from "../../lib/messages.ts"
import { FilterPicker } from "./filter-picker"
import { chip, styles } from "./filter-styles"

const statedDiets: Diet[] = ["vegan", "vegetarian", "gluten_free"]

interface ShortcutProps {
  label: string
  on: boolean
  onToggle: () => void
}

const Shortcut = (props: ShortcutProps) => (
  <button
    type="button"
    class={chip({ selected: props.on })}
    aria-pressed={props.on}
    onClick={() => props.onToggle()}
  >
    {props.label}
  </button>
)

interface FilterGroupProps {
  label: string
  /** What the picker does, such as "Choose a tag". */
  action: string
  /** What the group holds, so a search prompt reads as a sentence. */
  searchNoun: string
  load?: ChoiceLoader
  values: FilterValue[]
  selected: string[]
  onToggle: (id: string) => void
}

/**
 * One filter group: what the library is currently narrowed to, and a named way
 * to narrow it further.
 */
const FilterGroup = (props: FilterGroupProps) => {
  return (
    <Show when={props.values.length > 0 || props.load}>
      <div class={styles.group}>
        <p class={styles.groupTitle}>{props.label}</p>
        <FilterPicker {...props} />
      </div>
    </Show>
  )
}

export interface FilterPanelProps {
  filters: LibraryFilters
  vocabulary: LibraryVocabulary
  onChange: (filters: LibraryFilters) => void
}

export const FilterPanel = (props: FilterPanelProps) => {
  const booksLoader = (publicationsOnly = false): ChoiceLoader | undefined =>
    props.vocabulary.searchBooks
      ? async (query, offset) => {
          const page = await untrack(() => props.vocabulary.searchBooks)!(query, offset)
          return {
            items: page.items
              .filter((book) => !publicationsOnly || book.kind === "publication")
              .map((book) => ({ id: book.id, label: book.title })),
            has_more: page.has_more,
          }
        }
      : undefined

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

  const facetGroup = (key: (typeof facetGroups)[number]["key"]) =>
    facetGroups.find((group) => group.key === key)!
  const facetFilter = (key: (typeof facetGroups)[number]["key"]) => {
    const group = facetGroup(key)
    return (
      <FilterGroup
        label={group.label}
        action={m.library_filters_choose_a_category({ value1: group.singular })}
        searchNoun={group.plural}
        values={facetValues(group.kind)}
        selected={props.filters[group.key]}
        onToggle={(value) => toggle(group.key, value)}
      />
    )
  }
  const topChips = () =>
    topLabels(
      [
        ...props.vocabulary.tags().map((tag: Tag) => ({
          id: tag.id,
          name: tag.name,
          kind: "tag",
          key: "tags" as const,
          recipeCount: tag.recipe_count ?? 0,
        })),
        ...props.vocabulary.facets().map((facet: Facet) => ({
          id: facet.id,
          name: facet.value,
          kind: facet.kind,
          key: facet.kind,
          recipeCount: facet.recipe_count ?? 0,
        })),
      ],
      8,
      props.vocabulary.recipeTotal?.(),
    )
  const statedOn = (diet: Diet) => (props.filters.stated ?? []).includes(diet)
  const toggleStated = (diet: Diet) =>
    props.onChange({
      ...props.filters,
      stated: statedOn(diet)
        ? props.filters.stated?.filter((entry) => entry !== diet)
        : [...(props.filters.stated ?? []), diet],
    })
  const moreInUse = () =>
    props.filters.dietary.length > 0 ||
    props.filters.method.length > 0 ||
    props.filters.source.length > 0 ||
    (props.filters.stated ?? []).length > 0

  return (
    <div class={styles.panel}>
      <div class={styles.group}>
        <p class={styles.groupTitle}>{m.library_filters_only_show()}</p>
        <div class={styles.chips}>
          <Shortcut
            label={m.library_filters_favorites()}
            on={Boolean(props.filters.favorite)}
            onToggle={() =>
              props.onChange({
                ...props.filters,
                favorite: props.filters.favorite ? undefined : true,
              })
            }
          />
          <Shortcut
            label={m.library_filters_not_cooked_yet()}
            on={Boolean(props.filters.unmade)}
            onToggle={() =>
              props.onChange({ ...props.filters, unmade: props.filters.unmade ? undefined : true })
            }
          />
          <Shortcut
            label={m.library_filters_4_stars()}
            on={props.filters.rating === 4}
            onToggle={() =>
              props.onChange({
                ...props.filters,
                rating: props.filters.rating === 4 ? undefined : 4,
              })
            }
          />
          <For each={topChips()}>
            {(chip) => (
              <Shortcut
                label={chip.name}
                on={props.filters[chip.key].includes(chip.id)}
                onToggle={() => toggle(chip.key, chip.id)}
              />
            )}
          </For>
        </div>
      </div>
      <div class={styles.categories}>
        <FilterGroup
          label={m.library_filters_tags()}
          action={m.library_filters_choose_a_tag()}
          searchNoun={m.search_tags_noun()}
          values={props.vocabulary
            .tags()
            .map((tag: Tag) => ({ id: tag.id, label: tag.name, color: tag.color }))}
          selected={props.filters.tags}
          onToggle={(value) => toggle("tags", value)}
        />
        {facetFilter("cuisine")}
        {facetFilter("course")}
        <FilterGroup
          label={m.library_filters_in_a_book()}
          action={m.library_filters_choose_a_book()}
          searchNoun={m.search_books_noun()}
          values={props.vocabulary
            .books()
            .map((book: BookSummary) => ({ id: book.id, label: book.title }))}
          load={booksLoader()}
          selected={props.filters.book}
          onToggle={(value) => toggle("book", value)}
        />
      </div>
      <Disclosure
        variant="inline"
        class={styles.more}
        label={m.library_filters_more()}
        open={moreInUse()}
      >
        <div class={styles.moreGroups}>
          <div class={styles.group}>
            <p class={styles.groupTitle}>{facetGroup("dietary").label}</p>
            <FilterPicker
              label={facetGroup("dietary").label}
              action={m.library_filters_choose_a_category({
                value1: facetGroup("dietary").singular,
              })}
              searchNoun={facetGroup("dietary").plural}
              values={[
                ...statedDiets.map((diet) => ({
                  id: `stated:${diet}`,
                  label: `${dietName(diet)} · ${m.library_filters_stated_by_the_recipe()}`,
                })),
                ...facetValues("dietary").map((value) => ({
                  ...value,
                  id: `category:${value.id}`,
                })),
              ]}
              selected={[
                ...(props.filters.stated ?? []).map((diet) => `stated:${diet}`),
                ...props.filters.dietary.map((id) => `category:${id}`),
              ]}
              onToggle={(id) =>
                id.startsWith("stated:")
                  ? toggleStated(id.slice(7) as Diet)
                  : toggle("dietary", id.slice(9))
              }
            />
            <p class={styles.hint}>
              {m.library_filters_diet_hint()}{" "}
              <a
                class={styles.manual}
                href="https://docs.savorum.app/guide/organize-library/#search-for-a-recipe"
                target="_blank"
                rel="noreferrer"
              >
                {m.library_filters_diet_manual()}
              </a>
            </p>
          </div>
          {facetFilter("method")}
          <FilterGroup
            label={m.library_filters_taken_from_a_cookbook()}
            action={m.library_filters_choose_a_cookbook()}
            searchNoun={m.search_cookbooks_noun()}
            values={publications()}
            load={booksLoader(true)}
            selected={props.filters.source}
            onToggle={(value) => toggle("source", value)}
          />
        </div>
      </Disclosure>
    </div>
  )
}
