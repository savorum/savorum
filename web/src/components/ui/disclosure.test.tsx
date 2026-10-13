import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test } from "vitest"

import { Disclosure } from "./disclosure"
import { Input } from "./input"

afterEach(cleanup)

test("disclosures use Ark controls and retain edited input through typing, composition and toggles", async () => {
  const screen = render(() => {
    const [value, setValue] = createSignal("")
    return (
      <Disclosure label="Details" open>
        <Input
          aria-label="Recipe note"
          value={value()}
          onInput={(event) => setValue(event.currentTarget.value)}
        />
      </Disclosure>
    )
  })
  const trigger = screen.getByRole("button", { name: "Details" })
  const input = screen.getByRole("textbox", { name: "Recipe note" }) as HTMLInputElement
  expect(screen.container.querySelector("details, summary")).toBeNull()
  input.focus()
  for (const value of ["s", "sa", "salt"]) {
    fireEvent.input(input, { target: { value } })
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(value.length)
    expect(input.value).toBe(value)
  }
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "salt 味噌" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "味噌" })
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("salt 味噌")
  fireEvent.click(trigger)
  await waitFor(() => expect(trigger.getAttribute("aria-expanded")).toBe("false"))
  expect(screen.getByLabelText("Recipe note")).toBe(input)
  fireEvent.click(trigger)
  await waitFor(() => expect(trigger.getAttribute("aria-expanded")).toBe("true"))
  expect(screen.getByRole("textbox", { name: "Recipe note" })).toBe(input)
  expect(input.value).toBe("salt 味噌")
})
