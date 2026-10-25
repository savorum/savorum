import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import {
  listenForShortcuts,
  setShortcutsEnabled,
  shortcutAria,
  shortcutsEnabled,
} from "@/lib/keyboard-shortcuts"

const stops: (() => void)[] = []
afterEach(() => {
  stops.splice(0).forEach((stop) => stop())
  cleanup()
  localStorage.clear()
  setShortcutsEnabled(true)
  localStorage.clear()
})
function setup() {
  const action = vi.fn()
  const help = vi.fn()
  const view = render(() => (
    <>
      <button
        data-shortcut="recipe-edit"
        aria-keyshortcuts={shortcutAria("recipe-edit")}
        onClick={action}
      >
        Edit
      </button>
      <input aria-label="Notes" />
      <input data-shortcut="search" aria-label="Search" />
    </>
  ))
  for (const element of view.container.querySelectorAll<HTMLElement>("[data-shortcut]"))
    element.getClientRects = () => [{ width: 40, height: 40 }] as unknown as DOMRectList
  stops.push(listenForShortcuts(help))
  return { ...view, action, help }
}
test("shortcuts activate the visible control once and preserve native modifiers and composition", () => {
  const view = setup()
  fireEvent.keyDown(document.body, { key: "e" })
  expect(view.action).toHaveBeenCalledTimes(1)
  expect(document.activeElement).toBe(view.getByRole("button", { name: "Edit" }))
  for (const options of [
    { repeat: true },
    { ctrlKey: true },
    { metaKey: true },
    { altKey: true },
    { shiftKey: true },
    { isComposing: true },
  ])
    fireEvent.keyDown(document.body, { key: "e", ...options })
  expect(view.action).toHaveBeenCalledTimes(1)
  fireEvent.keyDown(document.body, { key: "?", shiftKey: true })
  expect(view.help).toHaveBeenCalledTimes(1)
  fireEvent.keyDown(document.body, { key: "/" })
  expect(document.activeElement).toBe(view.getByRole("textbox", { name: "Search" }))
})
test("typing, editable content, open dialogs and menus own their keys", () => {
  const view = setup()
  const input = view.getByRole("textbox", { name: "Notes" })
  input.focus()
  fireEvent.keyDown(input, { key: "e" })
  expect(document.activeElement).toBe(input)
  const editable = document.createElement("div")
  editable.contentEditable = "true"
  document.body.append(editable)
  Object.defineProperty(editable, "isContentEditable", { value: true })
  fireEvent.keyDown(editable, { key: "e" })
  editable.remove()
  for (const role of ["dialog", "alertdialog", "menu"]) {
    const panel = document.createElement("div")
    panel.setAttribute("role", role)
    panel.setAttribute("data-state", "open")
    document.body.append(panel)
    fireEvent.keyDown(document.body, { key: "e" })
    fireEvent.keyDown(document.body, { key: "?", shiftKey: true })
    panel.remove()
  }
  expect(view.action).not.toHaveBeenCalled()
  expect(view.help).not.toHaveBeenCalled()
})
test("disabling shortcuts persists, removes their ARIA claims and leaves ordinary controls usable", () => {
  const view = setup()
  setShortcutsEnabled(false)
  fireEvent.keyDown(document.body, { key: "e" })
  fireEvent.keyDown(document.body, { key: "?", shiftKey: true })
  expect(view.action).not.toHaveBeenCalled()
  expect(view.help).not.toHaveBeenCalled()
  const edit = view.getByRole("button", { name: "Edit" })
  expect(edit.hasAttribute("aria-keyshortcuts")).toBe(false)
  fireEvent.click(edit)
  expect(view.action).toHaveBeenCalledTimes(1)
  stops.push(listenForShortcuts(view.help))
  expect(shortcutsEnabled()).toBe(false)
  localStorage.setItem("savorum.keyboard-shortcuts.v1", "on")
  window.dispatchEvent(new StorageEvent("storage", { key: "savorum.keyboard-shortcuts.v1" }))
  expect(shortcutsEnabled()).toBe(true)
})
test("disabled and hidden controls are not shortcut targets and listeners are removed on cleanup", () => {
  const view = setup()
  const edit = view.getByRole("button", { name: "Edit" }) as HTMLButtonElement
  edit.disabled = true
  fireEvent.keyDown(document.body, { key: "e" })
  edit.disabled = false
  edit.getClientRects = () => [] as unknown as DOMRectList
  fireEvent.keyDown(document.body, { key: "e" })
  expect(view.action).not.toHaveBeenCalled()
  stops.splice(0).forEach((stop) => stop())
  fireEvent.keyDown(document.body, { key: "?", shiftKey: true })
  expect(view.help).not.toHaveBeenCalled()
})

test("layout punctuation accepts AltGr without intercepting browser modifier chords", () => {
  const view = setup()
  const next = document.createElement("button")
  next.dataset.shortcut = "plan-next"
  next.getClientRects = () => [{ width: 40, height: 40 }] as unknown as DOMRectList
  const action = vi.fn()
  next.addEventListener("click", action)
  view.container.append(next)
  fireEvent.keyDown(document.body, { key: "]", ctrlKey: true, altKey: true })
  expect(action).not.toHaveBeenCalled()
  const altGr = new KeyboardEvent("keydown", { key: "]", ctrlKey: true, altKey: true })
  vi.spyOn(altGr, "getModifierState").mockImplementation((modifier) => modifier === "AltGraph")
  window.dispatchEvent(altGr)
  expect(action).toHaveBeenCalledTimes(1)
})
