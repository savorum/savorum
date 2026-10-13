import { createFileRoute } from "@tanstack/solid-router"

import { SavedSearchesPage } from "@/features/saved-searches/saved-searches-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/saved-searches")({
  component: SavedSearchesPageRoute,
  head: () => ({ meta: [{ title: m.saved_searches_saved_searches_savorum() }] }),
})
function SavedSearchesPageRoute() {
  return <SavedSearchesPage />
}
