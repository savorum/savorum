import { createFileRoute } from "@tanstack/solid-router"

import { ImportsPage } from "@/features/import-job/imports-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/imports/")({
  component: ImportsPageRoute,
  head: () => ({ meta: [{ title: m.imports_index_imports_savorum() }] }),
})
function ImportsPageRoute() {
  return <ImportsPage />
}
