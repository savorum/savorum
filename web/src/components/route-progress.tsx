import { useRouterState } from "@tanstack/solid-router"
import { createEffect, createSignal, onCleanup, Show } from "solid-js"
import { css } from "styled-system/css"

import { m } from "../lib/messages.ts"

const delay = 150

export function RouteProgress() {
  const pending = useRouterState({ select: (state) => state.status === "pending" })
  const [shown, setShown] = createSignal(false)
  createEffect(() => {
    if (!pending()) return setShown(false)
    const timer = setTimeout(() => setShown(true), delay)
    onCleanup(() => clearTimeout(timer))
  })
  return (
    <Show when={shown()}>
      <div class={styles.track} role="progressbar" aria-label={m.common_loading()}>
        <span class={styles.bar} />
      </div>
    </Show>
  )
}

const styles = {
  track: css({
    height: "2px",
    insetInline: "0",
    overflow: "hidden",
    pointerEvents: "none",
    position: "fixed",
    top: "0",
    zIndex: "overlay",
  }),
  bar: css({
    animation: "route-progress 1.1s cubic-bezier(0.22, 1, 0.36, 1) infinite",
    background: "accent.default",
    display: "block",
    height: "100%",
    width: "40%",
    _motionReduce: { animation: "none", width: "100%", opacity: "0.6" },
  }),
}
