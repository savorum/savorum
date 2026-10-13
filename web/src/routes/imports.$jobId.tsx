import { createFileRoute } from "@tanstack/solid-router"

import { ImportJobPage } from "@/features/import-job/import-job-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/imports/$jobId")({
  component: ImportJobRoute,
  // `remaining` names the staged review this import was started from, when it
  // left recipes behind. It is the way back to work that is still waiting.
  validateSearch: (search: Record<string, unknown>): { remaining?: string } =>
    typeof search.remaining === "string" && search.remaining !== ""
      ? { remaining: search.remaining }
      : {},
  head: () => ({ meta: [{ title: m.imports_jobId_import_savorum() }] }),
})
function ImportJobRoute() {
  const params = Route.useParams()
  const search = Route.useSearch()
  return <ImportJobPage jobID={() => params().jobId} remaining={() => search().remaining} />
}
