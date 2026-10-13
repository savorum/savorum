import { createFileRoute } from "@tanstack/solid-router"

import { PreferencesPage } from "@/features/preferences/preferences-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/preferences")({
  component: PreferencesPage,
  head: () => ({ meta: [{ title: m.preferences_your_preferences_savorum() }] }),
})
