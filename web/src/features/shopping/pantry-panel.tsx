import { createResource, createSignal, For, Show } from "solid-js"
import { css } from "styled-system/css"

import { client, expectOk, ifMatch, unwrap } from "@/api/client"
import { createLiveRefresh } from "@/api/live-events"
import type { components } from "@/api/schema"
import { ChangeError, errorMessage, Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/disclosure"
import { Input } from "@/components/ui/input"
import { createAsyncAction } from "@/lib/create-async-action"
import { settled } from "@/lib/settled"
import { uniqueID } from "@/lib/unique-id"

import { m } from "../../lib/messages.ts"

type Entry = components["schemas"]["Entry"]
type Draft = components["schemas"]["PantryEntryInput"] & { id: string; revision: number }

export async function loadPantry() {
  return unwrap(await client.GET("/api/v1/pantry"), m.pantry_failed())
}

const styles = {
  body: css({ display: "grid", gap: "3", paddingBlockEnd: "3" }),
  hint: css({ color: "fg.muted", fontSize: "sm", margin: "0" }),
  add: css({ justifySelf: "start" }),
}

export function PantryPanel(props: { canEdit: boolean }) {
  const [entries, { refetch }] = createResource(loadPantry)
  createLiveRefresh(["pantry."], () => refetch())
  const [draft, setDraft] = createSignal<Draft>()
  const { busy, error, run } = createAsyncAction()
  const field = (key: keyof components["schemas"]["PantryEntryInput"], value: string) =>
    setDraft((current) => (current ? { ...current, [key]: value } : current))
  function save() {
    const value = draft()
    if (!value || busy()) return
    const { id, revision, ...body } = value
    void run(async () => {
      unwrap(
        await client.PUT("/api/v1/pantry/{id}", {
          params: {
            path: { id },
            header: revision ? { "If-Match": ifMatch(revision) } : { "If-None-Match": "*" },
          },
          body,
        }),
        m.pantry_failed(),
      )
      setDraft(undefined)
      await refetch()
    })
  }
  function remove(entry: Entry) {
    if (busy()) return
    void run(async () => {
      expectOk(
        await client.DELETE("/api/v1/pantry/{id}", {
          params: { path: { id: entry.id }, header: { "If-Match": ifMatch(entry.revision) } },
        }),
        m.pantry_failed(),
      )
      await refetch()
    })
  }
  return (
    <Disclosure label={m.pantry_title()}>
      <div class={styles.body}>
        <p class={styles.hint}>{m.pantry_hint()}</p>
        <ChangeError error={error()} fallback={m.pantry_failed()} onReload={() => void refetch()} />
        <Show when={entries.error}>
          <Problem message={errorMessage(entries.error, m.pantry_failed())} />
          <Button variant="outline" onClick={() => void refetch()}>
            {m.pantry_retry()}
          </Button>
        </Show>
        <For each={settled(entries) ?? []}>
          {(entry) => (
            <div
              class={css({ display: "flex", gap: "3", alignItems: "center", paddingBlock: "2" })}
            >
              <p class={css({ flex: "1" })}>
                {entry.name}{" "}
                {[entry.amount, entry.unit, entry.location].filter(Boolean).join(" · ")}
                <Show when={entry.note}>
                  <span class={css({ display: "block", color: "fg.muted" })}>{entry.note}</span>
                </Show>
              </p>
              <Show when={props.canEdit}>
                <Button
                  variant="plain"
                  disabled={busy()}
                  onClick={() =>
                    setDraft({
                      id: entry.id,
                      revision: entry.revision,
                      name: entry.name,
                      amount: entry.amount,
                      unit: entry.unit,
                      note: entry.note,
                      location: entry.location,
                      semantic_id: entry.semantic_id,
                    })
                  }
                >
                  {m.pantry_edit()}
                </Button>
                <Button variant="plain" disabled={busy()} onClick={() => remove(entry)}>
                  {m.pantry_remove()}
                </Button>
              </Show>
            </div>
          )}
        </For>
        <Show when={props.canEdit && !draft()}>
          <Button
            class={styles.add}
            variant="outline"
            onClick={() => setDraft({ id: uniqueID(), revision: 0, name: "" })}
          >
            {m.pantry_add()}
          </Button>
        </Show>
        <Show when={!!draft()}>
          <form
            class={css({ display: "grid", gap: "3", marginTop: "3" })}
            onSubmit={(event) => {
              event.preventDefault()
              save()
            }}
          >
            <label>
              {m.pantry_name()}
              <Input
                required
                maxLength={200}
                value={draft()?.name ?? ""}
                onInput={(event) => field("name", event.currentTarget.value)}
              />
            </label>
            <label>
              {m.pantry_amount()}
              <Input
                maxLength={100}
                value={draft()?.amount ?? ""}
                onInput={(event) => field("amount", event.currentTarget.value)}
              />
            </label>
            <label>
              {m.pantry_unit()}
              <Input
                maxLength={100}
                value={draft()?.unit ?? ""}
                onInput={(event) => field("unit", event.currentTarget.value)}
              />
            </label>
            <label>
              {m.pantry_location()}
              <Input
                maxLength={100}
                value={draft()?.location ?? ""}
                onInput={(event) => field("location", event.currentTarget.value)}
              />
            </label>
            <label>
              {m.pantry_note()}
              <Input
                maxLength={2000}
                value={draft()?.note ?? ""}
                onInput={(event) => field("note", event.currentTarget.value)}
              />
            </label>
            <div>
              <Button type="submit" disabled={busy()}>
                {m.pantry_save()}
              </Button>
              <Button
                type="button"
                variant="plain"
                disabled={busy()}
                onClick={() => setDraft(undefined)}
              >
                {m.pantry_cancel()}
              </Button>
            </div>
          </form>
        </Show>
      </div>
    </Disclosure>
  )
}
