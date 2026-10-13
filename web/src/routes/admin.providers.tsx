import { createFileRoute } from "@tanstack/solid-router"

import { AdminProvidersPage } from "@/features/account/admin-providers-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/admin/providers")({
  component: AdminProvidersPage,
  head: () => ({ meta: [{ title: m.account_admin_providers_page_title() }] }),
})
