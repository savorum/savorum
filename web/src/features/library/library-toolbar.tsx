import { CheckSquare } from "lucide-solid"
import { Show, untrack } from "solid-js"
import { css } from "styled-system/css"

import type { RecipeSort } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Select } from "@/components/ui/select"
import { FilterButton } from "@/features/library/filter-button"
import { FilterSummary } from "@/features/library/filter-summary"
import { activeFilterCount } from "@/lib/library-query"
import { formatNumber } from "@/lib/locale-format"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import type { LibraryQuery } from "./create-library-query"
import type { LibrarySelection } from "./create-library-selection"
import { sortLabels, sorts } from "./library-search"

export function LibraryToolbar(props: { query: LibraryQuery; selection: LibrarySelection }) {
  const {
    views,
    vocabulary,
    filtersOpen,
    setFiltersOpen,
    filters,
    applyFilters,
    sort,
    editSearch,
    entries,
    total,
    narrowed,
  } = untrack(() => props.query)
  const { selecting, enterSelection } = untrack(() => props.selection)
  const organizable = () =>
    total() > 0 ||
    narrowed() ||
    (settled(views) ?? []).length > 0 ||
    activeFilterCount(filters()) > 0
  return (
    <Show when={organizable()}>
      <div class={styles.tools}>
        <div class={styles.start}>
          <FilterButton
            filters={filters()}
            open={filtersOpen()}
            onToggleOpen={() => setFiltersOpen((open) => !open)}
          />
          <span class={styles.count} aria-live="polite">
            {formatNumber(total())} {total() === 1 ? "recipe" : "recipes"}
          </span>
        </div>
        <div class={styles.end}>
          <Show when={!selecting() && entries().length > 0}>
            <Button size="sm" variant="plain" onClick={() => enterSelection()}>
              <CheckSquare aria-hidden="true" size={14} /> {m.library_toolbar_select()}
            </Button>
          </Show>
          <Select
            class={styles.sort}
            size="sm"
            aria-label={m.library_toolbar_sort_recipes()}
            value={sort()}
            onValueChange={(value) => editSearch({ sort: value as RecipeSort })}
            items={sorts.map((value) => ({ value, label: sortLabels[value] }))}
          />
        </div>
      </div>
      <FilterSummary filters={filters()} vocabulary={vocabulary} onChange={applyFilters} />
    </Show>
  )
}
const styles = {
  tools: css({
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "2",
    marginTop: "4",
  }),
  start: css({ display: "flex", alignItems: "center", gap: "3", minWidth: "0" }),
  end: css({ display: "flex", alignItems: "center", gap: "2", minWidth: "0" }),
  sort: css({ width: "auto", minWidth: "10.5rem", maxWidth: "100%" }),
  count: css({ color: "fg.muted", fontSize: "sm", whiteSpace: "nowrap" }),
}
