import { Show, Suspense, createSignal, lazy, onMount, type ParentProps } from "solid-js"
import { css } from "styled-system/css"

import { AppLoading, Callout } from "@/components/status"
import { Button } from "@/components/ui/button"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import {
  account,
  dismissSessionEnded,
  refreshAccount,
  sessionEnded,
  watchSessionStanding,
} from "./account-state"

const Login = lazy(() => import("./login-page").then((module) => ({ default: module.Login })))

export function AccountShell(props: ParentProps) {
  const [error, setError] = createSignal("")
  watchSessionStanding()
  const load = () => {
    setError("")
    void refreshAccount().catch((failure: Error) => setError(failure.message))
  }
  onMount(load)
  return (
    <Show
      when={account()}
      fallback={
        <Show when={error()} fallback={<AppLoading />}>
          <main id="main-content" tabIndex={-1} class={styles.page}>
            <Callout
              tone="neutral"
              role="alert"
              actions={<Button onClick={load}>{m.account_retry()}</Button>}
            >
              {error()}
            </Callout>
          </main>
        </Show>
      }
    >
      <Show
        when={!account()?.auth_enabled || account()?.user}
        fallback={
          <Suspense fallback={<AppLoading />}>
            <Login />
          </Suspense>
        }
      >
        <SessionEnded />
        {props.children}
      </Show>
    </Show>
  )
}

const bannerStyles = {
  // Sticky, so an ended session is visible wherever the reader has scrolled.
  banner: css({
    paddingBlock: "3",
    paddingInline: "4",
    position: "sticky",
    insetBlockStart: "0",
    zIndex: "docked",
  }),
  alert: css({ marginBlock: "0", marginInline: "auto", maxWidth: "48rem" }),
}

/**
 * The session ended while the page was open. Nothing on screen is removed and
 * nothing is submitted for the reader: signing in again is a choice they make,
 * after which an editor offers back whatever it had kept.
 */
function SessionEnded() {
  return (
    <Show when={sessionEnded()}>
      <div class={bannerStyles.banner}>
        <Callout
          class={bannerStyles.alert}
          tone="warning"
          role="alert"
          actions={
            <>
              <Button size="sm" onClick={() => window.location.reload()}>
                {m.account_session_ended_sign_in()}
              </Button>
              <Button size="sm" variant="plain" onClick={() => dismissSessionEnded()}>
                {m.account_session_ended_dismiss()}
              </Button>
            </>
          }
        >
          {m.account_session_ended()}
        </Callout>
      </div>
    </Show>
  )
}
