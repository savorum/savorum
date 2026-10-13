import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { RecipePrintPage } from "@/features/printing/recipe-print-page"
import {
  type PrintSearch,
  parsePrintSearch,
  searchFromSettings,
  settingsFromSearch,
} from "@/lib/print-options"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/recipes/$recipeId/print")({
  component: RecipePrintRoute,
  validateSearch: (search: Record<string, unknown>): PrintSearch => parsePrintSearch(search),
  head: () => ({ meta: [{ title: m.recipes_recipeId_print_print_savorum() }] }),
})
function RecipePrintRoute() {
  const params = Route.useParams()
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  return (
    <RecipePrintPage
      recipeId={() => params().recipeId}
      search={search}
      onSettingsChange={(next) => {
        void navigate({
          search: (current: PrintSearch) => ({
            ...current,
            ...searchFromSettings({ ...settingsFromSearch(current), ...next }),
          }),
          replace: true,
        })
      }}
    />
  )
}
