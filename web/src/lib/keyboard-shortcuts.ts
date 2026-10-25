import { createSignal } from "solid-js"

import { keyTaken } from "./keyboard"
import { m } from "./messages"

const preferenceKey = "savorum.keyboard-shortcuts.v1"
const [enabled, setEnabled] = createSignal(true)
export const shortcutsEnabled = enabled

export function setShortcutsEnabled(value: boolean): void {
  setEnabled(value)
  try {
    localStorage.setItem(preferenceKey, value ? "on" : "off")
  } catch {
    // The choice still applies for this session when browser storage is unavailable.
  }
}

export const shortcutGroups = [
  {
    label: () => m.keyboard_shortcuts_library(),
    items: [
      { id: "search", key: "/", label: () => m.library_search_field_search_recipes(), focus: true },
      { id: "add-recipes", key: "n", label: () => m.library_page_add_recipes() },
    ],
  },
  {
    label: () => m.keyboard_shortcuts_reading(),
    items: [
      { id: "recipe-edit", key: "e", label: () => m.recipe_page_edit() },
      { id: "recipe-cook", key: "c", label: () => m.create_recipe_reader_cook() },
      { id: "recipe-print", key: "p", label: () => m.recipe_page_print() },
    ],
  },
  {
    label: () => m.keyboard_shortcuts_plan(),
    items: [
      { id: "plan-new", key: "n", label: () => m.plan_new_meal() },
      { id: "plan-today", key: "t", label: () => m.plan_today() },
      { id: "plan-previous", key: "[", label: () => m.keyboard_shortcuts_previous() },
      { id: "plan-next", key: "]", label: () => m.keyboard_shortcuts_next() },
    ],
  },
  {
    label: () => m.keyboard_shortcuts_shopping(),
    items: [{ id: "shopping-add", key: "a", label: () => m.keyboard_shortcuts_add(), focus: true }],
  },
  {
    label: () => m.keyboard_shortcuts_imports(),
    items: [{ id: "import-recipes", key: "n", label: () => m.import_menu_import() }],
  },
  {
    label: () => m.cooking_now(),
    items: [
      { id: "cooking-next", key: "j", label: () => m.cooking_mode_next_step() },
      { id: "cooking-previous", key: "k", label: () => m.cooking_mode_previous_step() },
      { id: "cooking-timers", key: "t", label: () => m.cooking_step_timers() },
    ],
  },
]

const shortcuts = shortcutGroups.flatMap((group) => group.items)
export function shortcutAria(id: string): string | undefined {
  return enabled() ? shortcuts.find((shortcut) => shortcut.id === id)?.key : undefined
}

export function listenForShortcuts(onHelp: () => void): () => void {
  const load = () => {
    try {
      setEnabled(localStorage.getItem(preferenceKey) !== "off")
    } catch {
      /* Keep the session choice. */
    }
  }
  load()
  const storage = (event: StorageEvent) => {
    if (event.key === preferenceKey || event.key === null) load()
  }
  const keydown = (event: KeyboardEvent) => {
    if (!enabled() || keyTaken(event)) return
    if (event.key === "?") {
      event.preventDefault()
      onHelp()
      return
    }
    for (const shortcut of shortcuts) {
      const letter = /^[a-z]$/.test(shortcut.key)
      if (
        (letter && (event.shiftKey || event.ctrlKey || event.altKey)) ||
        (letter ? event.key.toLowerCase() : event.key) !== shortcut.key
      )
        continue
      const target = [
        ...document.querySelectorAll<HTMLElement>(`[data-shortcut="${shortcut.id}"]`),
      ].find(
        (element) =>
          element.getClientRects().length && !element.matches(":disabled, [aria-disabled=true]"),
      )
      if (!target) continue
      event.preventDefault()
      target.focus({ preventScroll: true })
      if (!("focus" in shortcut && shortcut.focus)) target.click()
      return
    }
  }
  window.addEventListener("keydown", keydown)
  window.addEventListener("storage", storage)
  return () => {
    window.removeEventListener("keydown", keydown)
    window.removeEventListener("storage", storage)
  }
}
