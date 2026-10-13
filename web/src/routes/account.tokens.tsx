import { createFileRoute } from "@tanstack/solid-router"

import { TokensPage } from "@/features/account/token-panel"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/account/tokens")({
  component: TokensPage,
  head: () => ({ meta: [{ title: m.account_tokens_page_title() }] }),
})
