import { ArrowUpRight, Plug } from "lucide-solid"
import { createResource, createSignal, onCleanup, onMount, Show } from "solid-js"
import { css } from "styled-system/css"

import { SkeletonRows } from "@/components/skeleton"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Textarea } from "@/components/ui/textarea"
import { m } from "@/lib/messages"
import { settled } from "@/lib/settled"

import { accountStyles as styles } from "./account-layout"
import { accountRequest, administers } from "./account-state"
import { AdminPage } from "./admin-page"

type ConnectStatus = {
  configured: boolean
  state: string
  remote_origin?: string
  certificate_ready: boolean
}

export function AdminConnectPage() {
  return (
    <AdminPage title={m.connect_title()} lede={m.connect_lede()}>
      <ConnectSettings />
    </AdminPage>
  )
}

function ConnectSettings() {
  const [status, { refetch }] = createResource(administers, () =>
    accountRequest<ConnectStatus>("/instance/connect"),
  )
  const [setup, setSetup] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  onMount(() => {
    const timer = setInterval(() => {
      if (administers() && !busy()) void refetch()
    }, 10000)
    onCleanup(() => clearInterval(timer))
  })
  const stateLabel = () => {
    switch (settled(status)?.state) {
      case "connected":
        return settled(status)?.certificate_ready
          ? m.connect_connected()
          : m.connect_certificate_pending()
      case "connecting":
        return m.connect_connecting()
      case "reconnecting":
        return m.connect_reconnecting()
      case "unavailable":
        return m.connect_unavailable()
      case "authentication_required":
        return m.connect_authentication_required()
      case "invalid_setup":
        return m.connect_invalid_setup()
      case "storage_error":
        return m.connect_storage_error()
      default:
        return m.connect_disabled()
    }
  }
  async function save(event: SubmitEvent) {
    event.preventDefault()
    setBusy(true)
    setError(undefined)
    try {
      let document: unknown
      try {
        document = JSON.parse(setup())
      } catch {
        throw new Error(m.connect_invalid_setup())
      }
      await accountRequest("/instance/connect", "PUT", document)
      setSetup("")
      await refetch()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  async function disconnect() {
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest("/instance/connect", "DELETE")
      await refetch()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div class={css({ display: "grid", gap: "6" })}>
      <ChangeError error={error() ?? status.error} />
      <Show when={status.error}>
        <div>
          <Button variant="outline" onClick={() => void refetch()}>
            {m.account_retry()}
          </Button>
        </div>
      </Show>
      <Show when={status.loading && !settled(status)}>
        <SkeletonRows count={1} />
      </Show>
      <Show when={settled(status)}>
        <section class={css({ display: "flex", flexWrap: "wrap", alignItems: "start", gap: "4" })}>
          <Plug aria-hidden="true" size={20} class={css({ color: "fg.muted", marginTop: "1" })} />
          <div class={css({ display: "grid", gap: "2", flex: "1 1 16rem", minWidth: "0" })}>
            <h2 class={css({ fontSize: "md", fontWeight: "medium" })}>
              <span role="status">{stateLabel()}</span>
            </h2>
            <Show when={settled(status)?.remote_origin}>
              {(origin) => (
                <a class={styles.back} href={origin()}>
                  {origin()}
                </a>
              )}
            </Show>
            <Show when={settled(status)?.configured}>
              <p class={styles.muted}>{m.connect_disconnect_hint()}</p>
            </Show>
          </div>
          <Show when={settled(status)?.configured}>
            <Button
              size="sm"
              variant="outline"
              colorPalette="red"
              disabled={busy()}
              onClick={() => void disconnect()}
            >
              {m.connect_disconnect()}
            </Button>
          </Show>
        </section>
      </Show>
      <form
        class={css({
          display: "grid",
          gap: "4",
          borderTopWidth: "1px",
          borderColor: "border.subtle",
          paddingTop: "6",
        })}
        onSubmit={(event) => void save(event)}
      >
        <a
          class={styles.back}
          href="https://cloud.savorum.app/connect"
          target="_blank"
          rel="noreferrer"
        >
          {m.connect_get_setup()}{" "}
          <ArrowUpRight aria-hidden="true" size={16} class={css({ marginInlineStart: "1" })} />
        </a>
        <FormField label={m.connect_setup()} hint={m.connect_setup_hint()} required>
          <Textarea
            required
            rows={4}
            maxLength={8192}
            autocomplete="off"
            spellcheck={false}
            class={css({
              fontFamily: "mono",
              fontSize: "sm",
              lineHeight: "1.6",
              resize: "vertical",
            })}
            value={setup()}
            onInput={(event) => setSetup(event.currentTarget.value)}
          />
        </FormField>
        <div>
          <Button type="submit" loading={busy()} disabled={busy() || !setup().trim()}>
            {m.connect_save()}
          </Button>
        </div>
      </form>
      <p
        class={css({
          color: "fg.muted",
          fontSize: "sm",
          borderTopWidth: "1px",
          borderColor: "border.subtle",
          paddingTop: "4",
        })}
      >
        {m.connect_privacy()}
      </p>
    </div>
  )
}
