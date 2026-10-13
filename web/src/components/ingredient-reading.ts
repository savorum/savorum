import { createContext, useContext } from "solid-js"

import type { LineReading } from "@/api/ingredient-lines"

/**
 * What an ingredient row needs to read what is typed into it: the recipe's
 * language and source locale, and the author's choice for submitted lines.
 */
export const IngredientReadingContext = createContext<() => LineReading>(() => ({}))

export const useIngredientReading = () => useContext(IngredientReadingContext)
