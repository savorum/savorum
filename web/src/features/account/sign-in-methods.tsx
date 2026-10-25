import { For, Show, createResource, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { loadCapabilities } from "@/api/capabilities"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { accountRequest } from "./account-state"

export type Method = { id: string; provider: string; name: string; enabled: boolean }
export type Provider = { id: string; kind: string; display_name: string }

export async function readProviders(): Promise<Provider[]> {
  const available = await loadCapabilities().catch(() => undefined)
  if (available)
    return (available.auth_methods ?? [])
      .filter((method) => ["oidc", "ldap", "saml"].includes(method.kind))
      .map((method) => ({ id: method.id, kind: method.kind, display_name: method.name }))
  return (await accountRequest<{ providers: Provider[] }>("/auth/providers")).providers
}

function methodLabel(method: Method): string {
  return method.enabled ? method.name : `${method.name} · ${m.account_methods_unavailable()}`
}

/** The ways this person can sign in, and the fresh authentication to add one. */
export function LinkedSignInMethods() {
  const [providers] = createResource(readProviders)
  const [provider, setProvider] = createSignal("")
  const [login, setLogin] = createSignal("")
  const [password, setPassword] = createSignal("")
  const [linking, setLinking] = createSignal(false)
  const [methods, { refetch }] = createResource(
    async () => (await accountRequest<{ methods: Method[] }>("/account/methods")).methods,
  )
  const [error, setError] = createSignal<unknown>()
  const [removing, setRemoving] = createSignal("")
  async function link(event: SubmitEvent) {
    event.preventDefault()
    setLinking(true)
    setError(undefined)
    try {
      const result = await accountRequest<{ authorization_url?: string }>(
        `/auth/${encodeURIComponent(provider())}/login`,
        "POST",
        { link: true, login: login(), password: password() },
      )
      if (result.authorization_url) window.location.assign(result.authorization_url)
      else window.location.reload()
    } catch (failure) {
      setError(failure)
      setLinking(false)
    }
  }
  async function remove(id: string) {
    setError(undefined)
    try {
      await accountRequest(`/account/methods/${encodeURIComponent(id)}`, "DELETE")
      await refetch()
      window.location.reload()
    } catch (failure) {
      setError(failure)
      setRemoving("")
    }
  }
  // A local password alone is already presented as the password section, so the
  // list appears once a provider is configured or another identity is attached.
  const relevant = () =>
    (settled(providers)?.length ?? 0) > 0 ||
    (settled(methods) ?? []).some((method) => method.provider !== "local")
  return (
    <Show when={relevant()}>
      <section class={styles.section}>
        <h2>{m.account_methods_heading()}</h2>
        <p class={styles.muted}>{m.account_methods_hint()}</p>
        <ChangeError error={error() ?? methods.error} />
        <ul class={styles.list}>
          <For each={settled(methods)}>
            {(method) => (
              <li class={styles.row}>
                <span>{methodLabel(method)}</span>
                <Show
                  when={removing() === method.id}
                  fallback={
                    <Button
                      size="sm"
                      variant="outline"
                      aria-label={m.account_methods_remove_named({ method: method.name })}
                      onClick={() => setRemoving(method.id)}
                    >
                      {m.account_methods_remove()}
                    </Button>
                  }
                >
                  <Button size="sm" onClick={() => void remove(method.id)}>
                    {m.account_methods_confirm()}
                  </Button>
                  <Button size="sm" variant="plain" onClick={() => setRemoving("")}>
                    {m.common_cancel()}
                  </Button>
                </Show>
              </li>
            )}
          </For>
        </ul>
        <Show when={settled(providers)?.length}>
          <form class={styles.form} onSubmit={(event) => void link(event)}>
            <FormField label={m.account_methods_add()} required>
              <Select
                required
                placeholder={m.account_methods_choose()}
                value={provider()}
                onValueChange={setProvider}
                items={(settled(providers) ?? []).map((item) => ({
                  value: item.id,
                  label: item.display_name,
                }))}
              />
            </FormField>
            <Show
              when={settled(providers)?.find((item) => item.id === provider())?.kind === "ldap"}
            >
              <FormField label={m.account_methods_provider_username()} required>
                <Input
                  required
                  autocomplete="username"
                  value={login()}
                  onInput={(event) => setLogin(event.currentTarget.value)}
                />
              </FormField>
              <FormField label={m.account_methods_provider_password()} required>
                <Input
                  required
                  type="password"
                  autocomplete="current-password"
                  value={password()}
                  onInput={(event) => setPassword(event.currentTarget.value)}
                />
              </FormField>
            </Show>
            <Button type="submit" disabled={linking() || !provider()}>
              {m.account_methods_verify()}
            </Button>
          </form>
        </Show>
      </section>
    </Show>
  )
}

/** An administrator's forced unlink, kept beside the person it affects. */
export function ManagedSignInMethods(props: { userID: string; displayName: string }) {
  const [methods, { refetch }] = createResource(
    () => props.userID,
    async (id) =>
      (await accountRequest<{ methods: Method[] }>(`/users/${encodeURIComponent(id)}/methods`))
        .methods,
  )
  const [selected, setSelected] = createSignal("")
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  async function remove() {
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest(
        `/users/${encodeURIComponent(props.userID)}/methods/${encodeURIComponent(selected())}`,
        "DELETE",
      )
      setSelected("")
      await refetch()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Show when={methods.error || settled(methods)?.length}>
      <div class={css({ display: "grid", gap: "2" })}>
        <h3 class={css({ fontSize: "sm", fontWeight: "medium" })}>{m.account_methods_heading()}</h3>
        <ChangeError error={error() ?? methods.error} />
        <ul class={styles.list}>
          <For each={settled(methods)}>
            {(method) => (
              <li class={styles.row}>
                <span class={styles.rowLabel}>{methodLabel(method)}</span>
                <Show
                  when={selected() === method.id}
                  fallback={
                    // The last way to sign in cannot be taken away.
                    <Show when={(settled(methods)?.length ?? 0) > 1}>
                      <Button
                        size="sm"
                        variant="outline"
                        aria-label={m.account_managed_force_named({
                          method: method.name,
                          name: props.displayName,
                        })}
                        onClick={() => setSelected(method.id)}
                      >
                        {m.account_managed_force()}
                      </Button>
                    </Show>
                  }
                >
                  <span class={styles.muted}>{m.account_managed_methods_hint()}</span>
                  <Button size="sm" disabled={busy()} onClick={() => void remove()}>
                    {m.account_managed_force_confirm()}
                  </Button>
                  <Button size="sm" variant="plain" onClick={() => setSelected("")}>
                    {m.common_cancel()}
                  </Button>
                </Show>
              </li>
            )}
          </For>
        </ul>
      </div>
    </Show>
  )
}
