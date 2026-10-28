import { Link } from "@tanstack/solid-router"
import { For, Show, createSignal } from "solid-js"

import { Callout, ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { CopyField } from "@/components/ui/clipboard"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"

import { m } from "../../lib/messages.ts"
import { AccountNav, accountStyles as styles } from "./account-layout"
import {
  type AccountUser,
  account,
  accountRequest,
  authEnabled,
  libraries,
  roleName,
  setAccount,
} from "./account-state"
import { InvitationAcceptance } from "./invitations"

export function AccountOverview() {
  return (
    <main class={styles.page}>
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
        <NamePanel />
        <div class={styles.details}>
          <CopyField
            label={m.account_overview_user_id()}
            value={account()?.user?.id ?? ""}
            copyLabel={m.account_copy()}
            copiedLabel={m.account_copied()}
          />
          <p class={styles.muted}>{m.account_overview_user_id_hint()}</p>
        </div>
      </Show>
      <Show when={authEnabled() || libraries().length > 1}>
        <section class={styles.section}>
          <h2>{m.account_overview_libraries()}</h2>
          <Show when={libraries().length} fallback={<p>{m.account_overview_libraries_empty()}</p>}>
            <ul class={styles.rows}>
              <For each={libraries()}>
                {(library) => (
                  <li class={styles.listRow}>
                    <div class={styles.rowBody}>
                      <h3>{library.name}</h3>
                      <p class={styles.muted}>{roleName(library.role)}</p>
                    </div>
                    <Show when={authEnabled() && library.role === "owner"}>
                      <Link
                        class={styles.back}
                        to="/libraries/$libraryId/access"
                        params={{ libraryId: library.id }}
                        hash="invitations"
                      >
                        {m.account_overview_invite_people()}
                      </Link>
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
    </main>
  )
}

function NamePanel() {
  const [name, setName] = createSignal(account()?.user?.display_name ?? "")
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  return (
    <section class={styles.section}>
      <h2>{m.account_name_heading()}</h2>
      <p class={styles.muted}>{m.account_name_hint()}</p>
      <ChangeError error={error()} />
      <form
        class={styles.form}
        onSubmit={(event) => {
          event.preventDefault()
          setBusy(true)
          setError(undefined)
          void accountRequest<AccountUser>("/account", "PATCH", { display_name: name() })
            .then((user) => {
              setAccount((current) => current && { ...current, user })
              setName(user.display_name)
            })
            .catch((failure: unknown) => setError(failure))
            .finally(() => setBusy(false))
        }}
      >
        <FormField label={m.account_name_label()} required>
          <Input
            required
            autocomplete="name"
            value={name()}
            onInput={(event) => setName(event.currentTarget.value)}
          />
        </FormField>
        <Button type="submit" disabled={busy()}>
          {m.account_name_submit()}
        </Button>
      </form>
    </section>
  )
}
