import { Show, createResource, createSignal } from "solid-js"

import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { forgetAllDrafts } from "@/lib/draft-recovery"
import { forgetOfflineData } from "@/lib/offline-cache"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { AccountNav, accountStyles as styles } from "./account-layout"
import { accountRequest, authEnabled } from "./account-state"
import { PasskeysPanel, RecoveryCodesPanel } from "./passkeys-and-recovery"
import { SessionsPanel } from "./sessions-panel"
import { LinkedSignInMethods, type Method } from "./sign-in-methods"

export function SecurityPage() {
  const [methods] = createResource(
    async () => (await accountRequest<{ methods: Method[] }>("/account/methods")).methods,
  )
  const hasPassword = () =>
    settled(methods)?.some((method) => method.provider === "local" && method.enabled) ?? false
  return (
    <main class={styles.page}>
      <div class={styles.header}>
        <h1 class={styles.heading}>{m.account_security_title()}</h1>
        <p class={styles.lede}>{m.account_security_lede()}</p>
      </div>
      <AccountNav />
      <Show
        when={authEnabled()}
        fallback={<p class={styles.section}>{m.account_local_mode_unavailable()}</p>}
      >
        <Show when={hasPassword()}>
          <PasswordPanel />
        </Show>
        <PasskeysPanel />
        <LinkedSignInMethods />
        <RecoveryCodesPanel />
        <SessionsPanel />
        {/* Deletion asks for the password only when one exists, so the form
            waits for that answer instead of changing while it is being used. */}
        <Show when={settled(methods)}>
          <DeleteAccountPanel hasPassword={hasPassword()} />
        </Show>
      </Show>
    </main>
  )
}

function PasswordPanel() {
  const [current, setCurrent] = createSignal("")
  const [next, setNext] = createSignal("")
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  return (
    <section class={styles.section}>
      <h2>{m.account_password_heading()}</h2>
      <p class={styles.muted}>{m.account_password_hint()}</p>
      <ChangeError error={error()} />
      <form
        class={styles.form}
        onSubmit={(event) => {
          event.preventDefault()
          setBusy(true)
          setError(undefined)
          void accountRequest("/account/password", "PUT", {
            current_password: current(),
            new_password: next(),
          })
            .then(() => window.location.assign("/"))
            .catch((failure: unknown) => {
              setError(failure)
              setBusy(false)
            })
        }}
      >
        <FormField label={m.account_password_current()} required>
          <Input
            type="password"
            autocomplete="current-password"
            required
            maxLength={1024}
            value={current()}
            onInput={(event) => setCurrent(event.currentTarget.value)}
          />
        </FormField>
        <FormField label={m.account_password_new()} required>
          <Input
            type="password"
            autocomplete="new-password"
            required
            maxLength={1024}
            value={next()}
            onInput={(event) => setNext(event.currentTarget.value)}
          />
        </FormField>
        <Button type="submit" disabled={busy()}>
          {m.account_password_submit()}
        </Button>
      </form>
    </section>
  )
}

/**
 * Deletion states what it removes before it runs, and the server still requires
 * fresh authentication and resolved library ownership.
 */
function DeleteAccountPanel(props: { hasPassword: boolean }) {
  const [password, setPassword] = createSignal("")
  const [confirmation, setConfirmation] = createSignal("")
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  return (
    <section class={styles.section}>
      <h2>{m.account_delete_heading()}</h2>
      <p class={styles.muted}>{m.account_delete_body()}</p>
      <Dialog
        title={m.account_delete_heading()}
        description={m.account_delete_body()}
        trigger={(triggerProps) => (
          <Button {...triggerProps()} variant="outline" colorPalette="red" width="fit-content">
            {m.account_delete_open()}
          </Button>
        )}
      >
        <form
          class={styles.form}
          onSubmit={(event) => {
            event.preventDefault()
            setBusy(true)
            setError(undefined)
            void accountRequest("/account", "DELETE", {
              password: password(),
              confirmation: confirmation(),
            })
              .then(() => {
                forgetAllDrafts()
                return forgetOfflineData()
              })
              .then(() => {
                window.location.assign("/")
              })
              .catch((failure: unknown) => {
                setError(failure)
                setBusy(false)
              })
          }}
        >
          <Show
            when={props.hasPassword}
            fallback={<p class={styles.muted}>{m.account_delete_reauthenticate()}</p>}
          >
            <FormField label={m.account_delete_password()} required>
              <Input
                aria-label={m.account_delete_password()}
                type="password"
                autocomplete="current-password"
                required
                maxLength={1024}
                value={password()}
                onInput={(event) => setPassword(event.currentTarget.value)}
              />
            </FormField>
          </Show>
          <FormField label={m.account_delete_confirmation()} required>
            <Input
              aria-label={m.account_delete_confirmation()}
              required
              maxLength={200}
              value={confirmation()}
              onInput={(event) => setConfirmation(event.currentTarget.value)}
            />
          </FormField>
          <ChangeError error={error()} />
          <div class={styles.actions}>
            <DialogClose>
              {(closeProps) => (
                <Button {...closeProps()} variant="outline" disabled={busy()}>
                  {m.common_cancel()}
                </Button>
              )}
            </DialogClose>
            <Button
              type="submit"
              colorPalette="red"
              disabled={busy() || confirmation() !== "DELETE"}
            >
              {m.account_delete_submit()}
            </Button>
          </div>
        </form>
      </Dialog>
    </section>
  )
}
