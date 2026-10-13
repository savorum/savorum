import { createFileRoute } from "@tanstack/solid-router"

import { ShoppingPage } from "@/features/shopping/shopping-page"

import { m } from "../lib/messages.ts"

export const Route = createFileRoute("/shopping")({
  component: ShoppingPage,
  head: () => ({ meta: [{ title: m.shopping_page_title() }] }),
})
