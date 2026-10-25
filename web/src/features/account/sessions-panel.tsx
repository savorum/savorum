import { For, Show, createMemo, createResource, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { Callout, ChangeError, Notice } from "@/components/status"
import { Button } from "@/components/ui/button"
import { createAsyncAction } from "@/lib/create-async-action"
import { formatDateTime } from "@/lib/date-format"
import { formatNumber } from "@/lib/locale-format"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { accountRequest } from "./account-state"
import { readProviders } from "./sign-in-methods"

type Session = {
  id: string
  created_at: string
  last_seen_at: string
  browser_handoff?: boolean
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
  const ordered = createMemo(() =>
    [...(settled(sessions) ?? [])].sort(
      (a, b) =>
        Number(b.current) - Number(a.current) || b.last_seen_at.localeCompare(a.last_seen_at),
    ),
  )
  const [page, setPage] = createSignal(0)
  const others = () => ordered().filter((session) => !session.current)
  const lastPage = () => Math.max(0, Math.ceil(others().length / 4) - 1)
  const currentPage = () => Math.min(page(), lastPage())
  const shown = () => [
    ...ordered().filter((session) => session.current),
    ...others().slice(currentPage() * 4, currentPage() * 4 + 4),
  ]
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
      <ul class={styles.rows}>
        <For each={shown()}>
          {(session) => (
            <li class={styles.listRow}>
              <div class={styles.rowBody}>
                <div class={styles.actions}>
                  <h3>{methodName(session.auth_method)}</h3>
                  <Show when={session.browser_handoff}>
                    <span class={styles.muted}>{m.account_sessions_browser_handoff()}</span>
                  </Show>
                  <Show when={session.current}>
                    <span
                      class={css({ color: "fg.default", fontSize: "xs", fontWeight: "medium" })}
                    >
                      {m.account_sessions_this()}
                    </span>
                  </Show>
                </div>
                <div
                  class={css({ display: "flex", flexWrap: "wrap", columnGap: "3", rowGap: "1" })}
                >
                  <p class={styles.muted}>
                    <time dateTime={session.last_seen_at}>
                      {m.account_sessions_last_active({
                        date: formatDateTime(session.last_seen_at),
                      })}
                    </time>
                  </p>
                  <p class={styles.muted}>
                    <time dateTime={session.created_at}>
                      {m.account_sessions_signed_in({ date: formatDateTime(session.created_at) })}
                    </time>
                  </p>
                </div>
              </div>
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
      <Show when={others().length > 4}>
        <div class={styles.listRow}>
          <p class={styles.muted}>
            {m.account_sessions_other_heading()}
            {" · "}
            {m.common_pagination_range({
              value1: formatNumber(currentPage() * 4 + 1),
              value2: formatNumber(Math.min(currentPage() * 4 + 4, others().length)),
              value3: formatNumber(others().length),
            })}
          </p>
          <div class={styles.actions}>
            <Button
              size="sm"
              variant="outline"
              disabled={busy() || currentPage() === 0}
              onClick={() => setPage(currentPage() - 1)}
            >
              {m.common_previous()}
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={busy() || currentPage() === lastPage()}
              onClick={() => setPage(currentPage() + 1)}
            >
              {m.common_next()}
            </Button>
          </div>
        </div>
      </Show>
      <Show when={others().length > 0}>
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
