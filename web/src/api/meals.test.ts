import { afterEach, expect, test, vi } from "vitest"

import { client } from "./client"
import { createMeal, replaceSavedMeal } from "./meals"

afterEach(() => {
  vi.restoreAllMocks()
})

// A meal's ETag digests the representation; its revision is only in the body.
const representation = new Response(null, {
  headers: { ETag: '"r-0d20b69e34b7796ef6f9165ba5db0faf"' },
})

test("a planned meal's revision comes from its body, not its representation ETag", async () => {
  vi.spyOn(client, "POST").mockResolvedValue({
    data: { id: "meal", revision: 3, dishes: [] },
    response: representation,
  } as unknown as Awaited<ReturnType<typeof client.POST>>)

  const result = await createMeal({ dishes: [{ display_name: "Soup" }] })
  expect(result.revision).toBe(3)
  expect(result.value.id).toBe("meal")
})

test("a saved meal's revision comes from its body, not its representation ETag", async () => {
  vi.spyOn(client, "PUT").mockResolvedValue({
    data: { id: "saved", name: "Brunch", revision: 5, dishes: [] },
    response: representation,
  } as unknown as Awaited<ReturnType<typeof client.PUT>>)

  const result = await replaceSavedMeal("saved", 4, { name: "Brunch", dishes: [] })
  expect(result.revision).toBe(5)
})
