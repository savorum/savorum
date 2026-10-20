import { Search } from "lucide-solid"
import {
  For,
  Show,
  type Accessor,
  createEffect,
  createMemo,
  createResource,
  createSignal,
  onCleanup,
} from "solid-js"
import { css } from "styled-system/css"

import { LabelSwatch } from "@/components/label-swatch"
import { Input } from "@/components/ui/input"
import type { LibraryFilters } from "@/lib/library-query"
import { type SearchSuggestion, searchSuggestions, withSuggestion } from "@/lib/search-suggestions"

import { m } from "../../lib/messages.ts"
import type { LibraryVocabulary } from "./create-library-vocabulary"
import type { UpdateLibrarySearch } from "./library-search"

const listboxID = "library-search-suggestions"

export function LibrarySearchField(props: {
  text: Accessor<string>
  editSearch: UpdateLibrarySearch
  vocabulary: LibraryVocabulary
  filters: Accessor<LibraryFilters>
  onNarrow: (filters: LibraryFilters) => void
}) {
  const text = () => props.text()
  const editSearch: UpdateLibrarySearch = (next, replace) => props.editSearch(next, replace)
  const [typed, setTyped] = createSignal(text())
  const [searchFocused, setSearchFocused] = createSignal(false)
  const [composing, setComposing] = createSignal(false)
  const [dismissed, setDismissed] = createSignal(false)
  const [active, setActive] = createSignal(-1)
  let pendingSearch: string | undefined

  /** Filter suggestions for what is being typed; return still searches text. */
  const [bookMatches] = createResource(
    () => (composing() ? false : typed()),
    async (query) => props.vocabulary.searchBooks?.(query, 0),
  )
  createEffect(() => {
    void props.vocabulary.resolveBooks?.([
      ...(props.filters().book ?? []),
      ...(props.filters().source ?? []),
    ])
  })
  const matches = createMemo<SearchSuggestion[]>(() =>
    composing()
      ? []
      : searchSuggestions(
          {
            ...props.vocabulary,
            books: () =>
              bookMatches.loading ? [] : (bookMatches.latest?.items ?? props.vocabulary.books()),
          },
          typed(),
          props.filters(),
        ),
  )
  // Dismissing hides the list without forgetting it, so an arrow key brings
  // back what was there rather than nothing.
  const suggestions = () => (dismissed() ? [] : matches())
  const open = () => searchFocused() && suggestions().length > 0
  const accept = (suggestion: SearchSuggestion) => {
    props.onNarrow(withSuggestion(props.filters(), suggestion))
    // The words became a filter, so leaving them in the box would narrow the
    // library twice by the same thing.
    setTyped("")
    commitSearch("")
    setActive(-1)
  }
  /**
   * Moves through the list, with the typed words themselves as the position
   * before the first suggestion, so a reader can always arrow back to what they
   * wrote and search for it.
   */
  const move = (direction: number) => {
    const staticCount = matches().length
    if (staticCount === 0) return
    setActive((current) => {
      const next = current + direction
      if (next < -1) return staticCount - 1
      return next >= staticCount ? -1 : next
    })
  }

  const onKeyDown = (event: KeyboardEvent) => {
    if (composing()) return
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      if (matches().length === 0) return
      event.preventDefault()
      setDismissed(false)
      move(event.key === "ArrowDown" ? 1 : -1)
      return
    }
    if (event.key === "Enter") {
      const chosen = dismissed() ? undefined : matches()[active()]
      if (!chosen) return
      event.preventDefault()
      accept(chosen)
      return
    }
    if (event.key === "Escape" && open()) {
      event.preventDefault()
      setDismissed(true)
      setActive(-1)
    }
  }

  /** Keeps the locally edited value authoritative until the URL catches up. */
  const commitSearch = (value: string) => {
    if (value === pendingSearch || (pendingSearch === undefined && value === text())) {
      return
    }
    pendingSearch = value
    editSearch({ q: value || undefined }, true)
  }

  createEffect(() => {
    const effective = text()
    const focused = searchFocused()
    if (pendingSearch !== undefined) {
      if (effective !== pendingSearch) {
        // A slower navigation must not strand the field at an older query.
        editSearch({ q: pendingSearch || undefined }, true)
        return
      }
      pendingSearch = undefined
    }
    if (focused) return
    setTyped(effective)
  })
  createEffect(() => {
    if (composing()) return
    const staticValue = typed().trim()
    if (staticValue === (pendingSearch ?? text())) return
    const timeout = setTimeout(() => commitSearch(staticValue), 200)
    onCleanup(() => clearTimeout(timeout))
  })

  const finishSearchEditing = () => {
    if (composing()) return
    const value = typed().trim()
    commitSearch(value)
    setTyped(value)
    setSearchFocused(false)
    setActive(-1)
  }

  // Typing again is a new question, so a dismissed list comes back.
  createEffect(() => {
    typed()
    setDismissed(false)
    setActive(-1)
  })

  return (
    <div class={styles.searchWrap}>
      <Search class={styles.searchIcon} aria-hidden="true" size={17} />
      <Input
        class={styles.searchInput}
        type="search"
        variant="subtle"
        maxLength={200}
        role="combobox"
        aria-label={m.library_search_field_search_recipes()}
        aria-autocomplete="list"
        aria-expanded={open()}
        aria-controls={listboxID}
        aria-activedescendant={open() && active() >= 0 ? `${listboxID}-${active()}` : undefined}
        placeholder={m.library_search_field_search_recipes_and_ingredients()}
        value={typed()}
        onFocus={() => setSearchFocused(true)}
        onInput={(event) => setTyped(event.currentTarget.value)}
        onKeyDown={onKeyDown}
        onCompositionStart={() => setComposing(true)}
        onCompositionEnd={(event) => {
          setTyped(event.currentTarget.value)
          setComposing(false)
        }}
        onBlur={finishSearchEditing}
      />
      <Show when={open()}>
        <ul
          class={styles.suggestions}
          id={listboxID}
          role="listbox"
          aria-label={m.library_search_field_narrow_the_library()}
        >
          <For each={suggestions()}>
            {(suggestion, index) => (
              <li
                id={`${listboxID}-${index()}`}
                role="option"
                aria-selected={active() === index()}
                class={`${styles.suggestion} ${active() === index() ? styles.activeSuggestion : ""}`}
                // The list closes on blur, and blur arrives before click; the
                // choice has to be taken before the field can lose focus.
                onMouseDown={(event) => {
                  event.preventDefault()
                  accept(suggestion)
                }}
                onMouseEnter={() => setActive(index())}
              >
                <LabelSwatch color={suggestion.color} size="sm" />
                <span class={styles.suggestionLabel}>{suggestion.label}</span>
                <span class={styles.suggestionGroup}>{suggestion.group}</span>
              </li>
            )}
          </For>
        </ul>
      </Show>
    </div>
  )
}
const styles = {
  searchWrap: css({
    alignItems: "center",
    display: "grid",
    position: "relative",
    minWidth: "0",
    width: { base: "100%", md: "auto" },
    order: { base: "1", md: "0" },
    gridColumn: { base: "1 / -1", md: "auto" },
    gridRow: { base: "2", md: "auto" },
    gridTemplateColumns: "1.75rem minmax(0, 1fr)",
  }),
  searchIcon: css({
    color: "fg.muted",
    gridColumn: "1",
    gridRow: "1",
    marginInlineStart: "3",
    pointerEvents: "none",
    zIndex: "1",
  }),
  searchInput: css({
    gridColumn: "1 / -1",
    gridRow: "1",
    paddingInlineEnd: "4!",
    paddingInlineStart: "11!",
  }),
  suggestions: css({
    background: "bg.surface",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    boxShadow: "lg",
    gridColumn: "1 / -1",
    gridRow: "2",
    listStyle: "none",
    margin: "0",
    marginTop: "1",
    maxHeight: "18rem",
    overflowY: "auto",
    padding: "1",
    position: "absolute",
    top: "100%",
    width: "100%",
    zIndex: "dropdown",
  }),
  suggestion: css({
    alignItems: "center",
    borderRadius: "l1",
    color: "fg.default",
    cursor: "pointer",
    display: "flex",
    fontSize: "sm",
    gap: "2",
    paddingBlock: "1.5",
    paddingInline: "2",
  }),
  activeSuggestion: css({ background: "bg.muted" }),
  suggestionLabel: css({
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  }),
  suggestionGroup: css({ color: "fg.muted", fontSize: "2xs", marginInlineStart: "auto" }),
}
