import { listRecipes } from "./recipes"

const lifetime = 60_000
let cached: { at: number; value: Promise<number | undefined> } | undefined

export function libraryRecipeTotal(): Promise<number | undefined> {
  if (!cached || Date.now() - cached.at > lifetime) {
    cached = {
      at: Date.now(),
      value: listRecipes({ limit: 1 }).then(
        (page) => page.total,
        () => undefined,
      ),
    }
  }
  return cached.value
}
