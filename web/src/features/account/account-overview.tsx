import { Link } from "@tanstack/solid-router"
import { For, Show, createSignal } from "solid-js"

import { Callout, ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { CopyField } from "@/components/ui/clipboard"
import { ConfirmDialog } from "@/components/ui/dialog"
import { forgetCooking } from "@/features/cooking/cooking-session"
import { forgetShopping } from "@/features/shopping/shopping-store"
import { forgetAllDrafts } from "@/lib/draft-recovery"
import { forgetOfflineData } from "@/lib/offline-cache"

import { m } from "../../lib/messages.ts"
import { AccountNav, accountStyles as styles } from "./account-layout"
import { account, accountRequest, authEnabled, libraries, roleName } from "./account-state"
import { InvitationAcceptance } from "./invitations"

export function AccountOverview() {
  const [error, setError] = createSignal<unknown>()
  const [confirmingSignOut, setConfirmingSignOut] = createSignal(false)
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
        setConfirmingSignOut(false)
        setError(failure)
        setBusy(false)
      })
  }
  return (
    <main class={styles.page}>
      <Show when={confirmingSignOut()}>
        <ConfirmDialog
          busy={busy()}
          cancelLabel={m.common_cancel()}
          title={m.account_overview_sign_out()}
          confirmLabel={m.account_overview_sign_out()}
          onClose={() => setConfirmingSignOut(false)}
          onConfirm={signOut}
        >
          {m.account_sign_out_local_work()}
        </ConfirmDialog>
      </Show>
      <div class={styles.header}>
        <h1 class={styles.heading}>{m.account_overview_title()}</h1>
        <p class={styles.lede}>
          <Show when={authEnabled()} fallback={m.account_overview_local_mode()}>
            {m.account_overview_signed_in_as({ name: account()?.user?.display_name ?? "" })}
          </Show>
        </p>
      </div>
      <AccountNav />
      <InvitationAcceptance />
      <Show when={account()?.recovery_authenticated}>
        <Callout
          tone="warning"
          title={m.account_overview_recovery_title()}
          actions={
            <Link class={styles.back} to="/account/security">
              {m.account_overview_recovery_link()}
            </Link>
          }
        >
          {m.account_overview_recovery_body()}
        </Callout>
      </Show>
      <Show when={authEnabled()}>
        <section class={styles.section}>
          <h2>{m.account_overview_details()}</h2>
          <p>{account()?.user?.display_name}</p>
          <CopyField
            label={m.account_overview_user_id()}
            value={account()?.user?.id ?? ""}
            copyLabel={m.account_copy()}
            copiedLabel={m.account_copied()}
          />
          <p class={styles.muted}>{m.account_overview_user_id_hint()}</p>
        </section>
      </Show>
      <Show when={authEnabled() || libraries().length > 1}>
        <section class={styles.section}>
          <h2>{m.account_overview_libraries()}</h2>
          <Show when={libraries().length} fallback={<p>{m.account_overview_libraries_empty()}</p>}>
            <ul class={styles.list}>
              <For each={libraries()}>
                {(library) => (
                  <li class={styles.card}>
                    <h3>{library.name}</h3>
                    <p class={styles.muted}>{roleName(library.role)}</p>
                    <Show when={authEnabled() && library.role === "owner"}>
                      <Link
                        class={styles.back}
                        to="/libraries/$libraryId/access"
                        params={{ libraryId: library.id }}
                      >
                        {m.account_overview_manage_access()}
                      </Link>
                    </Show>
                  </li>
                )}
              </For>
            </ul>
          </Show>
        </section>
      </Show>
      <section class={styles.section}>
        <h2>{m.account_overview_preferences()}</h2>
        <p class={styles.muted}>{m.account_overview_preferences_hint()}</p>
        <Link class={styles.back} to="/preferences">
          {m.account_overview_preferences_link()}
        </Link>
      </section>
      <Show when={authEnabled()}>
        <section class={styles.section}>
          <h2>{m.account_overview_session_heading()}</h2>
          <p class={styles.muted}>{m.account_overview_sign_out_hint()}</p>
          <ChangeError error={error()} />
          <div class={styles.actions}>
            <Button disabled={busy()} onClick={() => setConfirmingSignOut(true)}>
              {m.account_overview_sign_out()}
            </Button>
          </div>
        </section>
      </Show>
    </main>
  )
}
