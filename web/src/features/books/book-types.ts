import { type Book } from "@/api/client"
import { type RecipePage } from "@/api/recipes"

export interface BookViewProps {
  book: Book
  revision: number
  sourced: { library_entry_id: string; title: string; page_start?: number; page_end?: number }[]
  candidatePage?: RecipePage
  candidateQuery: string
  onCandidateQuery: (query: string) => void
  onCandidatePage: (offset: number) => void
  onReload: () => void
  onTrashed: () => Promise<void>
}
