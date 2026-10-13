import { createSignal } from "solid-js"
import { css } from "styled-system/css"

/** Where a row sits: which list it is in, and its place in that list. */
export interface Slot {
  group: number
  index: number
}

/**
 * Pointer reordering for rows in one or more lists, using the browser's own
 * drag and drop. The handle is for a pointer; every list also offers move
 * buttons, which are the way to reorder from a keyboard or by touch.
 *
 * Spread `handle` on the element that is grabbed and `row` on the element that
 * receives the drop. Spread `group` on a list to accept a row dropped past its
 * last entry.
 */
export function createReorder(options: {
  /** Whether a row may be dropped into a list other than its own. */
  acrossGroups?: boolean
  onMove: (from: Slot, to: Slot) => void
}) {
  const [dragging, setDragging] = createSignal<Slot>()
  const [over, setOver] = createSignal<string>()
  const accepts = (group: number) =>
    dragging() !== undefined && (options.acrossGroups || dragging()?.group === group)
  const finish = () => {
    setDragging(undefined)
    setOver(undefined)
  }

  return {
    /** Props for the element a pointer picks up. */
    handle(slot: () => Slot, id: () => string) {
      return {
        draggable: true as const,
        onDragStart: (event: DragEvent) => {
          event.dataTransfer?.setData("text/plain", id())
          if (event.dataTransfer) event.dataTransfer.effectAllowed = "move"
          // The whole row follows the pointer rather than the handle alone, so
          // what is being carried is legible.
          const row = (event.currentTarget as HTMLElement).closest("[data-reorder-row]")
          if (row instanceof HTMLElement) event.dataTransfer?.setDragImage(row, 16, 16)
          setDragging(slot())
        },
        onDragEnd: finish,
      }
    },
    /** Props for the element a row is dropped on. */
    row(slot: () => Slot, id: () => string) {
      return {
        "data-reorder-row": "",
        get "data-drop"() {
          return over() === id() ? "true" : undefined
        },
        get "data-dragging"() {
          const source = dragging()
          return source?.group === slot().group && source.index === slot().index
            ? "true"
            : undefined
        },
        onDragOver: (event: DragEvent) => {
          if (!accepts(slot().group)) return
          event.preventDefault()
          event.stopPropagation()
          if (event.dataTransfer) event.dataTransfer.dropEffect = "move"
          setOver(id())
        },
        onDragLeave: () => {
          if (over() === id()) setOver(undefined)
        },
        onDrop: (event: DragEvent) => {
          const source = dragging()
          if (!source || !accepts(slot().group)) return
          event.preventDefault()
          event.stopPropagation()
          options.onMove(source, slot())
          finish()
        },
      }
    },
    /** Props for a list, so a row can be dropped after its last entry. */
    group(group: () => number, length: () => number) {
      return {
        onDragOver: (event: DragEvent) => {
          if (!accepts(group())) return
          event.preventDefault()
          if (event.dataTransfer) event.dataTransfer.dropEffect = "move"
        },
        onDrop: (event: DragEvent) => {
          const source = dragging()
          if (!source || !accepts(group())) return
          event.preventDefault()
          const last = source.group === group() ? length() - 1 : length()
          options.onMove(source, { group: group(), index: Math.max(0, last) })
          finish()
        },
      }
    },
  }
}

/**
 * The lists after a row moves from one slot to another, leaving the originals
 * as they were. Within a list the row takes the target's place; into another
 * list it goes in before the target.
 */
export function moveBetween<T>(groups: readonly (readonly T[])[], from: Slot, to: Slot): T[][] {
  const next = groups.map((list) => [...list])
  const [row] = next[from.group].splice(from.index, 1)
  if (row === undefined) return next
  next[to.group].splice(Math.min(to.index, next[to.group].length), 0, row)
  return next
}

/**
 * Marks where a dragged row would land. An outline rather than a border, so
 * nothing on the page moves while a row is carried over it.
 */
export const reorderRow = css({
  "&[data-drop=true]": {
    outline: "2px dashed",
    outlineColor: "accent.default",
    outlineOffset: "3px",
  },
  "&[data-dragging=true]": { opacity: "0.45" },
})
