import { cleanup, fireEvent, render, within } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import type { ChoiceLoader } from "@/components/label-choices"

import { FilterPicker } from "./filter-picker"

afterEach(cleanup)

function picker() {
  const [selected, setSelected] = createSignal<string[]>([])
  const screen = render(() => (
    <FilterPicker
      label="Tags"
      action="Choose a tag"
      searchNoun="tags"
      values={Array.from({ length: 15 }, (_, index) => ({
        id: String(index),
        label: `Weekend ${index}`,
      }))}
      selected={selected()}
      onToggle={(id) =>
        setSelected((current) =>
          current.includes(id) ? current.filter((value) => value !== id) : [...current, id],
        )
      }
    />
  ))
  return { ...screen, ...within(document.body), selected }
}

test("filter search retains its element, focus and caret during typing and composition", async () => {
  const screen = picker()
  fireEvent.click(screen.getByRole("button", { name: "Choose a tag" }))
  const input = (await screen.findByRole("searchbox", { name: "Search tags" })) as HTMLInputElement
  input.focus()
  for (const value of ["W", "We", "Weekend"]) {
    fireEvent.input(input, { target: { value } })
    expect(screen.getByRole("searchbox")).toBe(input)
    expect(document.activeElement).toBe(input)
    expect(input.value).toBe(value)
  }
  input.value = "WeeXkend"
  input.setSelectionRange(4, 4)
  fireEvent.input(input)
  expect(input.selectionStart).toBe(4)
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "週末" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "週末" })
  expect(screen.getByRole("searchbox")).toBe(input)
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("週末")
})

test("selected filter choices remain mounted and can be removed in the same picker", async () => {
  const screen = picker()
  fireEvent.click(screen.getByRole("button", { name: "Choose a tag" }))
  const choice = await screen.findByRole("checkbox", { name: "Weekend 1" })
  choice.focus()
  fireEvent.click(choice)
  expect(screen.selected()).toEqual(["1"])
  expect(screen.getByRole("checkbox", { name: "Weekend 1" })).toBe(choice)
  expect(document.activeElement).toBe(choice)
  expect((choice as HTMLInputElement).checked).toBe(true)
  fireEvent.click(choice)
  expect(screen.selected()).toEqual([])
  expect((choice as HTMLInputElement).checked).toBe(false)
})

function remotePicker(load: ChoiceLoader) {
  const toggle = vi.fn()
  render(() => (
    <FilterPicker
      label="Books"
      action="Choose a book"
      searchNoun="books"
      values={[]}
      selected={[]}
      load={load}
      onToggle={toggle}
    />
  ))
  return { ...within(document.body), toggle }
}

test("a new remote search removes stale choices until its results arrive", async () => {
  let complete!: (page: Awaited<ReturnType<ChoiceLoader>>) => void
  const next = new Promise<Awaited<ReturnType<ChoiceLoader>>>((resolve) => {
    complete = resolve
  })
  const screen = remotePicker(async (query) =>
    query ? next : { items: [{ id: "old", label: "Old book" }], has_more: false },
  )
  fireEvent.click(screen.getByRole("button", { name: "Choose a book" }))
  await screen.findByRole("checkbox", { name: "Old book" })
  fireEvent.input(screen.getByRole("searchbox", { name: "Search books" }), {
    target: { value: "New" },
  })
  expect(screen.queryByRole("checkbox", { name: "Old book" })).toBeNull()
  expect(screen.getByText("Loading…")).toBeTruthy()
  expect(screen.toggle).not.toHaveBeenCalled()
  complete({ items: [{ id: "new", label: "New book" }], has_more: false })
  fireEvent.click(await screen.findByRole("checkbox", { name: "New book" }))
  expect(screen.toggle).toHaveBeenCalledExactlyOnceWith("new")
})

test("an empty remote page explains its result while another page is available", async () => {
  const screen = remotePicker(async (_, offset) =>
    offset === 0
      ? { items: [], has_more: true }
      : { items: [{ id: "publication", label: "Later cookbook" }], has_more: false },
  )
  fireEvent.click(screen.getByRole("button", { name: "Choose a book" }))
  expect(await screen.findByText("Nothing here matches that.")).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Next" }))
  expect(await screen.findByRole("checkbox", { name: "Later cookbook" })).toBeTruthy()
})
