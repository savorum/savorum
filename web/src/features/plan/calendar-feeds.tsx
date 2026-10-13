import { For, Show, createResource, createSignal } from "solid-js"
import { css } from "styled-system/css"

import {
  type CalendarFeed,
  type CalendarFeedSecret,
  createCalendarFeed,
  deleteCalendarFeed,
  feedAddress,
  listCalendarFeeds,
  rotateCalendarFeed,
} from "@/api/calendar-feeds"
import { ChangeError, Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { CopyField } from "@/components/ui/clipboard"
import { ConfirmDialog, Dialog } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { createAsyncAction } from "@/lib/create-async-action"
import { formatDate } from "@/lib/date-format"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"

const styles = {
  stack: css({ display: "grid", gap: "4", marginTop: "4" }),
  row: css({
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    alignItems: "center",
    justifyContent: "space-between",
    paddingBlock: "3",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
  }),
  muted: css({ color: "fg.muted", fontSize: "sm" }),
  actions: css({ display: "flex", flexWrap: "wrap", gap: "2" }),
}

export function CalendarFeeds(props: { onClose: () => void }) {
  const [feeds, { refetch }] = createResource(listCalendarFeeds)
  const [name, setName] = createSignal<string>(m.plan_calendar_feed_default_name())
  const [secret, setSecret] = createSignal<CalendarFeedSecret>()
  const [revoking, setRevoking] = createSignal<CalendarFeed>()
  const change = createAsyncAction({ onSettled: () => void refetch() })
  const { busy, error } = change
  const run = (action: () => Promise<void>) => {
    if (!busy()) void change.run(action)
  }

  const revoke = (feed: CalendarFeed) =>
    run(async () => {
      await deleteCalendarFeed(feed.id)
      setSecret((shown) => (shown?.feed.id === feed.id ? undefined : shown))
      setRevoking(undefined)
    })

  const webcal = (address: string) => address.replace(/^https?:/, "webcal:")

  return (
    <Dialog
      open
      title={m.plan_calendar_feeds()}
      description={m.plan_calendar_feeds_hint()}
      onClose={props.onClose}
      footer={<Button onClick={props.onClose}>{m.plan_calendar_done()}</Button>}
    >
      <div class={styles.stack}>
        <Show when={secret()} keyed>
          {(shown) => (
            <section class={styles.stack} aria-live="polite">
              <p role="note">{m.plan_calendar_feed_warning()}</p>
              <CopyField
                label={shown.feed.name}
                value={feedAddress(shown)}
                copyLabel={m.plan_calendar_feed_copy()}
                copiedLabel={m.plan_calendar_feed_copied()}
              />
              <a href={webcal(feedAddress(shown))}>{m.plan_calendar_feed_subscribe()}</a>
            </section>
          )}
        </Show>
        <form
          class={styles.actions}
          onSubmit={(event) => {
            event.preventDefault()
            run(async () => {
              setSecret(await createCalendarFeed(name().trim()))
            })
          }}
        >
          <label class={css({ display: "grid", gap: "1", flex: "1", minWidth: "12rem" })}>
            {m.plan_calendar_feed_name()}
            <Input
              value={name()}
              maxLength={100}
              onInput={(event) => setName(event.currentTarget.value)}
            />
          </label>
          <Button
            type="submit"
            disabled={!name().trim() || busy()}
            class={css({ alignSelf: "end" })}
          >
            {m.plan_calendar_feed_create()}
          </Button>
        </form>
        <ChangeError
          error={revoking() ? undefined : error()}
          fallback={m.plan_calendar_feed_failed()}
        />
        <Show when={feeds.error}>
          <Problem message={m.plan_calendar_feeds_load_failed()} />
        </Show>
        <div>
          <For
            each={settled(feeds) ?? []}
            fallback={<p class={styles.muted}>{m.plan_calendar_feeds_empty()}</p>}
          >
            {(feed) => (
              <div class={styles.row}>
                <div>
                  <div>{feed.name}</div>
                  <div class={styles.muted}>
                    <Show when={!feed.mine}>
                      {m.plan_calendar_feed_created_by({ name: isolate(feed.created_by_name) })}{" "}
                      ·{" "}
                    </Show>
                    {feed.last_read_at
                      ? m.plan_calendar_feed_last_read({ date: formatDate(feed.last_read_at) })
                      : m.plan_calendar_feed_never_read()}
                  </div>
                </div>
                <div class={styles.actions}>
                  <Show when={feed.mine}>
                    <Button
                      variant="outline"
                      disabled={busy()}
                      onClick={() =>
                        run(async () => {
                          setSecret(await rotateCalendarFeed(feed.id))
                        })
                      }
                    >
                      {m.plan_calendar_feed_rotate()}
                    </Button>
                  </Show>
                  <Button
                    variant="outline"
                    disabled={busy()}
                    onClick={() => {
                      change.setError(undefined)
                      setRevoking(feed)
                    }}
                  >
                    {m.plan_calendar_feed_revoke()}
                  </Button>
                </div>
              </div>
            )}
          </For>
        </div>
      </div>
      <Show when={revoking()} keyed>
        {(feed) => (
          <ConfirmDialog
            title={m.plan_calendar_feed_revoke_title()}
            description={m.plan_calendar_feed_revoke_description()}
            confirmLabel={m.plan_calendar_feed_revoke()}
            cancelLabel={m.plan_cancel()}
            busy={busy()}
            onClose={() => setRevoking(undefined)}
            onConfirm={() => revoke(feed)}
          >
            <ChangeError error={error()} fallback={m.plan_calendar_feed_failed()} />
          </ConfirmDialog>
        )}
      </Show>
    </Dialog>
  )
}
