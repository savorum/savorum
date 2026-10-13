import { ChevronDown } from "lucide-solid"
import { Show } from "solid-js"

import { Button } from "@/components/ui/button"
import { type LibraryFilters, activeFilterCount } from "@/lib/library-query"
import { formatNumber } from "@/lib/locale-format"

import { m } from "../../lib/messages.ts"
import { styles } from "./filter-styles"

export interface FilterButtonProps {
  filters: LibraryFilters
  open: boolean
  onToggleOpen: () => void
}

export const FilterButton = (props: FilterButtonProps) => {
  const count = () => activeFilterCount(props.filters)
  return (
    <Button variant="outline" size="sm" onClick={props.onToggleOpen} aria-expanded={props.open}>
      {m.library_filters_filters()}{" "}
      <Show when={count() > 0}>
        <span class={styles.count}>{formatNumber(count())}</span>
      </Show>
      <ChevronDown aria-hidden="true" size={15} />
    </Button>
  )
}
