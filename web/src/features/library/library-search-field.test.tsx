import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { emptyFilters } from "@/lib/library-query"

import { LibrarySearchField } from "./library-search-field"

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})
const noVocabulary = {
  tags: () => [],
  facets: () => [],
  books: () => [],
  label: (id: string) => id,
}

function setup() {
  vi.useFakeTimers()
  const [text, setText] = createSignal("")
  const navigate = vi.fn()
  const screen = render(() => (
    <LibrarySearchField
      text={text}
      editSearch={navigate}
      vocabulary={noVocabulary}
      filters={() => emptyFilters}
      onNarrow={() => {}}
    />
  ))
  const input = screen.getByRole("combobox") as HTMLInputElement
  return { ...screen, getSearchbox: () => screen.getByRole("combobox"), input, setText, navigate }
}

test("rapid typing keeps the same input, focus, caret and newest value through delayed navigation and blur", () => {
  const { input, getSearchbox, setText, navigate } = setup()
  input.focus()
  fireEvent.input(input, { target: { value: "soup" } })
  vi.advanceTimersByTime(200)
  expect(navigate).toHaveBeenLastCalledWith({ q: "soup" }, true)
  fireEvent.input(input, { target: { value: "soup stock" } })
  input.setSelectionRange(4, 4)
  vi.advanceTimersByTime(200)
  setText("soup")
  expect(getSearchbox()).toBe(input)
  expect(document.activeElement).toBe(input)
  expect(input.selectionStart).toBe(4)
  expect(input.value).toBe("soup stock")
  expect(navigate).toHaveBeenLastCalledWith({ q: "soup stock" }, true)
  input.blur()
  setText("soup stock")
  expect(input.value).toBe("soup stock")
  expect(getSearchbox()).toBe(input)
})

test("composition retains the complete local value and waits until composition ends before navigation", () => {
  const { input, navigate, getSearchbox } = setup()
  input.focus()
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "味噌" }, isComposing: true })
  input.setSelectionRange(2, 2)
  vi.advanceTimersByTime(500)
  expect(navigate).not.toHaveBeenCalled()
  expect(document.activeElement).toBe(input)
  expect(input.selectionStart).toBe(2)
  expect(getSearchbox()).toBe(input)
  fireEvent.compositionEnd(input, { data: "味噌" })
  vi.advanceTimersByTime(200)
  expect(navigate).toHaveBeenLastCalledWith({ q: "味噌" }, true)
  expect(input.value).toBe("味噌")
})

test("saved-view and browser navigation synchronize an idle field; route cleanup stops pending debounce", () => {
  const { input, setText, navigate, unmount } = setup()
  setText("saved search")
  expect(input.value).toBe("saved search")
  setText("back navigation")
  expect(input.value).toBe("back navigation")
  input.focus()
  fireEvent.input(input, { target: { value: "new draft" } })
  unmount()
  vi.advanceTimersByTime(500)
  expect(navigate).not.toHaveBeenCalled()
})
