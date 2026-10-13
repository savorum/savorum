import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { SelectionPrintPage } from "@/features/printing/selection-print-page"
import {
  type PrintSearch,
  parsePrintSearch,
  searchFromSettings,
  settingsFromSearch,
} from "@/lib/print-options"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/print")({
  component: SelectionPrintRoute,
  validateSearch: (search: Record<string, unknown>): PrintSearch => parsePrintSearch(search),
  head: () => ({ meta: [{ title: m.print_print_recipes_savorum() }] }),
})
function SelectionPrintRoute() {
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  return (
    <SelectionPrintPage
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
