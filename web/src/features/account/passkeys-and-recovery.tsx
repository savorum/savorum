import {
  For,
  Show,
  createMemo,
  createResource,
  createSignal,
  onCleanup,
  onMount,
  untrack,
} from "solid-js"
import { css } from "styled-system/css"

import { Callout, ChangeError, Notice, Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/clipboard"
import { ConfirmDialog } from "@/components/ui/dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { createAsyncAction } from "@/lib/create-async-action"
import { formatDate } from "@/lib/date-format"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { accountRequest } from "./account-state"
import { passkeysSupported, registerPasskey, signInWithPasskey } from "./passkeys"

type Passkey = {
  rp_id?: string
  id: string
  name: string
  created_at: string
  last_used_at: string | null
}
type Run = (action: () => Promise<void>, message: string) => Promise<void>

function useTask() {
  const { busy, error, setError, notice, setNotice, run } = createAsyncAction()
  return { busy, error, setError, notice, setNotice, run: run as Run }
}

function TaskMessages(props: { error: unknown; notice: string }) {
  return (
    <>
      <ChangeError error={props.error} />
      <Notice message={props.notice} />
    </>
  )
}

export function PasskeysPanel() {
  const [keys, { refetch }] = createResource(async () =>
    accountRequest<{ passkeys: Passkey[]; available: boolean }>("/account/passkeys"),
  )
  const passkeys = createMemo<Passkey[]>((previous) => settled(keys)?.passkeys ?? previous, [])
  const [name, setName] = createSignal("")
  const task = useTask()
  async function reload() {
    await refetch()
  }
  return (
    <section class={styles.section}>
      <h2>{m.account_passkeys_heading()}</h2>
      <p class={styles.muted}>{m.account_passkeys_hint()}</p>
      <TaskMessages error={task.error()} notice={task.notice()} />
      <Show when={keys.error}>
        <Callout
          tone="danger"
          actions={
            <Button variant="outline" onClick={() => void refetch()}>
              {m.account_retry()}
            </Button>
          }
        >
          {m.account_load_error()}
        </Callout>
      </Show>
      <ul class={styles.rows}>
        <For each={passkeys().map((key) => key.id)}>
          {(id) => {
            const initial = passkeys().find((key) => key.id === id)!
            return (
              <PasskeyRow
                passkey={passkeys().find((key) => key.id === id) ?? initial}
                busy={task.busy()}
                error={task.error()}
                onStart={() => task.setError(undefined)}
                run={task.run}
                onReload={reload}
              />
            )
          }}
        </For>
      </ul>
      <Show
        when={settled(keys)?.available && passkeysSupported()}
        fallback={
          <Show when={settled(keys)}>
            <p class={styles.muted}>{m.account_passkeys_unavailable()}</p>
          </Show>
        }
      >
        <form
          class={css({
            display: "grid",
            gap: "3",
            gridTemplateColumns: { base: "1fr", sm: "minmax(0, 1fr) auto" },
            alignItems: "end",
            "& > button": { justifySelf: "start" },
          })}
          onSubmit={(event) => {
            event.preventDefault()
            const staticName = name()
            void task.run(async () => {
              await registerPasskey(staticName)
              setName("")
              await reload()
            }, m.account_passkeys())
          }}
        >
          <FormField label={m.account_passkeys_name()} required>
            <Input
              required
              maxLength={100}
              placeholder={m.account_passkeys_name_placeholder()}
              value={name()}
              onInput={(event) => setName(event.currentTarget.value)}
              autocomplete="off"
            />
          </FormField>
          <Button type="submit" disabled={task.busy() || !name().trim()}>
            {m.account_passkeys_add()}
          </Button>
        </form>
      </Show>
    </section>
  )
}

function PasskeyRow(props: {
  passkey: Passkey
  busy: boolean
  error: unknown
  onStart: () => void
  run: Run
  onReload: () => Promise<void>
}) {
  const [name, setName] = createSignal(untrack(() => props.passkey.name))
  const [editing, setEditing] = createSignal(false)
  const [confirm, setConfirm] = createSignal(false)
  let renameButton: HTMLButtonElement | undefined
  let input: HTMLInputElement | undefined
  const cancelRename = () => {
    setEditing(false)
    queueMicrotask(() => renameButton?.focus())
  }
  return (
    <li class={styles.listRow}>
      <div class={styles.rowBody}>
        <Show when={editing()} fallback={<h3>{props.passkey.name}</h3>}>
          <form
            class={css({ display: "flex", flexWrap: "wrap", gap: "2", alignItems: "end" })}
            onSubmit={(event) => {
              event.preventDefault()
              const id = props.passkey.id
              const nextName = name()
              const reload = props.onReload
              void props
                .run(async () => {
                  await accountRequest(`/account/passkeys/${encodeURIComponent(id)}`, "PUT", {
                    name: nextName,
                  })
                  setEditing(false)
                  await reload()
                }, m.account_passkeys_renamed())
                .then(() => {
                  if (editing()) input?.focus()
                  else renameButton?.focus()
                })
            }}
          >
            <FormField label={m.account_passkeys_name()} required>
              <Input
                ref={(element) => {
                  input = element
                }}
                aria-label={m.account_passkeys_name_for({ name: props.passkey.name })}
                value={name()}
                onInput={(event) => setName(event.currentTarget.value)}
                onKeyDown={(event) => {
                  if (event.key === "Escape" && !props.busy) {
                    event.preventDefault()
                    cancelRename()
                  }
                }}
                required
                maxLength={100}
                autocomplete="off"
                disabled={props.busy}
              />
            </FormField>
            <Button
              size="sm"
              type="submit"
              disabled={props.busy || !name().trim() || name() === props.passkey.name}
            >
              {m.account_passkeys_save_name()}
            </Button>
            <Button size="sm" variant="plain" disabled={props.busy} onClick={cancelRename}>
              {m.common_cancel()}
            </Button>
          </form>
        </Show>
        <p class={styles.muted}>
          {props.passkey.last_used_at
            ? m.account_passkeys_last_used({ date: formatDate(props.passkey.last_used_at) })
            : m.account_passkeys_never_used()}
        </p>
        <Show when={props.passkey.rp_id}>
          <p class={styles.muted}>{props.passkey.rp_id}</p>
        </Show>
      </div>
      <div class={styles.actions}>
        <Show when={!editing()}>
          <Button
            ref={(element) => {
              renameButton = element
            }}
            size="sm"
            variant="outline"
            disabled={props.busy}
            aria-label={m.account_passkeys_rename_named({ name: props.passkey.name })}
            onClick={() => {
              props.onStart()
              setName(props.passkey.name)
              setEditing(true)
              queueMicrotask(() => {
                input?.focus()
                input?.select()
              })
            }}
          >
            {m.account_passkeys_rename()}
          </Button>
        </Show>
        <Button
          size="sm"
          variant="plain"
          disabled={props.busy}
          aria-label={m.account_passkeys_remove_named({ name: props.passkey.name })}
          onClick={() => {
            props.onStart()
            setConfirm(true)
          }}
        >
          {m.account_passkeys_remove()}
        </Button>
      </div>
      <Show when={confirm()}>
        <ConfirmDialog
          title={m.account_passkeys_remove_named({ name: props.passkey.name })}
          description={m.account_passkeys_remove_confirm()}
          confirmLabel={m.account_confirm_removal()}
          cancelLabel={m.common_cancel()}
          busy={props.busy}
          onClose={() => setConfirm(false)}
          onConfirm={() => {
            const id = props.passkey.id
            const reload = props.onReload
            void props.run(async () => {
              await accountRequest(`/account/passkeys/${encodeURIComponent(id)}`, "DELETE")
              setConfirm(false)
              await reload()
            }, m.account_passkeys_removed())
          }}
        >
          <ChangeError error={props.error} />
        </ConfirmDialog>
      </Show>
    </li>
  )
}

export function RecoveryCodesPanel() {
  const [recovery, { refetch }] = createResource(async () =>
    accountRequest<{ remaining: number; allowed: boolean }>("/account/recovery"),
  )
  const [codes, setCodes] = createSignal<string[]>([])
  const [replace, setReplace] = createSignal(false)
  const task = useTask()
  onCleanup(() => setCodes([]))
  async function generate() {
    setCodes((await accountRequest<{ codes: string[] }>("/account/recovery", "POST", {})).codes)
    setReplace(false)
    await refetch()
  }
  return (
    <section class={styles.section}>
      <h2>{m.account_recovery_heading()}</h2>
      <TaskMessages error={task.error()} notice={task.notice()} />
      <Show when={recovery.error}>
        <Callout tone="danger">{m.account_load_error()}</Callout>
      </Show>
      <Show
        when={settled(recovery)?.allowed}
        fallback={
          <Show when={settled(recovery)}>
            <p class={styles.muted}>{m.account_recovery_disabled()}</p>
          </Show>
        }
      >
        <p class={styles.muted}>{m.account_recovery_hint()}</p>
        <p>
          <Show
            when={(settled(recovery)?.remaining ?? 0) > 0}
            fallback={m.account_recovery_empty()}
          >
            {m.account_recovery_remaining({ count: settled(recovery)?.remaining ?? 0 })}
          </Show>
        </p>
        <Show when={codes().length === 0}>
          <Show
            when={!replace()}
            fallback={
              <div class={styles.card}>
                <p>{m.account_recovery_replace_warning()}</p>
                <div class={styles.actions}>
                  <Button disabled={task.busy()} onClick={() => void task.run(generate, "")}>
                    {m.account_recovery_replace_confirm()}
                  </Button>
                  <Button
                    variant="outline"
                    disabled={task.busy()}
                    onClick={() => setReplace(false)}
                  >
                    {m.common_cancel()}
                  </Button>
                </div>
              </div>
            }
          >
            <div class={styles.actions}>
              <Button
                disabled={task.busy()}
                onClick={() => {
                  if ((settled(recovery)?.remaining ?? 0) > 0) setReplace(true)
                  else void task.run(generate, "")
                }}
              >
                {(settled(recovery)?.remaining ?? 0) > 0
                  ? m.account_recovery_replace()
                  : m.account_recovery_generate()}
              </Button>
            </div>
          </Show>
        </Show>
        <Show when={codes().length > 0}>
          <div class={styles.card}>
            <h3>{m.account_recovery_saved_heading()}</h3>
            <p class={styles.muted}>{m.account_recovery_saved_hint()}</p>
            <pre class={styles.codes} aria-label={m.account_recovery_codes_label()}>
              {codes().join("\n")}
            </pre>
            <div class={styles.actions}>
              <CopyButton
                value={codes().join("\n")}
                copyLabel={m.account_copy()}
                copiedLabel={m.account_copied()}
              />
              <Button
                onClick={() => {
                  setCodes([])
                  task.setNotice(m.account_recovery_hidden())
                }}
              >
                {m.account_recovery_dismiss()}
              </Button>
            </div>
          </div>
        </Show>
      </Show>
    </section>
  )
}

// PasskeyLogin shares the existing login screen; conditional mediation uses its username field.
export function PasskeyLogin(props: { available: boolean; onSignedIn?: () => Promise<void> }) {
  const [busy, setBusy] = createSignal(false)
  const [error, setError] = createSignal("")
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  onMount(() => {
    if (
      props.available &&
      passkeysSupported() &&
      PublicKeyCredential.isConditionalMediationAvailable
    )
      void PublicKeyCredential.isConditionalMediationAvailable()
        .then(async (supported) => {
          if (!supported || controller.signal.aborted) return
          await signInWithPasskey(true, controller.signal)
          if (controller.signal.aborted) return
          if (props.onSignedIn)
            await props.onSignedIn().catch((failure: Error) => setError(failure.message))
          else window.location.reload()
        })
        .catch(() => {})
  })
  return (
    <Show when={props.available && passkeysSupported()}>
      <div class={styles.actions}>
        <Button
          disabled={busy()}
          onClick={() => {
            controller.abort()
            setBusy(true)
            setError("")
            void signInWithPasskey()
              .then(() => (props.onSignedIn ? props.onSignedIn() : window.location.reload()))
              .catch((failure: Error) =>
                setError(
                  failure.name === "NotAllowedError"
                    ? m.account_login_passkey_cancelled()
                    : failure.message,
                ),
              )
              .finally(() => setBusy(false))
          }}
        >
          {m.account_login_passkey()}
        </Button>
        <Problem message={error()} />
      </div>
    </Show>
  )
}

export function RecoveryLogin(props: { onSignedIn?: () => Promise<void> } = {}) {
  const [login, setLogin] = createSignal("")
  const [code, setCode] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const [error, setError] = createSignal("")
  return (
    <Disclosure label={m.account_login_recovery_summary()}>
      <form
        class={styles.form}
        onSubmit={(event) => {
          event.preventDefault()
          setBusy(true)
          setError("")
          void accountRequest("/account/recovery/login", "POST", { login: login(), code: code() })
            .then(
              () => (props.onSignedIn ? props.onSignedIn() : window.location.assign("/account")),
              () => setError(m.account_login_recovery_error()),
            )
            .catch((failure: Error) => setError(failure.message))
            .finally(() => setBusy(false))
        }}
      >
        <p>{m.account_login_recovery_hint()}</p>
        <FormField label={m.account_login_recovery_login()} required>
          <Input
            required
            maxLength={128}
            autocomplete="username"
            value={login()}
            onInput={(event) => setLogin(event.currentTarget.value)}
          />
        </FormField>
        <FormField label={m.account_login_recovery_code()} required>
          <Input
            required
            maxLength={100}
            autocomplete="off"
            spellcheck={false}
            value={code()}
            onInput={(event) => setCode(event.currentTarget.value)}
          />
        </FormField>
        <Problem message={error()} />
        <Button type="submit" disabled={busy()}>
          {m.account_login_recovery_submit()}
        </Button>
      </form>
    </Disclosure>
  )
}
