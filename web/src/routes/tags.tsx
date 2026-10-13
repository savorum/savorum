import { createFileRoute } from "@tanstack/solid-router"

import { TagsPage } from "@/features/organization/tags-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/tags")({
  component: TagsPageRoute,
  head: () => ({ meta: [{ title: m.tags_tags_and_categories_savorum() }] }),
})
function TagsPageRoute() {
  return <TagsPage />
}
