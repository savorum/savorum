import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { type PlanSearch, parsePlanSearch } from "@/features/plan/plan-calendar"
import { PlanPage } from "@/features/plan/plan-page"

import { m } from "../lib/messages.ts"

export const Route = createFileRoute("/plan")({
  component: PlanRoute,
  validateSearch: parsePlanSearch,
  head: () => ({ meta: [{ title: m.plan_index_title() }] }),
})

function PlanRoute() {
  const search = Route.useSearch()
  const navigate = useNavigate()
  return (
    <PlanPage
      search={search}
      onSearch={(next: PlanSearch) => void navigate({ to: "/plan", search: next })}
    />
  )
}
