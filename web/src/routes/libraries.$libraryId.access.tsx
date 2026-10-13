import { createFileRoute } from "@tanstack/solid-router"

import { LibraryAccessPage } from "@/features/account/library-access-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/libraries/$libraryId/access")({
  component: LibraryAccessPage,
  head: () => ({ meta: [{ title: m.account_access_page_title() }] }),
})
