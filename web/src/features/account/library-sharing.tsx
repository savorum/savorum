import { Link } from "@tanstack/solid-router"
import { Share } from "lucide-solid"
import { Show, createEffect, createResource, createSignal, type JSX } from "solid-js"
import { css } from "styled-system/css"

import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { CopyField } from "@/components/ui/clipboard"
import { Dialog } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import {
  accountRequest,
  administers,
  refreshAccount,
  roleItems,
  type AccountLibrary,
  type AccountUser,
} from "./account-state"
import { hostingAccountURL, readManagedHosting } from "./managed-hosting"

type Library = Pick<AccountLibrary, "id" | "name">

const dialogContent = css({ maxWidth: "34rem" })
const dialogBody = css({ display: "grid", gap: "4", marginTop: "2" })

function DialogShell(props: {
  title: string
  description?: string
  onClose: () => void
  children: JSX.Element
}) {
  return (
    <Dialog
      open
      title={props.title}
      description={props.description}
      onClose={props.onClose}
      contentClass={dialogContent}
      footer={
        <Button variant="outline" onClick={props.onClose}>
          {m.dialog_done()}
        </Button>
      }
    >
      <div class={dialogBody}>{props.children}</div>
    </Dialog>
  )
}

/**
 * Creates invitation links for one of `libraries`. A managed instance hands
 * link creation to the hosting service. `linkToSettings` points back to where
 * the library's unused links are managed.
 */
export function InvitationLinkDialog(props: {
  libraries: Library[]
  linkToSettings?: boolean
  onCreated?: () => void
  onClose: () => void
}) {
  const [hosting] = createResource(readManagedHosting)
  const [libraryID, setLibraryID] = createSignal("")
  const [creating, setCreating] = createSignal(false)
  const library = () =>
    props.libraries.find((entry) => entry.id === libraryID()) ?? props.libraries[0]
  return (
    <DialogShell
      title={m.account_admin_invite_heading()}
      description={
        library() ? m.account_invitations_hint({ library: isolate(library()?.name ?? "") }) : ""
      }
      onClose={props.onClose}
    >
      <Show when={props.libraries.length > 1}>
        <FormField label={m.account_admin_invite_library()}>
          <Select
            value={library()?.id ?? ""}
            disabled={creating()}
            onValueChange={setLibraryID}
            items={props.libraries.map((entry) => ({ value: entry.id, label: entry.name }))}
          />
        </FormField>
      </Show>
      <ChangeError error={hosting.error} />
      <Show when={!hosting.loading && !hosting.error && library()} keyed>
        {(entry) => (
          <Show
            when={hosting()}
            fallback={
              <LinkCreator
                libraryID={entry.id}
                onBusy={setCreating}
                onCreated={props.onCreated}
                settingsLink={
                  props.linkToSettings ? (
                    <Link
                      class={styles.navLink}
                      to="/libraries/$libraryId/settings"
                      params={{ libraryId: entry.id }}
                      hash="invitations"
                    >
                      {m.library_settings()}
                    </Link>
                  ) : undefined
                }
              />
            }
          >
            {(managed) => (
              <div class={step}>
                <p>{m.account_hosted_invitations_hint()}</p>
                <a class={styles.navLink} href={hostingAccountURL(managed(), entry.id)}>
                  {m.account_invitations_create()}
                </a>
              </div>
            )}
          </Show>
        )}
      </Show>
    </DialogShell>
  )
}

function canShare(url: string): boolean {
  if (typeof navigator === "undefined" || typeof navigator.share !== "function") return false
  return navigator.canShare?.({ url }) ?? true
}

/** Creates one link at a time and shows it until the owner starts another. */
function LinkCreator(props: {
  libraryID: string
  settingsLink?: JSX.Element
  onBusy: (busy: boolean) => void
  onCreated?: () => void
}) {
  const [role, setRole] = createSignal("viewer")
  const [hint, setHint] = createSignal("")
  const [days, setDays] = createSignal("7")
  const [url, setURL] = createSignal("")
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  createEffect(() => props.onBusy(busy()))
  async function create(event: SubmitEvent) {
    event.preventDefault()
    setBusy(true)
    setError(undefined)
    try {
      const result = await accountRequest<{ token: string; url?: string }>(
        `/libraries/${encodeURIComponent(props.libraryID)}/invitations`,
        "POST",
        { role: role(), hint: hint(), days: Number(days()) },
      )
      setURL(result.url || `${window.location.origin}/join#${encodeURIComponent(result.token)}`)
      setHint("")
      props.onCreated?.()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  async function share() {
    setError(undefined)
    try {
      await navigator.share({ url: url() })
    } catch (failure) {
      if (!(failure instanceof DOMException && failure.name === "AbortError")) setError(failure)
    }
  }
  return (
    <Show
      when={url()}
      fallback={
        <form class={styles.form} onSubmit={(event) => void create(event)}>
          <FormField
            label={m.account_invitations_label()}
            hint={m.account_invitations_label_hint()}
          >
            <Input
              value={hint()}
              onInput={(event) => setHint(event.currentTarget.value)}
              maxLength={320}
            />
          </FormField>
          <div class={linkSettings}>
            <FormField label={m.account_members_role()}>
              <Select
                value={role()}
                onValueChange={setRole}
                items={roleItems("viewer", "editor")}
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
          </div>
          <ChangeError error={error()} />
          <Button type="submit" disabled={busy()}>
            {m.account_invitations_create()}
          </Button>
        </form>
      }
    >
      {(link) => (
        <div class={step} role="status">
          <h3>{m.account_invitations_ready()}</h3>
          <p class={styles.muted}>{m.account_invitations_link_hint()}</p>
          <CopyField
            label={m.account_invitations_link()}
            value={link()}
            copyLabel={m.account_copy()}
            copiedLabel={m.account_copied()}
          />
          <ChangeError error={error()} />
          <div class={styles.actions}>
            <Show when={canShare(link())}>
              <Button onClick={() => void share()}>
                <Share size={16} aria-hidden="true" />
                {m.account_invitations_share()}
              </Button>
            </Show>
            <Button variant="plain" onClick={() => setURL("")}>
              {m.account_invitations_another()}
            </Button>
            {props.settingsLink}
          </div>
        </div>
      )}
    </Show>
  )
}

/** Administrators choose an account by name; other owners enter its Savorum user ID. */
export function AddMemberDialog(props: {
  libraryID: string
  onAdded?: () => void
  onClose: () => void
}) {
  const members = () => `/libraries/${encodeURIComponent(props.libraryID)}/members`
  const [candidates] = createResource(
    () => administers() && members(),
    async (path) => {
      const [current, users] = await Promise.all([
        accountRequest<{ members: { user_id: string }[] }>(path),
        accountRequest<{ users: AccountUser[] }>("/users"),
      ])
      const existing = new Set(current.members.map((member) => member.user_id))
      return users.users.filter((user) => !user.disabled && !existing.has(user.id))
    },
  )
  const [person, setPerson] = createSignal("")
  const [role, setRole] = createSignal("viewer")
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  async function add(event: SubmitEvent) {
    event.preventDefault()
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest(`${members()}/${encodeURIComponent(person())}`, "PUT", { role: role() })
      await refreshAccount()
      props.onAdded?.()
      props.onClose()
    } catch (failure) {
      setError(failure)
      setBusy(false)
    }
  }
  const choices = () => settled(candidates) ?? []
  return (
    <DialogShell
      title={m.account_members_add()}
      description={administers() ? undefined : m.account_members_add_hint_owner()}
      onClose={props.onClose}
    >
      <ChangeError error={candidates.error} />
      <Show
        when={!administers() || choices().length > 0}
        fallback={
          <Show when={settled(candidates)}>
            <p class={styles.muted}>{m.library_members_all_added()}</p>
          </Show>
        }
      >
        <form class={styles.form} onSubmit={(event) => void add(event)}>
          <Show
            when={administers()}
            fallback={
              <FormField label={m.account_members_user_id()} required>
                <Input
                  required
                  maxLength={64}
                  value={person()}
                  onInput={(event) => setPerson(event.currentTarget.value)}
                />
              </FormField>
            }
          >
            <FormField label={m.account_members_person()} required>
              <Select
                required
                placeholder={m.account_members_choose()}
                value={person()}
                onValueChange={setPerson}
                items={choices().map((user) => ({ value: user.id, label: user.display_name }))}
              />
            </FormField>
          </Show>
          <FormField label={m.account_members_role()}>
            <Select
              value={role()}
              onValueChange={setRole}
              items={roleItems("viewer", "editor", "owner")}
            />
          </FormField>
          <ChangeError error={error()} />
          <Button type="submit" disabled={busy() || !person()}>
            {m.account_members_add()}
          </Button>
        </form>
      </Show>
    </DialogShell>
  )
}

const linkSettings = css({
  display: "grid",
  gap: "3",
  gridTemplateColumns: { base: "1fr", sm: "minmax(0, 1fr) 10rem" },
  alignItems: "start",
})

const step = css({
  display: "grid",
  gap: "3",
  minWidth: "0",
  "& > h3": { fontSize: "md", fontWeight: "semibold", lineHeight: "1.4" },
})
