import { Show, createSignal, onMount } from "solid-js"

import { Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { usePageTitle } from "@/lib/page-title"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { account, accountRequest } from "./account-state"
import { InvitedRegistration, invitationToken } from "./invitations"
import { PasskeyLogin, RecoveryLogin } from "./passkeys-and-recovery"
import { ProviderLogin } from "./provider-login"

export function Login(props: { onSignedIn?: () => Promise<void> } = {}) {
  const [ready, setReady] = createSignal(false)
  const [invited, setInvited] = createSignal(false)
  onMount(() => {
    setInvited(!!invitationToken())
    setReady(true)
  })
  usePageTitle(() => (invited() ? m.account_invitation_heading() : m.account_sign_in_page_title()))
  return (
    <main id="main-content" tabIndex={-1} class={styles.page}>
      <Show when={ready()} fallback={<p role="status">{m.common_loading()}</p>}>
        <Show
          when={invited()}
          fallback={
            <>
              <h1 class={styles.heading}>{m.account_sign_in_title()}</h1>
              <ExistingAccountSignIn {...props} />
            </>
          }
        >
          <InvitedRegistration signIn={<ExistingAccountSignIn {...props} />} />
        </Show>
      </Show>
    </main>
  )
}

function ExistingAccountSignIn(props: { onSignedIn?: () => Promise<void> }) {
  const [login, setLogin] = createSignal("")
  const [password, setPassword] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const [error, setError] = createSignal("")
  async function submit(event: SubmitEvent) {
    event.preventDefault()
    setBusy(true)
    setError("")
    try {
      await accountRequest("/account/login", "POST", { login: login(), password: password() })
      if (props.onSignedIn) await props.onSignedIn()
      else window.location.reload()
    } catch (failure) {
      setError((failure as Error).message)
      setBusy(false)
    }
  }
  return (
    <>
      <PasskeyLogin
        available={account()?.passkeys_available ?? false}
        onSignedIn={props.onSignedIn}
      />
      <ProviderLogin />
      <form class={styles.form} onSubmit={(event) => void submit(event)}>
        <FormField label={m.account_login_name()} required>
          <Input
            name="username"
            autocomplete="username webauthn"
            required
            value={login()}
            onInput={(event) => setLogin(event.currentTarget.value)}
          />
        </FormField>
        <FormField label={m.account_password()} required>
          <Input
            name="password"
            type="password"
            autocomplete="current-password"
            required
            maxLength={1024}
            value={password()}
            onInput={(event) => setPassword(event.currentTarget.value)}
          />
        </FormField>
        <Problem message={error()} />
        <Button type="submit" disabled={busy()}>
          {m.account_sign_in()}
        </Button>
      </form>
      <RecoveryLogin onSignedIn={props.onSignedIn} />
    </>
  )
}
