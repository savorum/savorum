import { For, Show, createResource, createSignal } from "solid-js"

import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { accountRequest } from "./account-state"
import { rememberAssistantApproval } from "./assistant-return"
import { invitationToken } from "./invitations"
import { rememberNativeBrowserReturn } from "./native-browser"
import { readProviders, type Provider } from "./sign-in-methods"

export function ProviderLogin() {
  const [providers] = createResource(readProviders)
  return (
    <Show when={settled(providers)?.length}>
      <section class={styles.section} aria-label={m.account_login_other_options()}>
        <h2>{m.account_login_other_options()}</h2>
        <For each={settled(providers)}>{(provider) => <ProviderOption provider={provider} />}</For>
      </section>
    </Show>
  )
}

function ProviderOption(props: { provider: Provider }) {
  const [login, setLogin] = createSignal("")
  const [password, setPassword] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  async function signIn() {
    setBusy(true)
    setError(undefined)
    try {
      const result = await accountRequest<{
        authorization_url?: string
        invitation_accepted?: boolean
      }>(`/auth/${encodeURIComponent(props.provider.id)}/login`, "POST", {
        login: login(),
        password: password(),
        invitation_token: invitationToken(),
      })
      setPassword("")
      if (result.invitation_accepted) sessionStorage.removeItem("savorum.invitation")
      if (result.authorization_url) {
        rememberNativeBrowserReturn()
        rememberAssistantApproval()
        window.location.assign(result.authorization_url)
      } else {
        rememberNativeBrowserReturn()
        window.location.reload()
      }
    } catch (failure) {
      setError(failure)
      setBusy(false)
    }
  }
  return (
    <div>
      <Show
        when={props.provider.kind === "ldap"}
        fallback={
          <Button disabled={busy()} onClick={() => void signIn()}>
            {m.account_login_continue_with({ provider: isolate(props.provider.display_name) })}
          </Button>
        }
      >
        <Disclosure label={m.account_login_with({ provider: props.provider.display_name })}>
          <form
            class={styles.form}
            onSubmit={(event) => {
              event.preventDefault()
              void signIn()
            }}
          >
            <FormField label={m.account_login_directory_username()} required>
              <Input
                required
                autocomplete="username"
                value={login()}
                onInput={(event) => setLogin(event.currentTarget.value)}
              />
            </FormField>
            <FormField label={m.account_login_directory_password()} required>
              <Input
                required
                type="password"
                autocomplete="current-password"
                maxLength={1024}
                value={password()}
                onInput={(event) => setPassword(event.currentTarget.value)}
              />
            </FormField>
            <Button type="submit" disabled={busy()}>
              {m.account_login_with({ provider: isolate(props.provider.display_name) })}
            </Button>
          </form>
        </Disclosure>
      </Show>
      <ChangeError error={error()} />
    </div>
  )
}
