import { Show, Suspense, lazy } from "solid-js"
import { css } from "styled-system/css"

import { timerAnnouncement, timers, useTimerClock } from "./timer-clock"

const TimerTray = lazy(() =>
  import("./timer-tray").then((module) => ({ default: module.TimerTray })),
)

export function TimerStatus() {
  useTimerClock()
  return (
    <>
      <span role="status" aria-live="polite" aria-atomic="true" class={css({ srOnly: true })}>
        {timerAnnouncement()}
      </span>
      <Show when={timers().length > 0}>
        <Suspense>
          <TimerTray />
        </Suspense>
      </Show>
    </>
  )
}
