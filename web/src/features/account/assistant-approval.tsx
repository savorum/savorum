import { Show, createResource, createSignal } from "solid-js"

import { ApiError } from "@/api/client"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { FormField, ToggleField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { formatNumber } from "@/lib/locale-format"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { account, accountRequest, libraries } from "./account-state"
import { Login } from "./login-page"

type Approval = {
  reauth_required?: boolean
  client_origin?: string
  client_name: string
  scope: string
  user_code: string
  remember_days: number
  verification_required: boolean
}
export function AssistantApproval() {
  const initialCode = new URLSearchParams(window.location.search).get("code") ?? ""
  const [code, setCode] = createSignal(initialCode)
  const [lookup, setLookup] = createSignal(initialCode)
  const [request, { refetch }] = createResource(lookup, (value) =>
    accountRequest<Approval>(`/account/authorization?code=${encodeURIComponent(value)}`),
  )
  const [libraryID, setLibraryID] = createSignal(
    account()?.active_library_id ?? libraries()[0]?.id ?? "",
  )
  const [permission, setPermission] = createSignal("read")
  const [remember, setRemember] = createSignal(false)
  const [busy, setBusy] = createSignal(false)
  const [done, setDone] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  const library = () => libraries().find((l) => l.id === libraryID())
  const canWrite = () =>
    library()?.role !== "viewer" && settled(request)?.scope.split(" ").includes("write")
  const access = () => (canWrite() && permission() === "write" ? "write" : "read")
  async function approve(deny = false) {
    if (busy()) return
    setBusy(true)
    setError(undefined)
    try {
      const result = await accountRequest<{ redirect_uri: string }>(
        "/account/authorization",
        "POST",
        { code: lookup(), library_id: libraryID(), access: access(), remember: remember(), deny },
      )
      if (result.redirect_uri) {
        window.location.assign(result.redirect_uri)
        return
      }
      setDone(true)
    } catch (failure) {
      if (failure instanceof ApiError && failure.kind === "recent-authentication-required")
        await refetch()
      else setError(failure)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Show when={!settled(request)?.reauth_required} fallback={<Login />}>
      <main class={styles.page}>
        <h1 class={styles.heading}>{m.account_assistant_heading()}</h1>
        <ChangeError error={error()} />
        <Show when={!done()} fallback={<p role="status">{m.account_assistant_done()}</p>}>
          <Show
            when={!request.error}
            fallback={
              <>
                <p role="alert">{m.account_assistant_request_unavailable()}</p>
                <Button variant="outline" onClick={() => void refetch()}>
                  {m.account_retry()}
                </Button>
              </>
            }
          >
            <Show
              when={settled(request)}
              fallback={
                <form
                  class={styles.form}
                  onSubmit={(event) => {
                    event.preventDefault()
                    setLookup(code())
                  }}
                >
                  <FormField label={m.account_assistant_code()}>
                    <Input
                      required
                      value={code()}
                      onInput={(event) => setCode(event.currentTarget.value)}
                    />
                  </FormField>
                  <Button type="submit">{m.account_assistant_continue()}</Button>
                </form>
              }
            >
              {(loaded) => (
                <form
                  class={styles.form}
                  onSubmit={(event) => {
                    event.preventDefault()
                    void approve()
                  }}
                >
                  <p>{m.account_assistant_request_from({ name: loaded().client_name })}</p>
                  <Show when={loaded().client_origin}>
                    <p class={styles.muted}>{loaded().client_origin}</p>
                  </Show>
                  <Show when={loaded().verification_required}>
                    <p>{m.account_assistant_check_code({ code: loaded().user_code })}</p>
                  </Show>
                  <FormField
                    label={m.account_admin_mapping_library()}
                    hint={libraries().length ? undefined : m.account_overview_libraries_empty()}
                  >
                    <Select
                      value={libraryID()}
                      disabled={busy()}
                      items={libraries().map((l) => ({ value: l.id, label: l.name }))}
                      onValueChange={(id) => {
                        setLibraryID(id)
                        if (!canWrite()) setPermission("read")
                      }}
                    />
                  </FormField>
                  <FormField
                    label={m.account_tokens_permissions()}
                    hint={
                      access() === "write"
                        ? m.account_assistant_access_write_hint()
                        : m.account_assistant_access_read_hint()
                    }
                  >
                    <Select
                      value={access()}
                      disabled={busy()}
                      onValueChange={setPermission}
                      items={[
                        { value: "read", label: m.account_assistant_access_read() },
                        {
                          value: "write",
                          label: m.account_assistant_access_write(),
                          disabled: !canWrite(),
                        },
                      ]}
                    />
                  </FormField>
                  <ToggleField
                    label={m.account_assistant_remember()}
                    checked={remember()}
                    disabled={busy()}
                    onChange={setRemember}
                  />
                  <p class={styles.muted}>
                    {remember()
                      ? m.account_tokens_expiry_days({ days: formatNumber(loaded().remember_days) })
                      : m.account_assistant_temporary()}
                  </p>
                  <div class={styles.actions}>
                    <Button type="submit" disabled={busy() || !library()}>
                      {m.account_assistant_allow()}
                    </Button>
                    <Button variant="outline" disabled={busy()} onClick={() => void approve(true)}>
                      {m.common_cancel()}
                    </Button>
                  </div>
                </form>
              )}
            </Show>
          </Show>
        </Show>
      </main>
    </Show>
  )
}
