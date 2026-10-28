import { For, Show, createResource, createSignal, onMount } from "solid-js"

import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { CopyField } from "@/components/ui/clipboard"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import {
  account,
  accountRequest,
  refreshAccount,
  roleName,
  type AccountLibrary,
} from "./account-state"
import { formatEpoch } from "./formats"

type Invitation = {
  id: string
  role: string
  hint: string
  expires_at: number
  accepted_at?: number
  revoked_at?: number
}

// The fragment stays out of server request logs and is removed as soon as it is read.
export function invitationToken() {
  const location = new URL(window.location.href)
  if (location.searchParams.get("invitation") === "accepted") {
    sessionStorage.removeItem("savorum.invitation")
    location.searchParams.delete("invitation")
    history.replaceState(null, "", location.pathname + location.search + location.hash)
    return ""
  }
  const token = new URLSearchParams(window.location.hash.slice(1)).get("invitation")
  if (token) {
    sessionStorage.setItem("savorum.invitation", token)
    history.replaceState(null, "", window.location.pathname + window.location.search)
  }
  return token || sessionStorage.getItem("savorum.invitation") || ""
}

export function InvitationAcceptance() {
  const [token, setToken] = createSignal("")
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  onMount(() => setToken(invitationToken()))
  async function accept() {
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest("/account/invitations/accept", "POST", { token: token() })
      sessionStorage.removeItem("savorum.invitation")
      setToken("")
      await refreshAccount()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Show when={token() && account()?.user}>
      <section class={styles.section} aria-label={m.account_invitation_heading()}>
        <h2>{m.account_invitation_heading()}</h2>
        <p>{m.account_invitation_join_as({ name: account()?.user?.display_name ?? "" })}</p>
        <ChangeError error={error()} />
        <div class={styles.actions}>
          <Button disabled={busy()} onClick={() => void accept()}>
            {m.account_invitation_accept()}
          </Button>
        </div>
      </section>
    </Show>
  )
}

export function InvitedRegistration() {
  const [token, setToken] = createSignal("")
  const [login, setLogin] = createSignal("")
  const [display, setDisplay] = createSignal("")
  const [password, setPassword] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  onMount(() => setToken(invitationToken()))
  async function submit(event: SubmitEvent) {
    event.preventDefault()
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest("/account/invitations/register", "POST", {
        token: token(),
        login: login(),
        display_name: display(),
        password: password(),
      })
      sessionStorage.removeItem("savorum.invitation")
      window.location.assign("/account")
    } catch (failure) {
      setError(failure)
      setBusy(false)
    }
  }
  return (
    <Show when={token()}>
      <form class={styles.form} onSubmit={(event) => void submit(event)}>
        <h2>{m.account_register_heading()}</h2>
        <p>{m.account_register_existing()}</p>
        <FormField label={m.account_display_name()}>
          <Input
            value={display()}
            onInput={(event) => setDisplay(event.currentTarget.value)}
            autocomplete="name"
          />
        </FormField>
        <FormField label={m.account_login_name()} required>
          <Input
            required
            value={login()}
            onInput={(event) => setLogin(event.currentTarget.value)}
            maxLength={128}
            autocomplete="username"
          />
        </FormField>
        <FormField label={m.account_password()} required>
          <Input
            required
            type="password"
            value={password()}
            onInput={(event) => setPassword(event.currentTarget.value)}
            maxLength={1024}
            autocomplete="new-password"
          />
        </FormField>
        <ChangeError error={error()} />
        <Button type="submit" disabled={busy()}>
          {m.account_register_submit()}
        </Button>
      </form>
    </Show>
  )
}

function invitationState(item: Invitation): string {
  if (item.accepted_at) return m.account_invitation_state_accepted()
  if (item.revoked_at) return m.account_invitation_state_revoked()
  if (item.expires_at <= Date.now() / 1000) return m.account_invitation_state_expired()
  return m.account_invitation_state_expires({ date: formatEpoch(item.expires_at) })
}

/** Invitations belong to one library and never carry instance authority. */
export function LibraryInvitations(props: { libraryID: string; libraryName: string }) {
  const [items, { refetch }] = createResource(
    () => props.libraryID,
    async (id) =>
      (
        await accountRequest<{ invitations: Invitation[] }>(
          `/libraries/${encodeURIComponent(id)}/invitations`,
        )
      ).invitations,
  )
  const [role, setRole] = createSignal("viewer")
  const [hint, setHint] = createSignal("")
  const [days, setDays] = createSignal("7")
  const [url, setURL] = createSignal("")
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  async function create(event: SubmitEvent) {
    event.preventDefault()
    setBusy(true)
    setError(undefined)
    try {
      const result = await accountRequest<{ token: string }>(
        `/libraries/${encodeURIComponent(props.libraryID)}/invitations`,
        "POST",
        { role: role(), hint: hint(), days: Number(days()) },
      )
      setURL(`${window.location.origin}/account#invitation=${encodeURIComponent(result.token)}`)
      setHint("")
      await refetch()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  async function revoke(id: string) {
    setError(undefined)
    try {
      await accountRequest(`/account/invitations/${encodeURIComponent(id)}`, "DELETE")
      await refetch()
    } catch (failure) {
      setError(failure)
    }
  }
  return (
    <section class={styles.section}>
      <h2>{m.account_invitations_heading()}</h2>
      <p class={styles.muted}>
        {m.account_invitations_hint({ library: isolate(props.libraryName) })}
      </p>
      <form class={styles.form} onSubmit={(event) => void create(event)}>
        <FormField label={m.account_invitations_label()} hint={m.account_invitations_label_hint()}>
          <Input
            value={hint()}
            onInput={(event) => setHint(event.currentTarget.value)}
            maxLength={320}
          />
        </FormField>
        <FormField label={m.account_invitations_role()}>
          <Select
            value={role()}
            onValueChange={setRole}
            items={[
              { value: "viewer", label: m.account_invitations_role_viewer() },
              { value: "editor", label: m.account_invitations_role_editor() },
              { value: "owner", label: m.account_invitations_role_owner() },
            ]}
          />
        </FormField>
        <FormField label={m.account_invitations_expiry()} required>
          <Input
            type="number"
            min={1}
            max={30}
            required
            value={days()}
            onInput={(event) => setDays(event.currentTarget.value)}
          />
        </FormField>
        <Button type="submit" disabled={busy()}>
          {m.account_invitations_create()}
        </Button>
      </form>
      <Show when={url()}>
        <div class={styles.card}>
          <h3>{m.account_invitations_link()}</h3>
          <p class={styles.muted}>{m.account_invitations_link_hint()}</p>
          <CopyField
            label={m.account_invitations_link()}
            value={url()}
            copyLabel={m.account_copy()}
            copiedLabel={m.account_copied()}
          />
          <div class={styles.actions}>
            <Button variant="outline" onClick={() => setURL("")}>
              {m.account_invitations_dismiss()}
            </Button>
          </div>
        </div>
      </Show>
      <ChangeError error={error() ?? items.error} />
      <Show
        when={settled(items)?.length}
        fallback={settled(items) ? <p>{m.account_invitations_none()}</p> : undefined}
      >
        <ul class={styles.list}>
          <For each={settled(items)}>
            {(item) => (
              <li class={styles.row}>
                <span class={styles.rowLabel}>
                  {item.hint || m.account_invitations_unnamed()} ·{" "}
                  {roleName(item.role as AccountLibrary["role"])} · {invitationState(item)}
                </span>
                <Show
                  when={
                    !item.accepted_at && !item.revoked_at && item.expires_at > Date.now() / 1000
                  }
                >
                  <Button
                    size="sm"
                    variant="outline"
                    aria-label={m.account_invitations_revoke_named({
                      label: item.hint || m.account_invitations_unnamed(),
                    })}
                    onClick={() => void revoke(item.id)}
                  >
                    {m.account_invitations_revoke()}
                  </Button>
                </Show>
              </li>
            )}
          </For>
        </ul>
      </Show>
    </section>
  )
}
