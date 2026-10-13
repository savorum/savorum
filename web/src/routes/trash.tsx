import { createFileRoute } from "@tanstack/solid-router"

import { TrashPage } from "@/features/trash/trash-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/trash")({
  component: TrashPageRoute,
  head: () => ({ meta: [{ title: m.trash_trash_savorum() }] }),
})
function TrashPageRoute() {
  return <TrashPage />
}
