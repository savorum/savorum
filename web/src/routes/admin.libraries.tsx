import { createFileRoute } from "@tanstack/solid-router"

import { AdminLibrariesPage } from "@/features/account/admin-libraries-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/admin/libraries")({
  component: AdminLibrariesPage,
  head: () => ({ meta: [{ title: m.account_admin_libraries_page_title() }] }),
})
