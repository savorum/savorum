import { Download, FileJson } from "lucide-solid"
import { For, Show, createMemo, createResource, createSignal, untrack } from "solid-js"
import { css } from "styled-system/css"

import { capabilities, loadCapabilities, supports } from "@/api/capabilities"
import { failure, ifMatch } from "@/api/client"
import type { components } from "@/api/schema"
import { sessionFetch } from "@/api/session"
import { ChangeError, Notice } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { CopyButton, CopyField } from "@/components/ui/clipboard"
import { ConfirmDialog } from "@/components/ui/dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField, ToggleField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { formatDateTime } from "@/lib/date-format"
import { latestUnlessFailed } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { AccountNav, ScopePage, accountStyles as styles } from "./account-layout"
import { account, authEnabled } from "./account-state"
import { AssistantPanel } from "./assistant-panel"

const developer = {
  stack: css({ display: "grid", gap: "6", paddingTop: "2" }),
  part: css({
    display: "grid",
    gap: "3",
    justifyItems: "start",
    minWidth: "0",
    "& > h3, & h3": { fontSize: "md", fontWeight: "semibold", lineHeight: "1.4" },
    "& > p": { maxWidth: "42rem" },
  }),
  heading: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    justifyContent: "space-between",
    width: "full",
  }),
  code: css({
    background: "bg.muted",
    borderColor: "border.subtle",
    borderRadius: "l2",
    borderWidth: "1px",
    fontFamily: "mono",
    fontSize: "sm",
    lineHeight: "1.6",
    margin: "0",
    maxWidth: "full",
    overflowX: "auto",
    padding: "4",
    whiteSpace: "pre",
    width: "full",
  }),
  formats: css({
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    listStyle: "none",
    margin: "0",
    padding: "0",
  }),
  format: css({
    background: "bg.muted",
    borderRadius: "l1",
    fontSize: "sm",
    paddingBlock: "1",
    paddingInline: "2.5",
  }),
}

type Subscription = components["schemas"]["Subscription"]
type Delivery = components["schemas"]["Delivery"]
type Detail = components["schemas"]["Detail"]
type Input = components["schemas"]["Input"]
type Draft = Input & { id: string; revision: number }

async function integrationRequest<T>(
  path: string,
  method = "GET",
  body?: unknown,
  revision?: number,
  libraryID?: string,
): Promise<T> {
  const response = await sessionFetch(`/api/v1${path}`, {
    method,
    headers: {
      ...(libraryID && !path.startsWith("/instance-webhooks")
        ? { "X-Savorum-Library": libraryID }
        : {}),
      ...(body ? { "Content-Type": "application/json" } : {}),
      ...(revision ? { "If-Match": ifMatch(revision) } : {}),
    },
    ...(body ? { body: JSON.stringify(body) } : {}),
  })
  if (!response.ok)
    throw failure(
      { response, error: await response.json().catch(() => undefined) },
      m.integrations_failed(),
    )
  return (response.status === 204 ? undefined : await response.json()) as T
}

export function IntegrationsPage() {
  const scope = createMemo(() =>
    JSON.stringify([account()?.user?.id, account()?.active_library_id]),
  )
  return (
    <Show when={scope()} keyed>
      {(owner) => <ScopedIntegrationsPage libraryID={(JSON.parse(owner) as string[])[1]!} />}
    </Show>
  )
}

function ScopedIntegrationsPage(props: { libraryID: string }) {
  const library = untrack(() => props.libraryID)
  const request = <T,>(path: string, method = "GET", body?: unknown, revision?: number) =>
    integrationRequest<T>(path, method, body, revision, library)
  const [available, { refetch: refreshCapabilities }] = createResource(() => loadCapabilities())
  const [instance, setInstance] = createSignal(false)
  const canManageLibrary = () =>
    account()?.libraries.find((value) => value.id === account()?.active_library_id)?.role ===
    "owner"
  const canManage = () => (instance() ? !!account()?.user?.instance_admin : canManageLibrary())
  const base = () => (instance() ? "/instance-webhooks" : "/webhooks")
  const source = createMemo(() =>
    supports("webhooks") && canManage() ? `${base()}|${account()?.active_library_id}` : false,
  )
  const [subscriptions, { refetch }] = createResource(source, () => request<Subscription[]>(base()))
  const [draft, setDraft] = createSignal<Draft>()
  const [secret, setSecret] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  const [notice, setNotice] = createSignal("")
  const [confirm, setConfirm] = createSignal<{ kind: "delete" | "rotate"; item: Subscription }>()
  const [historyID, setHistoryID] = createSignal("")
  const [offset, setOffset] = createSignal(0)
  const [history, { refetch: refreshHistory }] = createResource(
    () => (historyID() ? `${base()}|${historyID()}|${offset()}` : false),
    () => request<Delivery[]>(`${base()}/${historyID()}/deliveries?offset=${offset()}&limit=25`),
  )
  const [detail, setDetail] = createSignal<Detail>()
  const eventTypes = () =>
    (capabilities()?.event_types ?? []).filter(
      (type) =>
        type !== "webhook.test" &&
        instance() === (type.startsWith("user.") || type.startsWith("library.member.")),
    )
  const field = <K extends keyof Input>(key: K, value: Input[K]) =>
    setDraft((current) => (current ? { ...current, [key]: value } : current))
  async function handleAction(action: () => Promise<void>) {
    if (busy()) return
    setBusy(true)
    setError(undefined)
    setNotice("")
    try {
      await action()
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }
  async function save() {
    const value = draft()
    if (!value) return
    await handleAction(async () => {
      const { id, revision, ...input } = value
      if (id) await request(`${untrack(base)}/${id}`, "PUT", input, revision)
      else {
        const result = await request<{ secret: string }>(untrack(base), "POST", input)
        setSecret(result.secret)
      }
      setDraft(undefined)
      await refetch()
      setNotice(m.integrations_saved())
    })
  }
  async function confirmChange() {
    const value = confirm()
    if (!value) return
    await handleAction(async () => {
      if (value.kind === "delete")
        await request(`${untrack(base)}/${value.item.id}`, "DELETE", undefined, value.item.revision)
      else {
        const result = await request<{ secret: string }>(
          `${untrack(base)}/${value.item.id}/rotate-secret`,
          "POST",
          undefined,
          value.item.revision,
        )
        setSecret(result.secret)
      }
      setConfirm(undefined)
      setDetail(undefined)
      setHistoryID("")
      await refetch()
    })
  }
  const example = () =>
    `curl --fail-with-body "$SAVORUM_URL/api/v1/capabilities" \\\n  -H "Authorization: Bearer $SAVORUM_TOKEN"\n\ncurl --fail-with-body "$SAVORUM_URL/api/v1/import-jobs" \\\n  -H "Authorization: Bearer $SAVORUM_TOKEN" \\\n  -H "X-Savorum-Library: $SAVORUM_LIBRARY" \\\n  -H "Idempotency-Key: import-example-1" \\\n  -H "Content-Type: application/json" \\\n  --data '{"kind":"url","url":"https://example.com/recipe",\n    "policy":"preview-only","on_duplicate":"reject"}'`
  return (
    <ScopePage title={m.integrations_title()} description={m.integrations_lede()}>
      <AccountNav />
      <Show when={authEnabled()}>
        <AssistantPanel />
      </Show>
      <ChangeError error={error() ?? available.error ?? subscriptions.error ?? history.error} />
      <Notice message={notice()} />
      <section class={styles.section}>
        <h2>{m.integrations_webhooks()}</h2>
        <p class={styles.muted}>{m.integrations_scope_hint()}</p>
        <Show when={account()?.user?.instance_admin}>
          <FormField label={m.integrations_scope()}>
            <Select
              value={instance() ? "instance" : "library"}
              disabled={busy() || !!draft()}
              items={[
                { value: "library", label: m.integrations_library_scope() },
                { value: "instance", label: m.integrations_instance_scope() },
              ]}
              onValueChange={(value) => {
                setInstance(value === "instance")
                setHistoryID("")
                setDetail(undefined)
                setSecret("")
              }}
            />
          </FormField>
        </Show>
        <Show
          when={supports("webhooks")}
          fallback={<p class={styles.muted}>{m.integrations_unavailable()}</p>}
        >
          <Show
            when={canManage()}
            fallback={<p class={styles.muted}>{m.integrations_owner_required()}</p>}
          >
            <div class={styles.actions}>
              <Button
                variant="outline"
                disabled={busy()}
                onClick={() =>
                  void handleAction(async () => {
                    await refreshCapabilities()
                    await refetch()
                    if (untrack(historyID)) await refreshHistory()
                  })
                }
              >
                {m.account_retry()}
              </Button>
              <Button
                disabled={busy() || !!draft()}
                onClick={() =>
                  setDraft({
                    id: "",
                    revision: 0,
                    name: "",
                    url: "",
                    event_types: [],
                    enabled: false,
                  })
                }
              >
                {m.integrations_add()}
              </Button>
            </div>
            <Show when={secret()}>
              <section class={styles.card}>
                <h3>{m.integrations_secret()}</h3>
                <p>{m.integrations_secret_hint()}</p>
                <CopyField
                  label={m.integrations_secret()}
                  value={secret()}
                  copyLabel={m.account_copy()}
                  copiedLabel={m.account_copied()}
                />
                <Button variant="outline" onClick={() => setSecret("")}>
                  {m.account_tokens_dismiss()}
                </Button>
              </section>
            </Show>
            <Show when={!!draft()}>
              <form
                class={styles.card}
                onSubmit={(event) => {
                  event.preventDefault()
                  void save()
                }}
              >
                <FormField label={m.integrations_name()} required>
                  <Input
                    required
                    maxLength={100}
                    value={draft()?.name ?? ""}
                    onInput={(event) => field("name", event.currentTarget.value)}
                  />
                </FormField>
                <FormField
                  label={m.integrations_destination()}
                  required
                  hint={m.integrations_destination_hint()}
                >
                  <Input
                    required
                    type="url"
                    maxLength={2048}
                    value={draft()?.url ?? ""}
                    onInput={(event) => field("url", event.currentTarget.value)}
                  />
                </FormField>
                <fieldset>
                  <legend>{m.integrations_events()}</legend>
                  <For each={eventTypes()}>
                    {(type) => (
                      <ToggleField
                        label={type}
                        checked={draft()?.event_types?.includes(type) ?? false}
                        onChange={(checked) =>
                          field(
                            "event_types",
                            checked
                              ? [...(draft()?.event_types ?? []), type]
                              : (draft()?.event_types ?? []).filter((value) => value !== type),
                          )
                        }
                      />
                    )}
                  </For>
                </fieldset>
                <ToggleField
                  label={m.integrations_enabled()}
                  checked={draft()?.enabled ?? false}
                  onChange={(checked) => field("enabled", checked)}
                />
                <p class={styles.muted}>{m.integrations_enable_hint()}</p>
                <div class={styles.actions}>
                  <Button type="submit" disabled={busy() || !draft()?.event_types?.length}>
                    {m.pantry_save()}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={busy()}
                    onClick={() => setDraft(undefined)}
                  >
                    {m.common_cancel()}
                  </Button>
                </div>
              </form>
            </Show>
            <For each={(latestUnlessFailed(subscriptions) ?? []).map((value) => value.id)}>
              {(id) => {
                const item = () =>
                  (latestUnlessFailed(subscriptions) ?? []).find((value) => value.id === id)!
                return (
                  <section class={styles.card}>
                    <h3>{item().name}</h3>
                    <p>{item().url}</p>
                    <p>
                      {item().enabled ? m.integrations_active() : m.integrations_paused()}
                      {item().pause_reason
                        ? ` · ${item().pause_reason === "repeated-failures" ? m.integrations_repeated_failures() : m.integrations_queue_full()}`
                        : ""}
                    </p>
                    <Show when={item().consecutive_failures > 0}>
                      <p role="status">
                        {m.integrations_failing({ count: item().consecutive_failures })}
                      </p>
                    </Show>
                    <p class={styles.muted}>{(item().event_types ?? []).join(", ")}</p>
                    <div class={styles.actions}>
                      <Button
                        variant="outline"
                        disabled={busy() || !!draft()}
                        onClick={() =>
                          setDraft({
                            id,
                            revision: item().revision,
                            name: item().name,
                            url: item().url,
                            event_types: [...(item().event_types ?? [])],
                            enabled: item().enabled,
                          })
                        }
                      >
                        {m.pantry_edit()}
                      </Button>
                      <Button
                        variant="outline"
                        disabled={busy() || !item().enabled}
                        onClick={() =>
                          void handleAction(async () => {
                            await request(`${untrack(base)}/${id}/test`, "POST")
                            setHistoryID(id)
                            setOffset(0)
                            await refreshHistory()
                            setNotice(m.integrations_test_queued())
                          })
                        }
                      >
                        {m.integrations_test()}
                      </Button>
                      <Button
                        variant="plain"
                        onClick={() => {
                          setHistoryID(id)
                          setOffset(0)
                          setDetail(undefined)
                        }}
                      >
                        {m.integrations_history()}
                      </Button>
                      <Button
                        variant="plain"
                        disabled={busy()}
                        onClick={() => setConfirm({ kind: "rotate", item: item() })}
                      >
                        {m.integrations_rotate()}
                      </Button>
                      <Button
                        variant="plain"
                        disabled={busy()}
                        onClick={() => setConfirm({ kind: "delete", item: item() })}
                      >
                        {m.pantry_remove()}
                      </Button>
                    </div>
                  </section>
                )
              }}
            </For>
            <Show when={!subscriptions.loading && latestUnlessFailed(subscriptions)?.length === 0}>
              <p class={styles.muted}>{m.integrations_empty()}</p>
            </Show>
          </Show>
        </Show>
      </section>
      <Show when={historyID()}>
        <section class={styles.section}>
          <h2>{m.integrations_history()}</h2>
          <p class={styles.muted}>{m.integrations_history_hint()}</p>
          <Button
            variant="outline"
            disabled={busy()}
            onClick={() =>
              void handleAction(async () => {
                await refreshHistory()
                await refetch()
              })
            }
          >
            {m.account_retry()}
          </Button>
          <For each={latestUnlessFailed(history) ?? []}>
            {(delivery) => (
              <div class={styles.card}>
                <p>
                  {delivery.event_type} · {delivery.state} · {delivery.attempts}
                </p>
                <p class={styles.muted}>{formatDateTime(delivery.created_at)}</p>
                <Show when={delivery.next_attempt_at}>
                  <p>
                    {m.integrations_next_attempt({
                      time: formatDateTime(delivery.next_attempt_at!),
                    })}
                  </p>
                </Show>
                <Show when={delivery.last_code}>
                  <p>{deliveryMessage(delivery.last_code!)}</p>
                </Show>
                <div class={styles.actions}>
                  <Button
                    variant="plain"
                    onClick={() =>
                      void handleAction(async () => {
                        setDetail(
                          await request<Detail>(
                            `${untrack(base)}/${untrack(historyID)}/deliveries/${delivery.id}`,
                          ),
                        )
                      })
                    }
                  >
                    {m.integrations_attempts()}
                  </Button>
                  <Show when={delivery.state === "failed" || delivery.state === "delivered"}>
                    <Button
                      variant="outline"
                      disabled={busy()}
                      onClick={() =>
                        void handleAction(async () => {
                          await request(
                            `${untrack(base)}/${untrack(historyID)}/deliveries/${delivery.id}/redeliver`,
                            "POST",
                          )
                          await refreshHistory()
                          setNotice(m.integrations_redelivery_queued())
                        })
                      }
                    >
                      {m.integrations_redeliver()}
                    </Button>
                  </Show>
                </div>
              </div>
            )}
          </For>
          <div class={styles.actions}>
            <Button
              variant="outline"
              disabled={offset() === 0}
              onClick={() => setOffset(Math.max(0, offset() - 25))}
            >
              {m.integrations_previous()}
            </Button>
            <Button
              variant="outline"
              disabled={latestUnlessFailed(history)?.length !== 25 || offset() >= 975}
              onClick={() => setOffset(offset() + 25)}
            >
              {m.integrations_next()}
            </Button>
          </div>
          <Show when={detail()}>
            {(value) => (
              <div class={styles.card}>
                <h3>{m.integrations_attempts()}</h3>
                <For each={value().attempts}>
                  {(attempt) => (
                    <div>
                      <p>
                        {formatDateTime(attempt.at)} · {deliveryMessage(attempt.code)} ·{" "}
                        {attempt.status || ""} ·{" "}
                        {m.integrations_duration_ms({ duration: attempt.duration_ms })}
                      </p>
                      <Show when={attempt.excerpt}>
                        <pre class={styles.codes}>{attempt.excerpt}</pre>
                      </Show>
                    </div>
                  )}
                </For>
              </div>
            )}
          </Show>
        </section>
      </Show>
      <Disclosure label={m.integrations_developer()}>
        <div class={developer.stack}>
          <section class={developer.part}>
            <h3>{m.integrations_api_heading()}</h3>
            <p class={styles.muted}>{m.integrations_api_hint()}</p>
            <a class={buttonLink({ variant: "outline", size: "sm" })} href="/openapi.json">
              <FileJson aria-hidden="true" size={16} />
              {m.integrations_contract()}
            </a>
          </section>
          <section class={developer.part}>
            <div class={developer.heading}>
              <h3>{m.integrations_example_heading()}</h3>
              <CopyButton
                value={example()}
                copyLabel={m.account_copy()}
                copiedLabel={m.account_copied()}
                size="sm"
              />
            </div>
            <p class={styles.muted}>{m.integrations_example_hint({ id: "{id}" })}</p>
            <pre class={developer.code}>{example()}</pre>
          </section>
          <section class={developer.part}>
            <h3>{m.integrations_receiver_heading()}</h3>
            <p class={styles.muted}>{m.integrations_signing_hint()}</p>
            <a
              class={buttonLink({ variant: "outline", size: "sm" })}
              href="/examples/webhook_receiver.py"
              download="webhook_receiver.py"
            >
              <Download aria-hidden="true" size={16} />
              {m.integrations_receiver_example()}
            </a>
          </section>
          <Show when={capabilities()?.importers?.length}>
            <section class={developer.part}>
              <h3>{m.integrations_formats()}</h3>
              <ul class={developer.formats}>
                <For each={capabilities()?.importers ?? []}>
                  {(importer) => <li class={developer.format}>{importer.name}</li>}
                </For>
              </ul>
            </section>
          </Show>
        </div>
      </Disclosure>
      <Show when={!!confirm()}>
        <ConfirmDialog
          cancelLabel={m.common_cancel()}
          onClose={() => setConfirm(undefined)}
          title={confirm()?.kind === "rotate" ? m.integrations_rotate() : m.integrations_delete()}
          description={
            confirm()?.kind === "rotate"
              ? m.integrations_rotate_hint()
              : m.integrations_delete_hint()
          }
          confirmLabel={confirm()?.kind === "rotate" ? m.integrations_rotate() : m.pantry_remove()}
          onConfirm={() => void confirmChange()}
          busy={busy()}
        />
      </Show>
    </ScopePage>
  )
}

function deliveryMessage(code: string): string {
  const messages: Record<string, () => string> = {
    delivered: m.integrations_delivery_delivered,
    "network-error": m.integrations_delivery_network,
    "retry-window-expired": m.integrations_delivery_expired,
    "signing-key-unavailable": m.integrations_delivery_key,
    "destination-rejected": m.integrations_delivery_destination,
    "receiver-rejected": m.integrations_delivery_receiver,
    "redirect-refused": m.integrations_delivery_redirect,
    "subscription-deleted": m.integrations_delivery_deleted,
    "queue-capacity": m.integrations_delivery_capacity,
  }
  return messages[code]?.() ?? code
}
