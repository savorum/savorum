import { createFileRoute } from "@tanstack/solid-router"

import { AdminAuditPage } from "@/features/account/admin-audit-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/admin/audit")({
  component: AdminAuditPage,
  head: () => ({ meta: [{ title: m.account_admin_audit_page_title() }] }),
})
