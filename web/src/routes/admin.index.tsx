import { createFileRoute } from "@tanstack/solid-router"

import { AdminPeoplePage } from "@/features/account/admin-people-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/admin/")({
  component: AdminPeoplePage,
  head: () => ({ meta: [{ title: m.account_admin_people_page_title() }] }),
})
