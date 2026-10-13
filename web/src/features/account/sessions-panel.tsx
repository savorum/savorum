import { For, Show, createResource } from "solid-js"

import { Callout, ChangeError, Notice } from "@/components/status"
import { Button } from "@/components/ui/button"
import { createAsyncAction } from "@/lib/create-async-action"
import { formatDateTime } from "@/lib/date-format"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { accountRequest } from "./account-state"
import { readProviders } from "./sign-in-methods"

type Session = {
  id: string
  created_at: string
  last_seen_at: string
  auth_method: string
  current: boolean
}

/** Browser sessions, with the method that created each one named in words. */
export function SessionsPanel() {
  const [providers] = createResource(readProviders)
  const [sessions, { refetch }] = createResource(
    async () => (await accountRequest<{ sessions: Session[] }>("/account/sessions")).sessions,
  )
  const { busy, error, notice, run } = createAsyncAction({ onSettled: () => void refetch() })
  function methodName(method: string): string {
    if (method === "local") return m.account_method_local()
    if (method === "passkey") return m.account_method_passkey()
    if (method === "recovery") return m.account_method_recovery()
    return settled(providers)?.find((provider) => provider.id === method)?.display_name ?? method
  }
  return (
    <section class={styles.section}>
      <h2>{m.account_sessions_heading()}</h2>
      <p class={styles.muted}>{m.account_sessions_hint()}</p>
      <Show when={sessions.error}>
        <Callout tone="danger">{m.account_sessions_error()}</Callout>
      </Show>
      <ChangeError error={error()} />
      <Notice message={notice()} />
      <ul class={styles.list}>
        <For each={settled(sessions)}>
          {(session) => (
            <li class={styles.card}>
              <h3>{session.current ? m.account_sessions_this() : m.account_sessions_other()}</h3>
              <p class={styles.muted}>
                {m.account_sessions_detail({
                  method: isolate(methodName(session.auth_method)),
                  last: formatDateTime(session.last_seen_at),
                  created: formatDateTime(session.created_at),
                })}
              </p>
              <Show when={!session.current}>
                <div class={styles.actions}>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy()}
                    aria-label={m.account_sessions_revoke_named({
                      last: formatDateTime(session.last_seen_at),
                    })}
                    onClick={() =>
                      void run(
                        () =>
                          accountRequest(
                            `/account/sessions/${encodeURIComponent(session.id)}`,
                            "DELETE",
                          ),
                        m.account_sessions_revoked(),
                      )
                    }
                  >
                    {m.account_sessions_revoke()}
                  </Button>
                </div>
              </Show>
            </li>
          )}
        </For>
      </ul>
      <Show when={(settled(sessions)?.length ?? 0) > 1}>
        <div class={styles.actions}>
          <Button
            variant="outline"
            disabled={busy()}
            onClick={() =>
              void run(
                () => accountRequest("/account/sessions", "DELETE"),
                m.account_sessions_others_revoked(),
              )
            }
          >
            {m.account_sessions_revoke_others()}
          </Button>
        </div>
      </Show>
    </section>
  )
}
