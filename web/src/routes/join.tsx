import { Navigate, createFileRoute } from "@tanstack/solid-router"

import { invitationToken } from "@/features/account/invitations"

import { m } from "../lib/messages.ts"

export const Route = createFileRoute("/join")({
  component: Join,
  head: () => ({ meta: [{ title: m.account_invitation_heading() }] }),
})

function Join() {
  if (typeof window !== "undefined") invitationToken()
  return <Navigate to="/account" replace />
}
