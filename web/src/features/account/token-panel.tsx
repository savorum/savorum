import { For, Show, createResource, createSignal } from "solid-js"

import { Callout, ChangeError, Notice } from "@/components/status"
import { Button } from "@/components/ui/button"
import { CopyField } from "@/components/ui/clipboard"
import { FieldGroup, FormField, ToggleField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { isolate } from "@/lib/direction"
import { formatNumber } from "@/lib/locale-format"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { AccountNav, accountStyles as styles } from "./account-layout"
import { accountRequest, authEnabled, libraries } from "./account-state"
import { formatEpoch } from "./formats"

type Token = {
  id: string
  name: string
  scopes: string[]
  libraries: string[]
  created_at: number
  expires_at: number
  last_used_at: number
  revoked_at: number
}
type Policy = { default_days: number; maximum_days: number; allow_nonexpiring: boolean }

const capabilities = [
  { id: "recipes:read", label: () => m.account_tokens_scope_recipes_read() },
  { id: "recipes:write", label: () => m.account_tokens_scope_recipes_write() },
  { id: "library:read", label: () => m.account_tokens_scope_library_read() },
  { id: "library:write", label: () => m.account_tokens_scope_library_write() },
  { id: "imports:write", label: () => m.account_tokens_scope_imports_write() },
]

export function TokensPage() {
  return (
    <main class={styles.page}>
      <div class={styles.header}>
        <h1 class={styles.heading}>{m.account_tokens_title()}</h1>
        <p class={styles.lede}>{m.account_tokens_lede()}</p>
      </div>
      <AccountNav />
      <Show
        when={authEnabled()}
        fallback={<p class={styles.section}>{m.account_local_mode_unavailable()}</p>}
      >
        <TokenPanel />
      </Show>
    </main>
  )
}

function tokenState(token: Token): string {
  if (token.revoked_at) return m.account_tokens_state_revoked()
  if (!token.expires_at) return m.account_tokens_state_permanent()
  if (token.expires_at <= Date.now() / 1000) return m.account_tokens_state_expired()
  return m.account_tokens_state_expires({ date: formatEpoch(token.expires_at) })
}

export function TokenPanel() {
  const [data, { refetch }] = createResource(async () =>
    accountRequest<{ tokens: Token[]; policy: Policy }>("/account/tokens"),
  )
  const [name, setName] = createSignal("")
  const [days, setDays] = createSignal("")
  const [scopes, setScopes] = createSignal<string[]>(["recipes:read"])
  const [chosen, setChosen] = createSignal<string[]>([])
  const [allLibraries, setAllLibraries] = createSignal(true)
  const [secret, setSecret] = createSignal("")
  const [error, setError] = createSignal<unknown>()
  const [notice, setNotice] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const canWrite = () => libraries().some((library) => library.role !== "viewer")
  function toggle(values: string[], id: string, checked: boolean) {
    return checked ? [...values, id] : values.filter((value) => value !== id)
  }
  async function create(event: SubmitEvent) {
    event.preventDefault()
    setBusy(true)
    setError(undefined)
    setNotice("")
    try {
      const grant = await accountRequest<{ secret: string }>("/account/tokens", "POST", {
        name: name(),
        days: Number(days()) || 0,
        nonexpiring: days() === "never",
        scopes: scopes(),
        libraries: allLibraries() ? [] : chosen(),
      })
      setSecret(grant.secret)
      setName("")
      await refetch()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  async function revoke(id: string) {
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest(`/account/tokens/${encodeURIComponent(id)}`, "DELETE")
      await refetch()
      setNotice(m.account_tokens_revoked())
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <ChangeError error={error()} />
      <Notice message={notice()} />
      <Show when={data.error}>
        <Callout
          tone="danger"
          actions={
            <Button size="sm" variant="outline" onClick={() => void refetch()}>
              {m.account_retry()}
            </Button>
          }
        >
          {m.account_tokens_error()}
        </Callout>
      </Show>
      <Show when={secret()}>
        <section class={styles.section}>
          <h2>{m.account_tokens_new_heading()}</h2>
          <p class={styles.muted}>{m.account_tokens_new_hint()}</p>
          <CopyField
            label={m.account_tokens_secret_label()}
            value={secret()}
            copyLabel={m.account_copy()}
            copiedLabel={m.account_copied()}
          />
          <div class={styles.actions}>
            <Button variant="outline" onClick={() => setSecret("")}>
              {m.account_tokens_dismiss()}
            </Button>
          </div>
        </section>
      </Show>
      <Show when={settled(data)}>
        <section class={styles.section}>
          <h2>{m.account_tokens_create_heading()}</h2>
          <form class={styles.form} onSubmit={(event) => void create(event)}>
            <FormField label={m.account_tokens_name()} required>
              <Input
                required
                maxLength={100}
                value={name()}
                onInput={(event) => setName(event.currentTarget.value)}
              />
            </FormField>
            <FieldGroup
              label={m.account_tokens_permissions()}
              hint={canWrite() ? undefined : m.account_tokens_write_disabled()}
            >
              <For each={capabilities}>
                {(capability) => (
                  <ToggleField
                    label={capability.label()}
                    checked={scopes().includes(capability.id)}
                    disabled={capability.id.endsWith(":write") && !canWrite()}
                    onChange={(checked) => setScopes(toggle(scopes(), capability.id, checked))}
                  />
                )}
              </For>
            </FieldGroup>
            <FieldGroup label={m.account_tokens_libraries()}>
              <ToggleField
                label={m.account_tokens_all_libraries()}
                checked={allLibraries()}
                onChange={setAllLibraries}
              />
              <Show when={!allLibraries()}>
                <For each={libraries()}>
                  {(library) => (
                    <ToggleField
                      label={library.name}
                      checked={chosen().includes(library.id)}
                      onChange={(checked) => setChosen(toggle(chosen(), library.id, checked))}
                    />
                  )}
                </For>
              </Show>
            </FieldGroup>
            <FormField label={m.account_tokens_expires()}>
              <Select
                value={days()}
                onValueChange={setDays}
                items={[
                  {
                    value: "",
                    label: m.account_tokens_expiry_default({
                      days: formatNumber(settled(data)?.policy.default_days ?? 90),
                    }),
                  },
                  ...[7, 30, 90, 180, 365]
                    .filter((value) => value <= (settled(data)?.policy.maximum_days ?? 365))
                    .map((value) => ({
                      value: String(value),
                      label: m.account_tokens_expiry_days({ days: formatNumber(value) }),
                    })),
                  ...(settled(data)?.policy.allow_nonexpiring
                    ? [{ value: "never", label: m.account_tokens_expiry_never() }]
                    : []),
                ]}
              />
            </FormField>
            <Button
              type="submit"
              disabled={
                busy() ||
                !!secret() ||
                scopes().length === 0 ||
                (!allLibraries() && chosen().length === 0)
              }
            >
              {m.account_tokens_create()}
            </Button>
          </form>
        </section>
      </Show>
      <Show when={settled(data)}>
        <section class={styles.section}>
          <h2>{m.account_tokens_existing_heading()}</h2>
          <Show when={settled(data)?.tokens.length} fallback={<p>{m.account_tokens_none()}</p>}>
            <ul class={styles.list}>
              <For each={settled(data)?.tokens}>
                {(token) => (
                  <li class={styles.card}>
                    <h3>{token.name}</h3>
                    <p class={styles.muted}>
                      {tokenState(token)} ·{" "}
                      {token.last_used_at
                        ? m.account_tokens_last_used({ date: formatEpoch(token.last_used_at) })
                        : m.account_tokens_never_used()}
                    </p>
                    <p>
                      {token.scopes
                        .map(
                          (scope) =>
                            capabilities.find((capability) => capability.id === scope)?.label() ??
                            scope,
                        )
                        .join(", ")}
                    </p>
                    <p>
                      {token.libraries.length
                        ? token.libraries
                            .map(
                              (id) =>
                                libraries().find((library) => library.id === id)?.name ??
                                m.account_tokens_unavailable_library(),
                            )
                            .join(", ")
                        : m.account_tokens_all_accessible()}
                    </p>
                    <Show when={!token.revoked_at}>
                      <div class={styles.actions}>
                        <Button
                          variant="outline"
                          disabled={busy()}
                          onClick={() => void revoke(token.id)}
                        >
                          {m.account_tokens_revoke({ name: isolate(token.name) })}
                        </Button>
                      </div>
                    </Show>
                  </li>
                )}
              </For>
            </ul>
          </Show>
        </section>
      </Show>
    </>
  )
}
