import { expect, test } from "vitest"

import { bookHue } from "./book-cover"

test("a book keeps its binding hue and different books spread across hues", () => {
  expect(bookHue("book-a")).toBe(bookHue("book-a"))
  const hues = new Set(Array.from({ length: 40 }, (_, index) => bookHue(`book-${index}`)))
  expect(hues.size).toBeGreaterThanOrEqual(6)
})
