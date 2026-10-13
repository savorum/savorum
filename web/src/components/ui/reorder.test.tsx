import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import { For } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { createReorder, moveBetween } from "./reorder"

afterEach(cleanup)

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
