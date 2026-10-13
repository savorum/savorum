import { cleanup, render } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test } from "vitest"

import { WhenSet } from "./when-set"

afterEach(cleanup)

test("a field is absent until its value is set and stays once it has been", () => {
  const [value, setValue] = createSignal<string | undefined>(undefined)
  const view = render(() => (
    <WhenSet value={value()}>
      <input aria-label="As written" />
    </WhenSet>
  ))
  expect(view.queryByLabelText("As written")).toBeNull()
  setValue("About four bowls")
  expect(view.queryByLabelText("As written")).not.toBeNull()
  setValue("")
  expect(view.queryByLabelText("As written")).not.toBeNull()
})

test("a field that starts with a value is shown", () => {
  const view = render(() => (
    <WhenSet value="Serves four">
      <input aria-label="As written" />
    </WhenSet>
  ))
  expect(view.queryByLabelText("As written")).not.toBeNull()
})
