import { ArrowUpRight, Smartphone } from "lucide-solid"
import { Show, children, createResource, createSignal, onMount, type JSX } from "solid-js"
import { cx } from "styled-system/css"

import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import {
  account,
  accountRequest,
  refreshAccount,
  roleName,
  type AccountLibrary,
} from "./account-state"
import { invitationEntryStyles as entryStyles } from "./invitation-entry-styles"
import { readManagedHosting } from "./managed-hosting"
import { SignOutDialog } from "./sign-out-dialog"

type InvitationPreview = {
  library_id: string
  library_name: string
  role: AccountLibrary["role"]
  accepted: boolean
}

function previewInvitation(token: string) {
  return accountRequest<InvitationPreview>("/account/invitations/preview", "POST", { token })
}

function InvitationDetails(props: { preview?: InvitationPreview }) {
  return (
    <Show when={props.preview}>
      {(preview) => (
        <p>
          <strong>{preview().library_name}</strong> · {roleName(preview().role)}
        </p>
      )}
    </Show>
  )
}

function joinToken(fragment: string): string {
  try {
    return decodeURIComponent(fragment)
  } catch {
    return ""
  }
}

const invitationKey = "savorum.invitation"

export function forgetInvitation() {
  sessionStorage.removeItem(invitationKey)
}

/** The invitation this browser holds and what it grants. */
function createInvitationPreview() {
  const [token, setToken] = createSignal("")
  onMount(() => setToken(invitationToken()))
  const [preview, actions] = createResource(() => token() || false, previewInvitation)
  return { token, setToken, preview, ...actions }
}

// The fragment stays out of server request logs and is removed as soon as it is read.
// Links take the form `/join#<token>` or `/account#invitation=<token>`.
export function invitationToken() {
  const location = new URL(window.location.href)
  if (location.searchParams.get("invitation") === "accepted") {
    forgetInvitation()
    location.searchParams.delete("invitation")
    history.replaceState(null, "", location.pathname + location.search + location.hash)
    return ""
  }
  const fragment = window.location.hash.slice(1)
  const token = window.location.pathname.endsWith("/join")
    ? joinToken(fragment)
    : new URLSearchParams(fragment).get("invitation")
  if (token) {
    sessionStorage.setItem(invitationKey, token)
    history.replaceState(null, "", window.location.pathname + window.location.search)
  }
  return token || sessionStorage.getItem(invitationKey) || ""
}

/** The token stays in the fragment, so it never reaches a server log. */
export function appInvitationURL(origin: string, token: string) {
  return `savorum://invitation#server=${encodeURIComponent(origin)}&token=${encodeURIComponent(token)}`
}

function OpenInApp(props: { token: string }) {
  return (
    <a class={entryStyles.appLink} href={appInvitationURL(window.location.origin, props.token)}>
      <Smartphone size={18} aria-hidden="true" />
      <span>{m.account_invitation_open_app()}</span>
      <ArrowUpRight size={16} aria-hidden="true" />
    </a>
  )
}

export function InvitationAcceptance() {
  const [invited, setInvited] = createSignal(false)
  onMount(() => setInvited(!!invitationToken()))
  const [hosting] = createResource(invited, readManagedHosting)
  return (
    <Show when={invited() && !hosting.loading}>
      <ChangeError error={hosting.error} />
      <Show when={!hosting.error}>
        <Show when={hosting()} fallback={<LocalInvitationAcceptance />}>
          {(managed) => <HostedInvitationNotice origin={managed().origin} />}
        </Show>
      </Show>
    </Show>
  )
}

function LocalInvitationAcceptance() {
  const {
    token,
    setToken,
    preview,
    refetch: retryPreview,
    mutate: updatePreview,
  } = createInvitationPreview()
  const [switching, setSwitching] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  let acceptanceUncertain = false

  async function ensureAccepted(target: InvitationPreview): Promise<InvitationPreview> {
    if (target.accepted) return target
    if (acceptanceUncertain) {
      target = await previewInvitation(token())
      updatePreview(target)
      if (target.accepted) {
        acceptanceUncertain = false
        return target
      }
    }
    acceptanceUncertain = true
    try {
      await accountRequest("/account/invitations/accept", "POST", { token: token() })
      target = { ...target, accepted: true }
    } catch (failure) {
      target = await previewInvitation(token())
      updatePreview(target)
      if (!target.accepted) throw failure
    }
    acceptanceUncertain = false
    updatePreview(target)
    return target
  }

  async function accept() {
    setBusy(true)
    setError(undefined)
    try {
      const current = preview()
      if (!current) return
      const target = await ensureAccepted(current)
      await accountRequest("/account/library", "POST", { library_id: target.library_id })
      await refreshAccount()
      forgetInvitation()
      setToken("")
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
        <InvitationDetails preview={preview()} />
        <ChangeError error={preview.error} />
        <Show when={preview.error}>
          <Button variant="outline" onClick={() => void retryPreview()}>
            {m.account_retry()}
          </Button>
        </Show>
        <p>{m.account_invitation_join_as({ name: account()?.user?.display_name ?? "" })}</p>
        <ChangeError error={error()} />
        <div class={styles.actions}>
          <Button disabled={busy() || !preview()} onClick={() => void accept()}>
            {preview()?.accepted
              ? m.account_invitation_open_library()
              : m.account_invitation_accept()}
          </Button>
          <OpenInApp token={token()} />
          <Button variant="outline" disabled={busy()} onClick={() => setSwitching(true)}>
            {m.account_overview_sign_out()}
          </Button>
        </div>
        <SignOutDialog open={switching()} onClose={() => setSwitching(false)} returnTo="/account" />
      </section>
    </Show>
  )
}

export function InvitedRegistration(props: { signIn?: JSX.Element } = {}) {
  const [hosting, { refetch }] = createResource(readManagedHosting)
  return (
    <>
      <Show when={hosting.loading}>
        <p role="status">{m.common_loading()}</p>
      </Show>
      <Show when={hosting.error}>
        <Button variant="outline" onClick={() => void refetch()}>
          {m.account_retry()}
        </Button>
      </Show>
      <Show when={!hosting.loading}>
        <ChangeError error={hosting.error} />
        <Show when={!hosting.error}>
          <Show when={hosting()} fallback={<LocalInvitedRegistration signIn={props.signIn} />}>
            {(managed) => (
              <>
                <h1 class={styles.heading}>{m.account_invitation_heading()}</h1>
                <HostedInvitationNotice origin={managed().origin} />
              </>
            )}
          </Show>
        </Show>
      </Show>
    </>
  )
}

function LocalInvitedRegistration(props: { signIn?: JSX.Element }) {
  const signIn = children(() => props.signIn)
  const [existingAccount, setExistingAccount] = createSignal(false)
  const { token, preview, refetch: retryPreview } = createInvitationPreview()
  const [login, setLogin] = createSignal("")
  const [display, setDisplay] = createSignal("")
  const [password, setPassword] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
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
      forgetInvitation()
      window.location.assign("/account")
    } catch (failure) {
      setError(failure)
      setBusy(false)
    }
  }
  return (
    <Show when={token()} fallback={signIn()}>
      <section class={cx(styles.section, entryStyles.container)}>
        <Show when={signIn()} fallback={<h2>{m.account_register_submit()}</h2>}>
          <h1 class={styles.heading}>
            {existingAccount()
              ? m.account_invitation_sign_in_heading()
              : m.account_invitation_create_heading()}
          </h1>
        </Show>
        <InvitationDetails preview={preview()} />
        <ChangeError error={preview.error} />
        <Show when={preview.error}>
          <Button variant="outline" onClick={() => void retryPreview()}>
            {m.account_retry()}
          </Button>
        </Show>
        <Show when={!existingAccount()} fallback={signIn()}>
          <form class={styles.form} onSubmit={(event) => void submit(event)}>
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
            <FormField
              label={m.account_password()}
              hint={m.account_register_password_hint()}
              required
            >
              <Input
                required
                type="password"
                value={password()}
                onInput={(event) => setPassword(event.currentTarget.value)}
                minLength={12}
                maxLength={1024}
                autocomplete="new-password"
              />
            </FormField>
            <ChangeError error={error()} />
            <div class={styles.actions}>
              <Button type="submit" disabled={busy() || !preview()}>
                {m.account_register_submit()}
              </Button>
            </div>
          </form>
        </Show>
        <Show when={signIn()}>
          <p class={entryStyles.accountSwitch}>
            <span>
              {existingAccount()
                ? m.account_invitation_need_account()
                : m.account_register_existing()}
            </span>
            <button
              type="button"
              class={entryStyles.switchAction}
              disabled={busy()}
              onClick={() => setExistingAccount(!existingAccount())}
            >
              {existingAccount() ? m.account_register_heading() : m.account_sign_in()}
            </button>
          </p>
        </Show>
        <div class={entryStyles.appAlternative}>
          <OpenInApp token={token()} />
        </div>
      </section>
    </Show>
  )
}

function HostedInvitationNotice(props: { origin: string }) {
  const [invited, setInvited] = createSignal(false)
  onMount(() => setInvited(!!invitationToken()))
  return (
    <Show when={invited()}>
      <section class={styles.section}>
        <p>{m.account_hosted_legacy_invitation()}</p>
        <a class={styles.navLink} href={props.origin}>
          {m.account_hosted_account()}
        </a>
      </section>
    </Show>
  )
}
