import { cleanup, fireEvent, render, within } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test } from "vitest"

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
  input.setSelectionRange(3, 3)
  fireEvent.input(input, { target: { value: "WeeXkend" } })
  input.setSelectionRange(4, 4)
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
