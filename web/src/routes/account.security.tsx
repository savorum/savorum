import { createFileRoute } from "@tanstack/solid-router"

import { SecurityPage } from "@/features/account/security-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/account/security")({
  component: SecurityPage,
  head: () => ({ meta: [{ title: m.account_security_page_title() }] }),
})
