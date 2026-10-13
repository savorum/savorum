import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { CaptureForm } from "@/features/capture/capture-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/capture")({
  component: CapturePage,
  validateSearch: (
    search: Record<string, unknown>,
  ): { url?: string; text?: string; title?: string } => ({
    url: typeof search.url === "string" ? search.url : undefined,
    text: typeof search.text === "string" ? search.text : undefined,
    title: typeof search.title === "string" ? search.title : undefined,
  }),
  head: () => ({ meta: [{ title: m.capture_save_a_recipe_savorum() }] }),
})
function CapturePage() {
  const search = Route.useSearch()
  const navigate = useNavigate()
  return (
    <CaptureForm
      initial={search()}
      onPreview={(capture) => {
        void navigate({ to: "/", search: { capture } })
      }}
    />
  )
}
