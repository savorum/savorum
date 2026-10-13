import { createFileRoute } from "@tanstack/solid-router"

import { IntegrationsPage } from "@/features/account/integrations-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/account/integrations")({
  component: IntegrationsPage,
  head: () => ({ meta: [{ title: m.integrations_title() }] }),
})
