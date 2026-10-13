import type { APIRequestContext } from "@playwright/test"

/**
 * Puts a recipe in the library through the same API the frontend uses. Every
 * run gets its own identities, so a repeated run never meets a recipe it
 * already created.
 */
export async function seedRecipe(
  request: APIRequestContext,
  title: string,
  change: Record<string, unknown> = {},
): Promise<{ id: string; title: string }> {
  const suffix = Math.random().toString(36).slice(2, 10)
  const response = await request.post("/api/v1/recipes", {
    data: {
      schema_version: 1,
      id: `e2e-${suffix}`,
      title,
      description: "A weeknight version that leans on a hot pan and a long enough simmer.",
      language: "en-US",
      yields: [
        {
          quantity: { kind: "simple", amount: { value: "4" }, unit: "count.serving" },
          label: "servings",
        },
      ],
      times: { prep: { value: "PT25M" }, cook: { value: "PT35M" } },
      notes: ["The marinade can sit overnight; the yoghurt keeps the chicken tender."],
      ingredient_sections: [
        {
          id: "marinade",
          title: "Marinade",
          ingredients: [
            {
              id: "yoghurt",
              name: "whole-milk yoghurt",
              quantity: {
                kind: "simple",
                amount: { value: "1/2" },
                unit: "volume.cup.us.customary",
              },
            },
            {
              id: "chicken",
              name: "boneless chicken thighs",
              quantity: {
                kind: "simple",
                amount: { value: "2" },
                unit: "mass.pound.avoirdupois",
              },
            },
          ],
        },
      ],
      instruction_sections: [
        {
          id: "method",
          steps: [
            { id: "s1", text: "Whisk the yoghurt with a generous pinch of salt." },
            { id: "s2", text: "Fold the chicken through and refrigerate for two hours." },
          ],
        },
      ],
      ...change,
    },
  })
  if (!response.ok()) {
    throw new Error(`could not seed ${title}: ${response.status()} ${await response.text()}`)
  }
  const created = (await response.json()) as { id: string }
  return { id: created.id, title }
}

export async function seedBook(
  request: APIRequestContext,
  title: string,
  entryIDs: string[],
): Promise<{ id: string; title: string }> {
  const response = await request.post("/api/v1/books", {
    data: {
      kind: "personal",
      title,
      sections: [],
      entries: entryIDs.map((id, index) => ({ library_entry_id: id, position: index + 1 })),
    },
  })
  if (!response.ok()) {
    throw new Error(`could not seed book ${title}: ${response.status()} ${await response.text()}`)
  }
  const created = (await response.json()) as { id: string }
  return { id: created.id, title }
}
