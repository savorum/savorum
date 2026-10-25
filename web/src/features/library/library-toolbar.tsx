import { BookmarkPlus, CheckSquare } from "lucide-solid"
import { Show, createMemo, untrack } from "solid-js"
import { css } from "styled-system/css"

import type { RecipeSort } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Select } from "@/components/ui/select"
import { FilterButton } from "@/features/library/filter-button"
import { FilterSummary } from "@/features/library/filter-summary"
import { activeFilterCount, describeQuery } from "@/lib/library-query"
import { formatNumber } from "@/lib/locale-format"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import type { LibraryQuery } from "./create-library-query"
import type { LibrarySelection } from "./create-library-selection"
import { sortChoices, sortLabels } from "./library-search"

export function LibraryToolbar(props: { query: LibraryQuery; selection: LibrarySelection }) {
  const {
    views,
    vocabulary,
    filtersOpen,
    setFiltersOpen,
    filters,
    applyFilters,
    sort,
    text,
    editSearch,
    entries,
    total,
    narrowed,
    savableSearch,
    setNamingSearch,
    currentQuery,
    labelFor,
  } = untrack(() => props.query)
  const { selecting, enterSelection } = untrack(() => props.selection)
  let filterControls: HTMLDivElement | undefined
  const organizable = createMemo(
    (visible) =>
      visible ||
      total() > 0 ||
      narrowed() ||
      (settled(views) ?? []).length > 0 ||
      activeFilterCount(filters()) > 0,
    false,
  )
  return (
    <Show when={organizable()}>
      <div class={styles.container}>
        <div class={styles.tools}>
          <div
            class={styles.filter}
            ref={(element) => {
              filterControls = element
            }}
          >
            <FilterButton
              filters={filters()}
              open={filtersOpen()}
              onToggleOpen={() => setFiltersOpen((open) => !open)}
            />
          </div>
          <Select
            class={styles.sort}
            size="sm"
            aria-label={m.library_toolbar_sort_recipes()}
            value={sort()}
            onValueChange={(value) => editSearch({ sort: value as RecipeSort })}
            items={sortChoices(text()).map((value) => ({ value, label: sortLabels[value] }))}
          />
          <Show when={activeFilterCount(filters()) > 0 || savableSearch()}>
            <div class={styles.applied}>
              <FilterSummary
                filters={filters()}
                vocabulary={vocabulary}
                onChange={applyFilters}
                onEmpty={() => filterControls?.querySelector("button")?.focus()}
                actions={
                  <Show when={savableSearch()}>
                    <Button
                      size="sm"
                      variant="plain"
                      class={styles.save}
                      aria-label={m.library_toolbar_save_this_search()}
                      title={m.library_toolbar_save_this_search()}
                      onClick={() => setNamingSearch(describeQuery(currentQuery(), labelFor))}
                    >
                      <BookmarkPlus size={16} aria-hidden="true" />
                      <span class={styles.saveShort}>{m.save_search_dialog_save()}</span>
                      <span class={styles.saveFull}>{m.library_toolbar_save_this_search()}</span>
                    </Button>
                  </Show>
                }
              />
            </div>
          </Show>
          <span class={styles.count} aria-live="polite">
            {formatNumber(total())} {total() === 1 ? "recipe" : "recipes"}
          </span>
          <div class={styles.select}>
            <Show when={!selecting() && entries().length > 0}>
              <Button size="sm" variant="plain" onClick={() => enterSelection()}>
                <CheckSquare aria-hidden="true" size={14} /> {m.library_toolbar_select()}
              </Button>
            </Show>
          </div>
        </div>
      </div>
    </Show>
  )
}
const styles = {
  container: css({ containerName: "library-tools", containerType: "inline-size" }),
  applied: css({ gridArea: "applied", minWidth: "0" }),
  saveShort: css({ display: { base: "inline", md: "none" } }),
  saveFull: css({ display: { base: "none", md: "inline" } }),
  save: css({
    flexShrink: "0",
    maxWidth: "100%",
    whiteSpace: "normal",
    textAlign: "start",
    height: "auto",
    minHeight: "11",
  }),
  tools: css({
    display: "grid",
    gridTemplateColumns: { base: "auto minmax(0, 1fr)", md: "auto auto minmax(0, 1fr) auto" },
    gridTemplateAreas: {
      base: '"filter sort" "applied applied" "count select"',
      md: '"filter sort count select" "applied applied applied applied"',
    },
    alignItems: "center",
    columnGap: "3",
    rowGap: "2",
    marginTop: "4",
    "@container library-tools (max-width: 17rem)": {
      gridTemplateColumns: "minmax(0, 1fr) auto",
      gridTemplateAreas: '"filter filter" "sort sort" "applied applied" "count select"',
    },
  }),
  filter: css({ gridArea: "filter", "& > button": { minHeight: "11" } }),
  select: css({ gridArea: "select", justifySelf: "end" }),
  sort: css({
    gridArea: "sort",
    justifySelf: "end",
    width: { base: "100%", md: "auto" },
    minWidth: "0",
    maxWidth: "100%",
    "& [data-part=trigger]": { minHeight: "11", height: "auto", paddingBlock: "2" },
    "& [data-part=value-text]": { whiteSpace: "normal" },
  }),
  count: css({
    gridArea: "count",
    justifySelf: { base: "start", md: "end" },
    color: "fg.muted",
    fontSize: "sm",
    whiteSpace: "normal",
  }),
}
