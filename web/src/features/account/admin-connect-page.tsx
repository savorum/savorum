import { createResource, createSignal, onCleanup, onMount, Show } from "solid-js"

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
    <section class={styles.section}>
      <ChangeError error={error() ?? status.error} />
      <p role="status">{stateLabel()}</p>
      <Show when={settled(status)?.remote_origin}>
        {(origin) => <a href={origin()}>{origin()}</a>}
      </Show>
      <Show when={settled(status)?.configured}>
        <p class={styles.muted}>{m.connect_disconnect_hint()}</p>
        <Button variant="outline" disabled={busy()} onClick={() => void disconnect()}>
          {m.connect_disconnect()}
        </Button>
      </Show>
      <form class={styles.form} onSubmit={(event) => void save(event)}>
        <p>
          <a href="https://cloud.savorum.app/connect" target="_blank" rel="noreferrer">
            {m.connect_get_setup()}
          </a>
        </p>
        <FormField label={m.connect_setup()} hint={m.connect_setup_hint()} required>
          <Textarea
            required
            rows={6}
            maxLength={8192}
            autocomplete="off"
            spellcheck={false}
            value={setup()}
            onInput={(event) => setSetup(event.currentTarget.value)}
          />
        </FormField>
        <Button type="submit" disabled={busy() || !setup().trim()}>
          {m.connect_save()}
        </Button>
      </form>
      <p class={styles.muted}>{m.connect_privacy()}</p>
    </section>
  )
}
