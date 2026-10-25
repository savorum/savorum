import { Show, createResource, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { SkeletonRows } from "@/components/skeleton"
import { ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { accountRequest, libraries, type AccountUser } from "./account-state"
import { auditActor, type AuditEvent } from "./audit-presentation"
import { AuditRecords } from "./audit-records"

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

export function AuditView(props: { library?: string }) {
  const [category, setCategory] = createSignal("")
  const [actor, setActor] = createSignal("")
  const [target, setTarget] = createSignal("")
  const [since, setSince] = createSignal("")
  const [until, setUntil] = createSignal("")
  const [query, setQuery] = createSignal("")
  const [previousPages, setPreviousPages] = createSignal<string[]>([])
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
  function apply(before?: number) {
    const params = before
      ? new URLSearchParams(query())
      : new URLSearchParams({ category: category(), actor: actor(), target: target() })
    if (!before) {
      if (since()) params.set("since", String(Math.floor(new Date(since()).getTime() / 1000)))
      if (until()) params.set("until", String(Math.floor(new Date(until()).getTime() / 1000)))
    }
    if (before) {
      params.set("before", String(before))
      setPreviousPages([...previousPages(), query()])
    } else setPreviousPages([])
    setQuery(params.toString())
  }
  return (
    <>
      <form
        class={css({
          display: "grid",
          gap: "3",
          minWidth: "0",
          gridTemplateColumns: {
            base: "minmax(0, 1fr) auto",
            md: "minmax(0, 1fr) minmax(0, 1fr) auto auto",
          },
          alignItems: "start",
        })}
        onSubmit={(event) => {
          event.preventDefault()
          apply()
        }}
      >
        <div
          class={css({
            display: { base: "grid", md: "contents" },
            gridColumn: "1 / -1",
            gridTemplateColumns: "repeat(2, minmax(0, 1fr))",
            gap: "3",
            alignItems: "end",
          })}
        >
          <FormField label={m.account_audit_activity()}>
            <Select
              value={category()}
              onValueChange={setCategory}
              items={categories.map((entry) => ({ value: entry.id, label: entry.label() }))}
            />
          </FormField>
          <FormField label={m.account_audit_by()}>
            <Select
              value={actor()}
              onValueChange={setActor}
              items={[
                { value: "", label: m.account_audit_anyone() },
                ...Object.entries(settled(names) ?? {})
                  .map(([id, name]) => ({ value: id, label: name }))
                  .sort((a, b) => a.label.localeCompare(b.label)),
                ...["system", "instance-owner", "hosting"]
                  .filter((id) => !settled(names)?.[id])
                  .map((id) => ({ value: id, label: auditActor(id, {}) })),
                ...(actor() &&
                !Object.hasOwn(settled(names) ?? {}, actor()) &&
                !["system", "instance-owner", "hosting"].includes(actor())
                  ? [{ value: actor(), label: actor() }]
                  : []),
              ]}
            />
          </FormField>
        </div>
        <Disclosure
          variant="inline"
          label={m.library_filters_more()}
          class={css({
            display: "contents",
            "& > [data-part=trigger]": {
              gridColumn: { base: "1", md: "3" },
              gridRow: { base: "2", md: "1" },
              justifySelf: "start",
              alignSelf: "end",
            },
            "& > [data-part=content]": { gridColumn: "1 / -1", gridRow: { base: "3", md: "2" } },
          })}
        >
          <div class={css({ display: "grid", gap: "3", paddingTop: "2" })}>
            <div
              class={css({
                display: "grid",
                gap: "3",
                gridTemplateColumns: { base: "1fr", sm: "repeat(2, minmax(0, 1fr))" },
              })}
            >
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
            <div
              class={css({
                display: "grid",
                gap: "3",
                gridTemplateColumns: { base: "1fr", sm: "repeat(2, minmax(0, 1fr))" },
              })}
            >
              <FormField label={m.account_audit_actor()}>
                <Input value={actor()} onInput={(event) => setActor(event.currentTarget.value)} />
              </FormField>
              <FormField label={m.account_audit_target()}>
                <Input value={target()} onInput={(event) => setTarget(event.currentTarget.value)} />
              </FormField>
            </div>
            <p class={styles.muted}>{m.account_audit_identifier_hint()}</p>
          </div>
        </Disclosure>
        <Button
          class={css({
            justifySelf: "end",
            gridColumn: { base: "2", md: "4" },
            gridRow: { base: "2", md: "1" },
            alignSelf: "end",
          })}
          type="submit"
          variant="outline"
          disabled={events.loading}
        >
          {m.account_audit_apply()}
        </Button>
      </form>
      <ResourceState
        resource={events}
        fallback={m.account_audit_error()}
        loading={<SkeletonRows count={6} />}
        isEmpty={(items) => items.length === 0}
        empty={<p>{m.account_audit_empty()}</p>}
      >
        {(loaded) => (
          <AuditRecords events={loaded()} names={settled(names) ?? {}} libraries={libraries()} />
        )}
      </ResourceState>
      <Show when={settled(events)?.length === 100 || previousPages().length > 0}>
        <div class={styles.actions}>
          <Show when={previousPages().length > 0}>
            <Button
              variant="outline"
              disabled={events.loading}
              onClick={() => {
                const pages = previousPages()
                setQuery(pages.at(-1)!)
                setPreviousPages(pages.slice(0, -1))
              }}
            >
              {m.common_previous()}
            </Button>
          </Show>
          <Button
            variant="outline"
            onClick={() => apply(settled(events)?.at(-1)?.id)}
            disabled={events.loading || settled(events)?.length !== 100}
          >
            {m.account_audit_older()}
          </Button>
        </div>
      </Show>
    </>
  )
}
