import { For, Show, createResource, createSignal } from "solid-js"

import { Loading, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { accountRequest, type AccountUser } from "./account-state"
import { formatEpoch } from "./formats"

type AuditEvent = {
  id: number
  created_at: number
  actor_id: string
  action: string
  target_type: string
  target_id: string
  outcome: string
}
type Member = { user_id: string; display_name: string }

const categories = [
  { id: "", label: () => m.account_audit_all() },
  { id: "auth", label: () => m.account_audit_auth() },
  { id: "user", label: () => m.account_audit_users() },
  { id: "library", label: () => m.account_audit_library() },
  { id: "recipe", label: () => m.account_audit_recipes() },
  { id: "book", label: () => m.account_audit_books() },
  { id: "import", label: () => m.account_audit_imports() },
  { id: "token", label: () => m.account_audit_tokens() },
  { id: "settings", label: () => m.account_audit_settings() },
]

const targetTypes: Record<string, () => string> = {
  authentication: () => m.account_audit_target_authentication(),
  book: () => m.account_audit_target_book(),
  invitation: () => m.account_audit_target_invitation(),
  library: () => m.account_audit_target_library(),
  mapping: () => m.account_audit_target_mapping(),
  passkey: () => m.account_audit_target_passkey(),
  provider: () => m.account_audit_target_provider(),
  recipe: () => m.account_audit_target_recipe(),
  session: () => m.account_audit_target_session(),
  settings: () => m.account_audit_target_settings(),
  token: () => m.account_audit_target_token(),
  user: () => m.account_audit_target_user(),
}

function targetTypeName(type: string): string {
  return targetTypes[type]?.() ?? type
}

/**
 * The append-only record. Action names stay verbatim because they are the
 * record; the people are named wherever this viewer may read their names.
 */
export function AuditView(props: { library?: string }) {
  const [category, setCategory] = createSignal("")
  const [actor, setActor] = createSignal("")
  const [target, setTarget] = createSignal("")
  const [since, setSince] = createSignal("")
  const [until, setUntil] = createSignal("")
  const [query, setQuery] = createSignal("")
  const [events] = createResource(
    () => `${props.library ?? ""}:${query()}`,
    async () => {
      const params = new URLSearchParams(query())
      if (props.library) params.set("library", props.library)
      return (await accountRequest<{ events: AuditEvent[] }>(`/account/audit?${params}`)).events
    },
  )
  const [names] = createResource(
    () => props.library ?? "",
    async (library) =>
      library
        ? Object.fromEntries(
            (
              await accountRequest<{ members: Member[] }>(
                `/libraries/${encodeURIComponent(library)}/members`,
              )
            ).members.map((member) => [member.user_id, member.display_name]),
          )
        : Object.fromEntries(
            (await accountRequest<{ users: AccountUser[] }>("/users")).users.map((user) => [
              user.id,
              user.display_name,
            ]),
          ),
  )
  function personName(id: string): string {
    return settled(names)?.[id] ?? id
  }
  function targetName(event: AuditEvent): string {
    return event.target_type === "user" ? personName(event.target_id) : event.target_id
  }
  function apply(before?: number) {
    const params = new URLSearchParams({ category: category(), actor: actor(), target: target() })
    if (since()) params.set("since", String(Math.floor(new Date(since()).getTime() / 1000)))
    if (until()) params.set("until", String(Math.floor(new Date(until()).getTime() / 1000)))
    if (before) params.set("before", String(before))
    setQuery(params.toString())
  }
  return (
    <>
      <p class={styles.muted}>{m.account_audit_hint()}</p>
      <form
        class={styles.form}
        onSubmit={(event) => {
          event.preventDefault()
          apply()
        }}
      >
        <FormField label={m.account_audit_activity()}>
          <Select
            value={category()}
            onValueChange={setCategory}
            items={categories.map((entry) => ({ value: entry.id, label: entry.label() }))}
          />
        </FormField>
        <div class={styles.row}>
          <FormField label={m.account_audit_from()}>
            <Input
              type="datetime-local"
              value={since()}
              onInput={(event) => setSince(event.currentTarget.value)}
            />
          </FormField>
          <FormField label={m.account_audit_until()}>
            <Input
              type="datetime-local"
              value={until()}
              onInput={(event) => setUntil(event.currentTarget.value)}
            />
          </FormField>
        </div>
        <div class={styles.row}>
          <FormField label={m.account_audit_actor()}>
            <Input value={actor()} onInput={(event) => setActor(event.currentTarget.value)} />
          </FormField>
          <FormField label={m.account_audit_target()}>
            <Input value={target()} onInput={(event) => setTarget(event.currentTarget.value)} />
          </FormField>
        </div>
        <p class={styles.muted}>{m.account_audit_identifier_hint()}</p>
        <Button type="submit" variant="outline" disabled={events.loading}>
          {m.account_audit_apply()}
        </Button>
      </form>
      <ResourceState
        resource={events}
        fallback={m.account_audit_error()}
        loading={<Loading>{m.account_audit_loading()}</Loading>}
        isEmpty={(items) => items.length === 0}
        empty={<p>{m.account_audit_empty()}</p>}
      >
        {(loaded) => (
          <ol class={styles.list}>
            <For each={loaded()}>
              {(event) => (
                <li class={styles.card}>
                  <h3>{event.action}</h3>
                  <time
                    class={styles.muted}
                    datetime={new Date(event.created_at * 1000).toISOString()}
                  >
                    {formatEpoch(event.created_at)}
                  </time>
                  <p class={styles.muted}>
                    {m.account_audit_event_detail({
                      actor: isolate(personName(event.actor_id)),
                      targetType: isolate(targetTypeName(event.target_type)),
                      target: isolate(targetName(event)),
                      outcome: event.outcome,
                    })}
                  </p>
                </li>
              )}
            </For>
          </ol>
        )}
      </ResourceState>
      <Show when={settled(events)?.length === 100}>
        <div class={styles.actions}>
          <Button
            variant="outline"
            onClick={() => apply(settled(events)?.at(-1)?.id)}
            disabled={events.loading}
          >
            {m.account_audit_older()}
          </Button>
        </div>
      </Show>
    </>
  )
}
