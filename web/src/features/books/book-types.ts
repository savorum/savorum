import { type Book, type RecipeSummary } from "@/api/client"
import type { SuggestionPage } from "@/api/selections"

export interface BookViewProps {
  book: Book
  revision: number
  sourced: { library_entry_id: string; title: string; page_start?: number; page_end?: number }[]
  candidatePage?: SuggestionPage<RecipeSummary>
  candidateQuery: string
  onCandidateQuery: (query: string) => void
  onCandidatePage: (offset: number) => void
  onReload: () => void
  onTrashed: () => Promise<void>
}
