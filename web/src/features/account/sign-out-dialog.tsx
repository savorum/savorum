import { Show, createSignal } from "solid-js"

import { ChangeError } from "@/components/status"
import { ConfirmDialog } from "@/components/ui/dialog"
import { forgetCooking } from "@/features/cooking/cooking-session"
import { forgetShopping } from "@/features/shopping/shopping-store"
import { forgetAllDrafts } from "@/lib/draft-recovery"
import { m } from "@/lib/messages"
import { forgetOfflineData } from "@/lib/offline-cache"

import { accountRequest } from "./account-state"

export function SignOutDialog(props: { open: boolean; onClose: () => void }) {
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  function signOut() {
    if (busy()) return
    setBusy(true)
    setError(undefined)
    void accountRequest("/account/logout", "POST")
      .then(() => {
        // Signing out ends this browser's claim to the library,
        // including work that was kept but never saved.
        forgetAllDrafts()
        forgetCooking()
        forgetShopping()
        return forgetOfflineData()
      })
      .then(() => {
        window.location.assign("/")
      })
      .catch((failure: unknown) => {
        setError(failure)
        setBusy(false)
      })
  }
  return (
    <Show when={props.open}>
      <ConfirmDialog
        busy={busy()}
        cancelLabel={m.common_cancel()}
        title={m.account_overview_sign_out()}
        confirmLabel={m.account_overview_sign_out()}
        onClose={() => {
          if (busy()) return
          setError(undefined)
          props.onClose()
        }}
        onConfirm={signOut}
      >
        {m.account_sign_out_local_work()}
        <ChangeError error={error()} />
      </ConfirmDialog>
    </Show>
  )
}
