import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { ImportsPage } from "@/features/import-job/imports-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/imports/")({
  component: ImportsPageRoute,
  head: () => ({ meta: [{ title: m.imports_index_imports_savorum() }] }),
})
function ImportsPageRoute() {
  const navigate = useNavigate()
  return (
    <ImportsPage
      onStaged={(preview) =>
        void navigate({ to: "/", search: { preview }, state: { fromImports: true } })
      }
      onJob={(jobId, remaining) =>
        navigate({
          to: "/imports/$jobId",
          params: { jobId },
          search: { remaining },
          state: { fromImports: true },
        })
      }
    />
  )
}
