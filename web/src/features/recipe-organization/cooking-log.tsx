import { Pencil, Trash2 } from "lucide-solid"
import { For, Show, createResource, createSignal } from "solid-js"
import { css } from "styled-system/css"

import type { CookingRecord } from "@/api/client"
import { deleteCooking, listCookings, recordCooking, replaceCooking } from "@/api/cookings"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { createAsyncAction } from "@/lib/create-async-action"
import { formatDate } from "@/lib/date-format"
import { calendarDate } from "@/lib/date-format"
import { type Rational, compare, formatRational, rational } from "@/lib/rational"
import { uniqueID } from "@/lib/unique-id"

import { m } from "../../lib/messages.ts"

const styles = {
  root: css({ minWidth: "0", width: "100%" }),
  bar: css({
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "3",
    marginBottom: "3",
  }),
  title: css({ fontSize: "md", margin: "0" }),
  muted: css({ color: "fg.muted", fontSize: "sm", margin: "0" }),
  list: css({
    listStyle: "none",
    margin: "0",
    padding: "0",
    maxHeight: "24rem",
    overflowY: "auto",
  }),
  entry: css({
    display: "grid",
    gridTemplateColumns: "minmax(0,1fr) auto",
    gap: "2",
    borderTopWidth: "1px",
    borderColor: "border.subtle",
    paddingBlock: "4",
  }),
  date: css({ fontSize: "sm", fontWeight: "medium" }),
  note: css({
    fontSize: "sm",
    whiteSpace: "pre-wrap",
    overflowWrap: "anywhere",
    lineHeight: "1.65",
    margin: "0",
    marginTop: "2",
  }),
  actions: css({ display: "flex", gap: "1", alignSelf: "start" }),
  form: css({ display: "grid", gap: "5" }),
  row: css({
    display: "grid",
    gap: "4",
    gridTemplateColumns: "minmax(0,1fr) minmax(0,1fr)",
  }),
}

export interface CookingLogProps {
  entryID: string
  scale?: Rational
  onChange?: () => void
}

export const CookingLog = (props: CookingLogProps) => {
  const [history, { refetch, mutate }] = createResource(
    () => props.entryID,
    (id) => listCookings(id),
  )
  const [open, setOpen] = createSignal(false)
  const [editing, setEditing] = createSignal<CookingRecord>()
  const [date, setDate] = createSignal("")
  const [time, setTime] = createSignal("")
  const [note, setNote] = createSignal("")
  const { busy, error, setError, run } = createAsyncAction()
  let sessionID = ""
  const begin = (record?: CookingRecord) => {
    setEditing(record)
    setDate(record?.cooked_on ?? calendarDate())
    const now = new Date()
    setTime(
      record
        ? (record.cooked_time ?? "")
        : `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}`,
    )
    setNote(record?.note ?? "")
    setError(undefined)
    sessionID = uniqueID()
    setOpen(true)
  }
  // Each reads what it sends before starting, so later edits do not leak in.
  const save = async (event: SubmitEvent) => {
    event.preventDefault()
    if (busy()) return
    const entryID = props.entryID
    const onChange = props.onChange
    const staticRecord = editing()
    const scale = staticRecord
      ? staticRecord.scale
      : props.scale && compare(props.scale, rational(1n)) !== 0
        ? formatRational(props.scale)
        : undefined
    const staticInput = {
      cooked_on: date(),
      cooked_time: time() || undefined,
      note: note(),
      scale,
    }
    const session = sessionID
    await run(async () => {
      if (staticRecord) await replaceCooking(entryID, staticRecord.id, staticInput)
      else await recordCooking(entryID, { ...staticInput, session_id: session })
      mutate(await listCookings(entryID))
      setOpen(false)
      onChange?.()
    })
  }
  const remove = async (record: CookingRecord) => {
    if (busy()) return
    const entryID = props.entryID
    const onChange = props.onChange
    await run(async () => {
      await deleteCooking(entryID, record.id)
      mutate(await listCookings(entryID))
      onChange?.()
    })
  }
  const more = async () => {
    if (busy()) return
    const entryID = props.entryID
    const loaded = history.latest?.items ?? []
    await run(async () => {
      const next = await listCookings(entryID, 50, loaded.length)
      mutate({ ...next, items: [...loaded, ...(next.items ?? [])] })
    })
  }
  const recordedCount = () =>
    (history.latest?.times_cooked ?? 0) -
    (history.latest?.imported_baselines ?? []).reduce((n, item) => n + item.count, 0)
  return (
    <section class={styles.root} aria-label={m.cooking_log_cooking_history()}>
      <div class={styles.bar}>
        <h2 class={styles.title}>{m.cooking_log_cooking_history()}</h2>
        <Button size="sm" variant="outline" onClick={() => begin()}>
          {m.cooking_log_log_cook()}
        </Button>
      </div>
      <Show when={!open()}>
        <ChangeError error={error() ?? history.error} onReload={() => void refetch()} />
      </Show>
      <Show when={history.latest} fallback={<p class={styles.muted}>{m.cooking_log_loading()}</p>}>
        {(loaded) => (
          <>
            <p class={styles.muted}>
              {loaded().times_cooked
                ? m.cooking_times_cooked({ count: loaded().times_cooked })
                : m.cooking_log_not_cooked_yet()}
            </p>
            <ol class={styles.list} aria-label={m.cooking_log_previous_cooks()}>
              <For each={loaded().items}>
                {(record) => (
                  <li class={styles.entry}>
                    <div>
                      <time
                        class={styles.date}
                        dateTime={
                          record.cooked_on + (record.cooked_time ? `T${record.cooked_time}` : "")
                        }
                      >
                        {formatCookedOn(record.cooked_on)}
                        <Show when={record.cooked_time}>
                          {" "}
                          {m.common_dot_separator()} {record.cooked_time}
                        </Show>
                      </time>
                      <Show when={record.scale}>
                        <span class={styles.muted}>
                          {m.cooking_log_batch({ value1: record.scale ?? "" })}
                        </span>
                      </Show>
                      <Show when={record.note}>
                        <p class={styles.note}>{record.note}</p>
                      </Show>
                    </div>
                    <div class={styles.actions}>
                      <IconButton
                        size="sm"
                        variant="plain"
                        aria-label={m.cooking_log_edit_cook_on({
                          value1: formatCookedOn(record.cooked_on),
                        })}
                        disabled={busy()}
                        onClick={() => begin(record)}
                      >
                        <Pencil size={15} />
                      </IconButton>
                      <IconButton
                        size="sm"
                        variant="plain"
                        aria-label={m.cooking_log_forget_the_cooking_on({
                          value1: formatCookedOn(record.cooked_on),
                        })}
                        disabled={busy()}
                        onClick={() => void remove(record)}
                      >
                        <Trash2 size={15} />
                      </IconButton>
                    </div>
                  </li>
                )}
              </For>
            </ol>
            <Show when={(loaded().items?.length ?? 0) < recordedCount()}>
              <Button variant="plain" size="sm" loading={busy()} onClick={() => void more()}>
                {m.cooking_log_older_cooks()}
              </Button>
            </Show>
          </>
        )}
      </Show>
      <Show when={open()}>
        <Dialog
          open
          title={editing() ? m.cooking_log_edit_cook() : m.cooking_log_log_cook()}
          onOpenChange={(event) => !event.open && !busy() && setOpen(false)}
          closeOnEscape={!busy()}
          closeOnInteractOutside={!busy()}
          footer={
            <>
              <DialogClose>
                {(closeProps) => (
                  <Button {...closeProps()} variant="outline" disabled={busy()}>
                    {m.common_cancel()}
                  </Button>
                )}
              </DialogClose>
              <Button type="submit" form="cooking-log-form" colorPalette="accent" loading={busy()}>
                {m.cooking_log_save_cook()}
              </Button>
            </>
          }
        >
          <form id="cooking-log-form" class={styles.form} onSubmit={(event) => void save(event)}>
            <ChangeError error={error()} />
            <div class={styles.row}>
              <FormField label={m.cooking_log_date()}>
                <Input
                  type="date"
                  required
                  max={calendarDate()}
                  value={date()}
                  onInput={(event) => setDate(event.currentTarget.value)}
                />
              </FormField>
              <FormField label={m.cooking_log_time()}>
                <Input
                  type="time"
                  value={time()}
                  onInput={(event) => setTime(event.currentTarget.value)}
                />
              </FormField>
            </div>
            <FormField label={m.cooking_log_cooking_note_optional()}>
              <Textarea
                maxLength={10000}
                rows={5}
                value={note()}
                onInput={(event) => setNote(event.currentTarget.value)}
              />
            </FormField>
          </form>
        </Dialog>
      </Show>
    </section>
  )
}
function formatCookedOn(value: string): string {
  return formatDate(value)
}
