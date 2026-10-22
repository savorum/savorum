import { For, Show, createResource, createSignal } from "solid-js"

import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { CopyField } from "@/components/ui/clipboard"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { accountRequest, libraries } from "./account-state"
import { formatEpoch } from "./formats"

type Connection = {
  id: string
  name: string
  scopes: string[]
  libraries: string[]
  expires_at: number
  last_used_at: number
}

export function AssistantPanel() {
  const [data, { refetch }] = createResource(() =>
    accountRequest<{ connections: Connection[] | null }>("/account/connections"),
  )
  const connections = () => settled(data)?.connections ?? []
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  const prompt = () =>
    m.account_assistant_setup_prompt({ guide: `${window.location.origin}/llms.txt` })
  async function disconnect(id: string) {
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest(`/account/connections/${encodeURIComponent(id)}`, "DELETE")
      await refetch()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  return (
    <section class={styles.section} aria-label={m.account_assistant_heading()}>
      <h2>{m.account_assistant_heading()}</h2>
      <p class={styles.muted}>{m.account_assistant_setup_hint()}</p>
      <CopyField
        label={m.account_assistant_prompt_label()}
        value={prompt()}
        copyLabel={m.account_copy()}
        copiedLabel={m.account_copied()}
      />
      <ChangeError error={error() ?? data.error} />
      <Show when={data.error}>
        <Button variant="outline" onClick={() => void refetch()}>
          {m.account_retry()}
        </Button>
      </Show>
      <Show when={connections().length}>
        <h3>{m.account_assistant_connections()}</h3>
        <ul class={styles.list}>
          <For each={connections()}>
            {(connection) => (
              <li class={styles.card}>
                <h4>{connection.name}</h4>
                <p>
                  {connection.libraries
                    .map(
                      (id) =>
                        libraries().find((l) => l.id === id)?.name ??
                        m.account_tokens_unavailable_library(),
                    )
                    .join(", ")}{" "}
                  ·{" "}
                  {connection.scopes.includes("recipes:write")
                    ? m.account_assistant_access_write()
                    : m.account_assistant_access_read()}
                </p>
                <p class={styles.muted}>
                  {m.account_tokens_state_expires({ date: formatEpoch(connection.expires_at) })} ·{" "}
                  {connection.last_used_at
                    ? m.account_tokens_last_used({ date: formatEpoch(connection.last_used_at) })
                    : m.account_tokens_never_used()}
                </p>
                <div class={styles.actions}>
                  <Button
                    variant="outline"
                    disabled={busy()}
                    onClick={() => void disconnect(connection.id)}
                  >
                    {m.connect_disconnect()}
                  </Button>
                </div>
              </li>
            )}
          </For>
        </ul>
      </Show>
    </section>
  )
}
