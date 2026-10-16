import { createFileRoute } from "@tanstack/solid-router"

import { AdminConnectPage } from "@/features/account/admin-connect-page"

import { m } from "../lib/messages.ts"

export const Route = createFileRoute("/admin/connect")({
  component: AdminConnectPage,
  head: () => ({ meta: [{ title: m.connect_page_title() }] }),
})
