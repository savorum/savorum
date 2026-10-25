import { Show, createSignal, onMount } from "solid-js"

import { AppLoading, Problem } from "@/components/status"

import { Login } from "./login-page"
import { completeNativeBrowser, nativeBrowserReturning } from "./native-browser"

export function NativeBrowserLogin() {
  const [returning, setReturning] = createSignal(nativeBrowserReturning())
  const [error, setError] = createSignal("")
  onMount(() => {
    if (returning()) {
      void completeNativeBrowser()
        .catch((failure: Error) => setError(failure.message))
        .finally(() => setReturning(false))
    }
  })
  return (
    <Show when={!returning()} fallback={<AppLoading />}>
      <Problem message={error()} />
      <Login onSignedIn={completeNativeBrowser} />
    </Show>
  )
}
