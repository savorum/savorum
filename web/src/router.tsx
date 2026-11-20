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
  // Solid keeps resource updates made during a transition until it commits, and
  // a load that settles in that window can stay unresolved. Navigation commits
  // directly instead; the Transitioner's assignment is ignored.
  Object.defineProperty(router, "startTransition", {
    get: () => (commit: () => void) => {
      commit()
      return Promise.resolve(true)
    },
    set: () => {},
  })

  return router
}

declare module "@tanstack/solid-router" {
  interface Register {
    router: ReturnType<typeof getRouter>
  }
}
