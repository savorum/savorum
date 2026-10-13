import { useRouter, useRouterState } from "@tanstack/solid-router"
import { createEffect } from "solid-js"

/**
 * Names the browser tab after what the page shows. A route's own title is
 * written before the page knows its subject and again after every route
 * change, so this runs after both.
 */
export function usePageTitle(title: () => string | undefined): void {
  const router = useRouter({ warn: false })
  const matches = router ? useRouterState({ router, select: (state) => state.matches }) : () => []
  createEffect(() => {
    matches()
    const value = title()
    if (value) document.title = value
  })
}
