import { useParams } from "@tanstack/solid-router"
import { Link2 } from "lucide-solid"
import { For, Show, createResource, createSignal, type JSX } from "solid-js"
import { css } from "styled-system/css"

import { Callout, ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
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
  authEnabled,
  libraryByID,
  refreshAccount,
  roleItems,
  roleName,
  type AccountLibrary,
} from "./account-state"
import { AuditView } from "./audit-view"
import { formatEpoch } from "./formats"
import { AddMemberDialog, InvitationLinkDialog } from "./library-sharing"
import { hostingAccountURL, readManagedHosting } from "./managed-hosting"

type Member = { user_id: string; display_name: string; role: AccountLibrary["role"] }

type Invitation = {
  id: string
  role: AccountLibrary["role"]
  hint: string
  expires_at: number
  accepted_at?: number
  revoked_at?: number
}

/** Links Cloud created on this server; Cloud manages them. */
const cloudInvitationHint = "Cloud invitation"

/**
 * Settings are addressed by library, not by whichever library another tab last
 * selected: every request on this page names the library in its path.
 */
export function LibrarySettingsPage() {
  const params = useParams({ from: "/libraries/$libraryId/settings" })
  return <LibrarySettings libraryID={params().libraryId} />
}

export function LibrarySettings(props: { libraryID: string }) {
  const library = () => libraryByID(props.libraryID)
  return (
    <main class={styles.page}>
      <div class={styles.header}>
        <h1 class={styles.heading}>
          {m.library_settings_title({ library: isolate(library()?.name ?? props.libraryID) })}
        </h1>
      </div>
      <Show
        when={authEnabled()}
        fallback={<p class={styles.section}>{m.account_local_mode_unavailable()}</p>}
      >
        <Show
          when={library()}
          fallback={<p class={styles.section}>{m.library_settings_forbidden()}</p>}
        >
          {(entry) => (
            <Show
              when={entry().role === "owner"}
              fallback={
                <p class={styles.section}>
                  {m.library_settings_owner_only({ library: isolate(entry().name) })}
                </p>
              }
            >
              {/* Remount per library so no row acts on the previous library's members. */}
              <Show when={entry().id} keyed>
                <Members library={entry()} />
                <InvitationLinks library={entry()} />
                <section class={styles.section}>
                  <h2>{m.account_activity_heading()}</h2>
                  <AuditView library={entry().id} />
                </section>
                <RetireLibrary libraryID={entry().id} libraryName={entry().name} />
              </Show>
            </Show>
          )}
        </Show>
      </Show>
    </main>
  )
}

function SectionHeader(props: { title: string; action: JSX.Element }) {
  return (
    <div class={styles.sectionHeader}>
      <h2>{props.title}</h2>
      {props.action}
    </div>
  )
}

/** Runs one change at a time and reports its failure in place. */
function createChange(onChanged: () => void) {
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  async function run(path: string, method: string, body?: unknown) {
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest(path, method, body)
      onChanged()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  return { error, busy, run }
}

function Members(props: { library: AccountLibrary }) {
  const path = () => `/libraries/${encodeURIComponent(props.library.id)}/members`
  const [members, { refetch }] = createResource(
    path,
    async (base) => (await accountRequest<{ members: Member[] }>(base)).members,
  )
  const change = createChange(() => {
    void refetch()
    void refreshAccount()
  })
  const [adding, setAdding] = createSignal(false)
  const member = (id: string) => `${path()}/${encodeURIComponent(id)}`
  return (
    <section class={styles.section}>
      <SectionHeader
        title={m.account_members_heading()}
        action={
          <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
            {m.account_members_add()}
          </Button>
        }
      />
      <ChangeError error={change.error()} />
      <Show when={members.error}>
        <Callout tone="danger">{m.account_members_error()}</Callout>
      </Show>
      <ul class={styles.rows}>
        <For each={settled(members)}>
          {(person) => (
            <li class={styles.listRow}>
              <div class={styles.rowBody}>
                <span>{person.display_name}</span>
                <Show when={person.user_id === account()?.user?.id}>
                  <span class={styles.muted}>{m.site_header_you()}</span>
                </Show>
              </div>
              <div class={styles.actions}>
                <Select
                  class={styles.chooser}
                  size="sm"
                  aria-label={m.account_members_role_for({ name: person.display_name })}
                  value={person.role}
                  disabled={change.busy()}
                  onValueChange={(role) => void change.run(member(person.user_id), "PUT", { role })}
                  items={roleItems("owner", "editor", "viewer")}
                />
                <Button
                  size="sm"
                  variant="outline"
                  disabled={change.busy()}
                  aria-label={m.account_members_remove_named({
                    name: person.display_name,
                    library: props.library.name,
                  })}
                  onClick={() => void change.run(member(person.user_id), "DELETE")}
                >
                  {m.account_members_remove()}
                </Button>
              </div>
            </li>
          )}
        </For>
      </ul>
      <Show when={adding()}>
        <AddMemberDialog
          libraryID={props.library.id}
          onAdded={() => void refetch()}
          onClose={() => setAdding(false)}
        />
      </Show>
    </section>
  )
}

/** Links not yet used. Accepted, revoked, and expired links appear in activity. */
function InvitationLinks(props: { library: AccountLibrary }) {
  const [hosting] = createResource(readManagedHosting)
  const managedHosting = () => (hosting.state === "ready" ? hosting() : undefined)
  const [invitations, { refetch }] = createResource(
    () => hosting.state === "ready" && ([props.library.id, !!hosting()] as const),
    async ([id, managed]) =>
      (
        await accountRequest<{ invitations: Invitation[] }>(
          `/libraries/${encodeURIComponent(id)}/invitations`,
        )
      ).invitations.filter(
        (item) =>
          !item.accepted_at &&
          !item.revoked_at &&
          item.expires_at > Date.now() / 1000 &&
          !(managed && item.hint === cloudInvitationHint),
      ),
  )
  const change = createChange(() => void refetch())
  const [creating, setCreating] = createSignal(false)
  return (
    <section class={styles.section} id="invitations">
      <SectionHeader
        title={m.library_invitations_heading()}
        action={
          <Show when={hosting.state === "ready"}>
            <Show
              when={managedHosting()}
              fallback={
                <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
                  {m.account_invitations_create()}
                </Button>
              }
            >
              {(managed) => (
                <a class={styles.navLink} href={hostingAccountURL(managed(), props.library.id)}>
                  {m.account_invitations_create()}
                </a>
              )}
            </Show>
          </Show>
        }
      />
      <Show when={managedHosting()}>
        <p class={styles.muted}>{m.account_hosted_invitations_hint()}</p>
      </Show>
      <ChangeError error={change.error() ?? hosting.error ?? invitations.error} />
      <Show
        when={settled(invitations)?.length}
        fallback={
          <Show when={settled(invitations)}>
            <p class={styles.muted}>{m.library_invitations_empty()}</p>
          </Show>
        }
      >
        <ul class={styles.rows}>
          <For each={settled(invitations)}>
            {(item) => (
              <li class={styles.listRow}>
                <Link2 class={linkIcon} size={16} aria-hidden="true" />
                <div class={styles.rowBody}>
                  <span>{item.hint || m.account_invitations_unnamed()}</span>
                  <span class={styles.muted}>
                    {roleName(item.role)} ·{" "}
                    {m.account_invitation_state_expires({ date: formatEpoch(item.expires_at) })}
                  </span>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={change.busy()}
                  aria-label={m.account_invitations_revoke_named({
                    label: item.hint || m.account_invitations_unnamed(),
                  })}
                  onClick={() =>
                    void change.run(`/account/invitations/${encodeURIComponent(item.id)}`, "DELETE")
                  }
                >
                  {m.account_invitations_revoke()}
                </Button>
              </li>
            )}
          </For>
        </ul>
        <Show when={managedHosting()}>
          <p class={styles.muted}>{m.account_hosted_replace_hint()}</p>
        </Show>
      </Show>
      <Show when={creating()}>
        <InvitationLinkDialog
          libraries={[props.library]}
          onCreated={() => void refetch()}
          onClose={() => setCreating(false)}
        />
      </Show>
    </section>
  )
}

function RetireLibrary(props: { libraryID: string; libraryName: string }) {
  const [confirmation, setConfirmation] = createSignal("")
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  return (
    <section class={styles.section}>
      <h2>{m.account_retire_heading()}</h2>
      <p class={styles.muted}>{m.account_retire_body()}</p>
      <Dialog
        title={m.account_retire_submit({ library: isolate(props.libraryName) })}
        description={m.account_retire_body()}
        trigger={(triggerProps) => (
          <Button {...triggerProps()} variant="outline" colorPalette="red" width="fit-content">
            {m.account_retire_open()}
          </Button>
        )}
      >
        <form
          class={styles.form}
          onSubmit={(event) => {
            event.preventDefault()
            setBusy(true)
            setError(undefined)
            void accountRequest(`/libraries/${encodeURIComponent(props.libraryID)}`, "DELETE", {
              confirmation: confirmation(),
            })
              .then(() => window.location.assign("/account"))
              .catch((failure: unknown) => {
                setError(failure)
                setBusy(false)
              })
          }}
        >
          <FormField label={m.account_retire_confirmation()} required>
            <Input
              aria-label={m.account_retire_confirmation()}
              required
              maxLength={100}
              value={confirmation()}
              onInput={(event) => setConfirmation(event.currentTarget.value)}
            />
          </FormField>
          <ChangeError error={error()} />
          <div class={styles.actions}>
            <DialogClose>
              {(closeProps) => (
                <Button {...closeProps()} variant="outline" disabled={busy()}>
                  {m.common_cancel()}
                </Button>
              )}
            </DialogClose>
            <Button
              type="submit"
              colorPalette="red"
              disabled={busy() || confirmation() !== props.libraryName}
            >
              {m.account_retire_submit({ library: isolate(props.libraryName) })}
            </Button>
          </div>
        </form>
      </Dialog>
    </section>
  )
}

const linkIcon = css({ color: "fg.muted", flexShrink: "0" })
