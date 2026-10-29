import { listRecipes } from "./recipes"

export function libraryRecipeTotal(): Promise<number | undefined> {
  return listRecipes({ limit: 1 }).then(
    (page) => page.total,
    () => undefined,
  )
}
