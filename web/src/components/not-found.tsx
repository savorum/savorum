import { Link } from "@tanstack/solid-router"
import { onMount } from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { m } from "../lib/messages.ts"

const layout = pageShell({ width: "narrow" })

const styles = {
  detail: css({ color: "fg.muted", marginBlock: "3 6", maxWidth: "38rem" }),
  back: css({
    color: "accent.default",
    fontWeight: "medium",
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "3px" },
  }),
}

/** The address a person typed, followed, or kept after the page moved. */
export function NotFoundPage() {
  // No route matched, so no route head ran; the tab would otherwise keep the
  // name of whichever page the person came from.
  onMount(() => {
    document.title = m.not_found_savorum()
  })
  return (
    <main class={layout.page}>
      <h1 class={layout.title}>{m.not_found_title()}</h1>
      <p class={styles.detail}>{m.not_found_detail()}</p>
      <Link class={styles.back} to="/">
        {m.not_found_back_to_your_library()}
      </Link>
    </main>
  )
}
