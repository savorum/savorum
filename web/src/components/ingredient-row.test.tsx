import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { readIngredientLine } from "@/api/ingredient-lines"
import type { components } from "@/api/schema"
import { IngredientReadingContext } from "@/components/ingredient-reading"
import { readingsSettled } from "@/lib/line-readings"

import { IngredientRow } from "./ingredient-row"

vi.mock("@/api/ingredient-lines", () => ({ readIngredientLine: vi.fn() }))

type Ingredient = components["schemas"]["Ingredient"]

afterEach(() => {
  cleanup()
  vi.mocked(readIngredientLine).mockReset()
})

function row(ingredient: Ingredient, onChange: (next: Ingredient) => void) {
  return render(() => (
    <IngredientReadingContext.Provider value={() => ({ language: "de", sourceLocale: "de-AT" })}>
      <IngredientRow
        ingredient={ingredient}
        position={2}
        style="us"
        first={false}
        last
        removable
        onChange={onChange}
        onMove={() => {}}
        onRemove={() => {}}
      />
    </IngredientReadingContext.Provider>
  ))
}

test("a typed line is read into an amount, a name and a note when the field is left", async () => {
  vi.mocked(readIngredientLine).mockResolvedValue({
    id: "line",
    name: "Mehl",
    note: "gesiebt",
    quantity: { kind: "simple", amount: { value: "200" }, unit: "mass.gram" },
    normalization: { method: "extracted" },
  })
  const onChange = vi.fn()
  const screen = row({ id: "flour", name: "" }, onChange)
  const field = screen.getByRole("textbox", { name: "Ingredient" })

  fireEvent.input(field, { target: { value: "200 g Mehl, gesiebt" } })
  expect(onChange).not.toHaveBeenCalled()
  fireEvent.blur(field)
  await readingsSettled()

  await waitFor(() => expect(onChange).toHaveBeenCalledTimes(1))
  expect(onChange.mock.calls[0][0]).toMatchObject({
    id: "flour",
    name: "Mehl",
    note: "gesiebt",
    source_text: "200 g Mehl, gesiebt",
    quantity: { unit: "mass.gram" },
  })
  expect(readIngredientLine).toHaveBeenCalledWith("200 g Mehl, gesiebt", {
    language: "de",
    sourceLocale: "de-AT",
  })
})

test("a line the server cannot be asked about is kept as the ingredient's name", async () => {
  vi.mocked(readIngredientLine).mockRejectedValue(new Error("offline"))
  const onChange = vi.fn()
  const screen = row({ id: "flour", name: "" }, onChange)
  const field = screen.getByRole("textbox", { name: "Ingredient" })

  fireEvent.input(field, { target: { value: "2 cups flour" } })
  fireEvent.blur(field)
  await readingsSettled()

  await waitFor(() => expect(onChange).toHaveBeenCalledTimes(1))
  expect(onChange.mock.calls[0][0]).toMatchObject({
    id: "flour",
    name: "2 cups flour",
    source_text: "2 cups flour",
  })
  expect(onChange.mock.calls[0][0].quantity).toBeUndefined()
})

test("a line left as it was is not read again", async () => {
  const onChange = vi.fn()
  const screen = row(
    {
      id: "flour",
      name: "Mehl",
      quantity: { kind: "simple", amount: { value: "200" }, unit: "mass.gram" },
    },
    onChange,
  )
  const field = screen.getByRole("textbox", { name: "Ingredient" }) as HTMLInputElement
  expect(field.value).toBe("200 g Mehl")

  fireEvent.blur(field)
  await readingsSettled()
  expect(readIngredientLine).not.toHaveBeenCalled()
  expect(onChange).not.toHaveBeenCalled()
})

test("an answer for text that was typed over is dropped", async () => {
  let answer: (value: Ingredient) => void = () => {}
  vi.mocked(readIngredientLine).mockReturnValue(
    new Promise<Ingredient>((resolve) => {
      answer = resolve
    }),
  )
  const onChange = vi.fn()
  const screen = row({ id: "flour", name: "" }, onChange)
  const field = screen.getByRole("textbox", { name: "Ingredient" })

  fireEvent.input(field, { target: { value: "200 g Mehl" } })
  fireEvent.blur(field)
  fireEvent.input(field, { target: { value: "300 g Mehl" } })
  answer({ id: "line", name: "Mehl" })
  await readingsSettled()

  expect(onChange).not.toHaveBeenCalled()
})
