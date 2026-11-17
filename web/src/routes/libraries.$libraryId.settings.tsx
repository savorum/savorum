import { createFileRoute } from "@tanstack/solid-router"

import { LibrarySettingsPage } from "@/features/account/library-settings-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/libraries/$libraryId/settings")({
  component: LibrarySettingsPage,
  head: () => ({ meta: [{ title: m.library_settings_page_title() }] }),
})
