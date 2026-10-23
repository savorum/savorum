import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { StepDetails } from "./step-details"
afterEach(cleanup)

test("step quantities keep decimals and their unit through typing and composition", async () => {
  const changed = vi.fn()
  render(() => (
    <StepDetails
      step={{
        id: "mix",
        text: "Mix",
        ingredient_uses: [
          {
            ingredient_ref: "flour",
            quantity: { kind: "simple", amount: { value: "1" }, unit: "mass.gram" },
          },
        ],
      }}
      position={1}
      recipeID="cake"
      photos={[]}
      ingredients={[{ id: "flour", name: "Flour", label: "Flour" }]}
      style="us"
      onChange={changed}
      onClose={() => {}}
      onAttachPhotos={() => Promise.resolve([])}
    />
  ))
  const input = (await screen.findByRole("textbox", {
    name: "Amount of Flour used",
  })) as HTMLInputElement
  input.focus()
  for (const value of ["1", "1.", "1.2", "1.25"]) {
    fireEvent.input(input, { target: { value } })
    expect(input.value).toBe(value)
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(value.length)
  }
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "1.25" }, isComposing: true })
  fireEvent.compositionEnd(input)
  fireEvent.blur(input)
  fireEvent.click(screen.getByRole("button", { name: "Done" }))
  expect(changed.mock.calls[0][0].ingredient_uses[0].quantity).toEqual({
    kind: "simple",
    amount: { value: "5/4" },
    unit: "mass.gram",
  })
})
