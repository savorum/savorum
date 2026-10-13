import type { RecipeSort } from "@/api/client"
export interface Draft {
  id?: string
  revision: number
  title: string
  query: string
  sort: RecipeSort
}
