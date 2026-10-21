import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import { For, createRoot, untrack } from "solid-js"
import { createStore } from "solid-js/store"
import { afterEach, expect, test, vi } from "vitest"

import { createGroupedReorder, createReorder, moveBetween } from "./reorder"

afterEach(cleanup)

test("grouped move actions cross empty lists, keep row data, and respect outer boundaries", () => {
  createRoot((dispose) => {
    try {
      const [state, setState] = createStore({
        groups: [
          [
            { id: "a", note: "First" },
            { id: "b", note: "Keep this note" },
          ],
          [],
          [{ id: "c", note: "Last" }],
        ],
      })
      const untouched = untrack(() => state.groups[2])
      const setGroup = vi.fn((group: number, rows: { id: string; note: string }[]) =>
        setState("groups", group, rows),
      )
      const onMoved = vi.fn()
      const reorder = createGroupedReorder({ groups: () => state.groups, setGroup, onMoved })
      expect(reorder.canMoveBy({ group: 0, index: 0 }, -1)).toBe(false)
      expect(reorder.canMoveBy({ group: 2, index: 0 }, 1)).toBe(false)
      reorder.moveBy({ group: 0, index: 0 }, -1)
      expect(onMoved).not.toHaveBeenCalled()

      reorder.moveBy({ group: 0, index: 1 }, 1)
      expect(untrack(() => state.groups.map((rows) => rows.map((row) => row.id)))).toEqual([
        ["a"],
        ["b"],
        ["c"],
      ])
      expect(onMoved).toHaveBeenLastCalledWith(
        { id: "b", note: "Keep this note" },
        { group: 1, index: 0 },
      )
      expect(setGroup.mock.calls.map(([group]) => group)).toEqual([0, 1])
      expect(untrack(() => state.groups[2])).toBe(untouched)

      reorder.moveBy({ group: 1, index: 0 }, -1)
      expect(untrack(() => state.groups[0].map((row) => row.id))).toEqual(["a", "b"])
      setGroup.mockClear()
      reorder.moveBy({ group: 0, index: 0 }, 1)
      expect(untrack(() => state.groups[0].map((row) => row.id))).toEqual(["b", "a"])
      expect(setGroup.mock.calls.map(([group]) => group)).toEqual([0])
      reorder.moveBy({ group: 2, index: 0 }, -1)
      expect(untrack(() => state.groups[1].map((row) => row.id))).toEqual(["c"])
      expect(reorder.canMoveBy({ group: 1, index: 0 }, 1)).toBe(true)
    } finally {
      dispose()
    }
  })
})

test("grouped dragging and move actions use the same row transfer", () => {
  const [state, setState] = createStore({ groups: [["a", "b"], ["c"], ["untouched"]] })
  const onMoved = vi.fn()
  render(() => {
    const reorder = createGroupedReorder({
      groups: () => state.groups,
      setGroup: (group, rows) => setState("groups", group, rows),
      onMoved,
    })
    return (
      <>
        <span
          data-testid="handle"
          {...reorder.handle(
            () => ({ group: 0, index: 1 }),
            () => "b",
          )}
        />
        <div
          data-testid="target"
          {...reorder.group(
            () => 1,
            () => state.groups[1].length,
          )}
        />
      </>
    )
  })
  const handle = document.querySelector('[data-testid="handle"]')!
  const target = document.querySelector('[data-testid="target"]')!
  fireEvent.dragStart(handle)
  fireEvent.dragOver(target)
  fireEvent.drop(target)
  expect(untrack(() => state.groups)).toEqual([["a"], ["c", "b"], ["untouched"]])
  expect(onMoved).toHaveBeenCalledExactlyOnceWith("b", { group: 1, index: 1 })
})

test("a row moves within its list and between lists", () => {
  const lists = [["a", "b", "c"], ["x"]]
  expect(moveBetween(lists, { group: 0, index: 0 }, { group: 0, index: 2 })).toEqual([
    ["b", "c", "a"],
    ["x"],
  ])
  expect(moveBetween(lists, { group: 0, index: 1 }, { group: 1, index: 0 })).toEqual([
    ["a", "c"],
    ["b", "x"],
  ])
  expect(moveBetween(lists, { group: 1, index: 0 }, { group: 0, index: 3 })).toEqual([
    ["a", "b", "c", "x"],
    [],
  ])
  expect(lists).toEqual([["a", "b", "c"], ["x"]])
})

function lists(acrossGroups: boolean) {
  const onMove = vi.fn()
  const reorder = createReorder({ acrossGroups, onMove })
  const groups = [["a", "b"], ["c"]]
  const screen = render(() => (
    <For each={groups}>
      {(list, group) => (
        <ul data-testid={`list-${group()}`} {...reorder.group(group, () => list.length)}>
          <For each={list}>
            {(id, index) => (
              <li
                data-testid={id}
                {...reorder.row(
                  () => ({ group: group(), index: index() }),
                  () => id,
                )}
              >
                <span
                  data-testid={`grip-${id}`}
                  {...reorder.handle(
                    () => ({ group: group(), index: index() }),
                    () => id,
                  )}
                />
              </li>
            )}
          </For>
        </ul>
      )}
    </For>
  ))
  return { screen, onMove }
}

test("dropping a carried row on another reports both places", () => {
  const { screen, onMove } = lists(false)
  fireEvent.dragStart(screen.getByTestId("grip-a"))
  expect(screen.getByTestId("a").dataset.dragging).toBe("true")
  fireEvent.dragOver(screen.getByTestId("b"))
  expect(screen.getByTestId("b").dataset.drop).toBe("true")
  fireEvent.drop(screen.getByTestId("b"))
  expect(onMove).toHaveBeenCalledWith({ group: 0, index: 0 }, { group: 0, index: 1 })
  expect(screen.getByTestId("b").dataset.drop).toBeUndefined()
})

test("a row stays in its own list unless lists may exchange rows", () => {
  const within = lists(false)
  fireEvent.dragStart(within.screen.getByTestId("grip-a"))
  fireEvent.drop(within.screen.getByTestId("c"))
  expect(within.onMove).not.toHaveBeenCalled()
  cleanup()

  const across = lists(true)
  fireEvent.dragStart(across.screen.getByTestId("grip-a"))
  fireEvent.drop(across.screen.getByTestId("list-1"))
  expect(across.onMove).toHaveBeenCalledWith({ group: 0, index: 0 }, { group: 1, index: 1 })
})
