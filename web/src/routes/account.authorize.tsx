import { createFileRoute } from "@tanstack/solid-router"

import { AssistantApproval } from "@/features/account/assistant-approval"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/account/authorize")({
  component: AssistantApproval,
  head: () => ({ meta: [{ title: m.account_assistant_heading() }] }),
})
