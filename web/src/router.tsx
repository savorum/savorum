import { createRouter as createTanStackRouter } from "@tanstack/solid-router"

import { NotFoundPage } from "./components/not-found"
import { routeTree } from "./routeTree.gen"

export function getRouter() {
  const router = createTanStackRouter({
    routeTree,

    scrollRestoration: true,
    scrollRestorationBehavior: "instant",
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    defaultNotFoundComponent: NotFoundPage,
  })

  return router
}

declare module "@tanstack/solid-router" {
  interface Register {
    router: ReturnType<typeof getRouter>
  }
}
