import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import { createSignal, untrack } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { readIngredientLine } from "@/api/ingredient-lines"
import type { components } from "@/api/schema"
import { IngredientReadingContext } from "@/components/ingredient-reading"
import { readingsPending, readingsSettled } from "@/lib/line-readings"

import { IngredientChoice } from "./ingredient-choice"
import { IngredientRow } from "./ingredient-row"

vi.mock("@/api/ingredient-lines", () => ({ readIngredientLine: vi.fn() }))

type Ingredient = components["schemas"]["Ingredient"]

const measurementStyle = "us"

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
        style={measurementStyle}
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

test("an older completion cannot discard a newer pending line during an ingredient update", async () => {
  let first!: (value: Ingredient) => void
  let second!: (value: Ingredient) => void
  vi.mocked(readIngredientLine)
    .mockReturnValueOnce(
      new Promise<Ingredient>((resolve) => {
        first = resolve
      }),
    )
    .mockReturnValueOnce(
      new Promise<Ingredient>((resolve) => {
        second = resolve
      }),
    )
  const [ingredient, setIngredient] = createSignal<Ingredient>({ id: "flour", name: "flour" })
  const onChange = vi.fn()
  const screen = render(() => (
    <IngredientRow
      ingredient={ingredient()}
      position={2}
      style={measurementStyle}
      first={false}
      last
      removable
      onChange={onChange}
      onMove={() => {}}
      onRemove={() => {}}
    />
  ))
  const field = screen.getByRole("textbox", { name: "Ingredient" }) as HTMLInputElement
  fireEvent.input(field, { target: { value: "200 g flour" } })
  fireEvent.blur(field)
  fireEvent.input(field, { target: { value: "300 g flour" } })
  fireEvent.blur(field)
  first({ id: "line", name: "flour" })
  await waitFor(() => expect(readingsPending()).toBe(1))
  setIngredient((current) => ({ ...current, note: "sifted", optional: true }))
  expect(field.value).toBe("300 g flour")
  second({ id: "line", name: "flour" })
  await readingsSettled()
  expect(onChange).toHaveBeenCalledTimes(1)
  expect(onChange).toHaveBeenCalledWith(
    expect.objectContaining({ source_text: "300 g flour", optional: true }),
  )
})

test.each(["move", "remove"] as const)(
  "a pending reading stays with its alternative after %s",
  async (change) => {
    let answer!: (value: Ingredient) => void
    vi.mocked(readIngredientLine).mockReturnValue(
      new Promise<Ingredient>((resolve) => {
        answer = resolve
      }),
    )
    const [ingredient, setIngredient] = createSignal<Ingredient>({
      id: "choice",
      relation: "or",
      alternatives: [
        { id: "flour", name: "Flour" },
        { id: "milk", name: "Milk" },
        { id: "water", name: "Water" },
      ],
    })
    const screen = render(() => (
      <IngredientChoice
        ingredient={ingredient()}
        position={1}
        style={measurementStyle}
        first
        last
        removable
        onChange={setIngredient}
        onMove={() => {}}
        onRemove={() => {}}
      />
    ))
    const input = screen.getAllByRole("textbox", { name: "Ingredient" })[0]
    fireEvent.input(input, { target: { value: "2 cups Flour" } })
    fireEvent.blur(input)
    setIngredient((current) => ({
      ...current,
      alternatives:
        change === "move"
          ? [current.alternatives![1], current.alternatives![0], current.alternatives![2]]
          : current.alternatives!.slice(1),
    }))
    answer({
      id: "parsed",
      name: "Flour",
      quantity: { kind: "simple", amount: { value: "2" }, unit: "volume.cup.us.customary" },
    })
    await readingsSettled()
    const alternatives = untrack(ingredient).alternatives
    expect(alternatives?.find((item) => item.id === "milk")).toEqual({
      id: "milk",
      name: "Milk",
    })
    expect(
      (screen.getAllByRole("textbox", { name: "Ingredient" })[0] as HTMLInputElement).value,
    ).toBe("Milk")
    const flour = alternatives?.find((item) => item.id === "flour")
    if (change === "move") expect(flour?.quantity?.amount?.value).toBe("2")
    else expect(flour).toBeUndefined()
  },
)
