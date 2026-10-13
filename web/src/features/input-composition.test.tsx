import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { ImportSourceDialogs } from "./import-review/import-source-dialogs"
import { SaveSearchDialog } from "./library/save-search-dialog"

afterEach(cleanup)

function compose(input: HTMLInputElement, submit: ReturnType<typeof vi.fn>) {
  input.focus()
  for (const value of ["に", "にほん"]) {
    fireEvent.input(input, { target: { value } })
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(value.length)
  }
  fireEvent.compositionStart(input)
  fireEvent.keyDown(input, { key: "Enter", isComposing: true })
  fireEvent.keyDown(input, { key: "Enter", keyCode: 229 })
  expect(submit).not.toHaveBeenCalled()
  fireEvent.input(input, { target: { value: "日本料理" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "日本料理" })
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("日本料理")
  expect(input.selectionStart).toBe(4)
  fireEvent.keyDown(input, { key: "Enter" })
  expect(submit).toHaveBeenCalledTimes(1)
}

test("saved search composition retains the input and waits for a separate Enter", async () => {
  const save = vi.fn()
  render(() => {
    const [namingSearch, setNamingSearch] = createSignal<string | undefined>("")
    const query = {
      namingSearch,
      setNamingSearch,
      savingSearch: () => false,
      saveSearchError: () => undefined,
      saveSearch: save,
    }
    return <SaveSearchDialog query={query} />
  })
  const input = (await screen.findByRole("textbox", {
    name: "Name for this search",
  })) as HTMLInputElement
  compose(input, save)
  expect(screen.getByRole("textbox", { name: "Name for this search" })).toBe(input)
  expect(save).toHaveBeenCalledWith("日本料理")
})

test("URL composition retains the input and waits for a separate Enter", async () => {
  const stage = vi.fn()
  render(() => {
    const [urlImport, setURLImport] = createSignal("")
    const workflow = {
      urlImport,
      setURLImport,
      previewing: () => false,
      urlImportOpen: () => true,
      textImportOpen: () => false,
      editingTextItem: () => undefined,
      urlImportError: () => undefined,
      setURLImportError: vi.fn(),
      stageImportURL: stage,
      setURLImportOpen: vi.fn(),
      setTextImportOpen: vi.fn(),
      setEditingTextItem: vi.fn(),
      stageImportFiles: vi.fn(),
      applyEditedText: vi.fn(),
      cancelStaging: vi.fn(),
    }
    return <ImportSourceDialogs workflow={workflow} />
  })
  const input = (await screen.findByRole("textbox", { name: "Recipe URL" })) as HTMLInputElement
  compose(input, stage)
  expect(screen.getByRole("textbox", { name: "Recipe URL" })).toBe(input)
})
