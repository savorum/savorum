import { client } from "./client"
import type { components } from "./schema"

type Ingredient = components["schemas"]["Ingredient"]

/** What decides how a line is read: its language and the measures its writer uses. */
export interface LineReading {
  /** The language the line is written in. */
  language?: string
  /** The locale the recipe declares, which takes the language's place when set. */
  sourceLocale?: string
  /** An explicit author choice for unqualified measures in this submitted line. */
  convention?: "metric" | "us" | "au" | "imperial"
}

export type IngredientConvention = NonNullable<LineReading["convention"]> | ""

/**
 * Reads one typed ingredient line into an amount, unit, name and note on the
 * server, which holds the unit words of every language. The result is what an
 * import of the same line would make.
 */
export async function readIngredientLine(text: string, reading: LineReading): Promise<Ingredient> {
  const { data } = await client.POST("/api/v1/ingredient-lines/parse", {
    body: {
      text,
      language: reading.language || undefined,
      source_locale: reading.sourceLocale || undefined,
      convention: reading.convention,
    },
  })
  if (!data) throw new Error("the ingredient line could not be read")
  return data.ingredient
}
