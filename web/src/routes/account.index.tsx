import { createFileRoute } from "@tanstack/solid-router"

import { AccountOverview } from "@/features/account/account-overview"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/account/")({
  component: AccountOverview,
  head: () => ({ meta: [{ title: m.account_overview_page_title() }] }),
})
