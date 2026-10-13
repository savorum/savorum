import { Link, useParams } from "@tanstack/solid-router"
import { For, Show, createResource, createSignal } from "solid-js"

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
  accountRequest,
  administers,
  authEnabled,
  libraryByID,
  refreshAccount,
  type AccountUser,
} from "./account-state"
import { AuditView } from "./audit-view"
import { LibraryInvitations } from "./invitations"

type Member = { user_id: string; display_name: string; role: string }

/**
 * Access is addressed by library, not by whichever library another tab last
 * selected: every request on this page names the library in its path.
 */
export function LibraryAccessPage() {
  const params = useParams({ from: "/libraries/$libraryId/access" })
  return <LibraryAccess libraryID={params().libraryId} />
}

export function LibraryAccess(props: { libraryID: string }) {
  const library = () => libraryByID(props.libraryID)
  return (
    <main class={styles.page}>
      <div class={styles.header}>
        <Link class={styles.back} to="/account">
          {m.account_access_back()}
        </Link>
        <h1 class={styles.heading}>
          {m.account_access_title({ library: isolate(library()?.name ?? props.libraryID) })}
        </h1>
        <p class={styles.lede}>{m.account_access_lede()}</p>
      </div>
      <Show
        when={authEnabled()}
        fallback={<p class={styles.section}>{m.account_local_mode_unavailable()}</p>}
      >
        <Show
          when={library()}
          fallback={<p class={styles.section}>{m.account_access_forbidden()}</p>}
        >
          {(entry) => (
            <Show
              when={entry().role === "owner"}
              fallback={
                <p class={styles.section}>
                  {m.account_access_owner_only({ library: isolate(entry().name) })}
                </p>
              }
            >
              <Members libraryID={entry().id} libraryName={entry().name} />
              <LibraryInvitations libraryID={entry().id} libraryName={entry().name} />
              <section class={styles.section}>
                <h2>{m.account_activity_heading()}</h2>
                <AuditView library={entry().id} />
              </section>
              <RetireLibrary libraryID={entry().id} libraryName={entry().name} />
            </Show>
          )}
        </Show>
      </Show>
    </main>
  )
}

function Members(props: { libraryID: string; libraryName: string }) {
  const [members, { refetch }] = createResource(
    () => props.libraryID,
    async (id) =>
      (await accountRequest<{ members: Member[] }>(`/libraries/${encodeURIComponent(id)}/members`))
        .members,
  )
  const [people] = createResource(
    () => administers(),
    async (enabled) =>
      enabled ? (await accountRequest<{ users: AccountUser[] }>("/users")).users : [],
  )
  const [person, setPerson] = createSignal("")
  const [role, setRole] = createSignal("viewer")
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  async function change(path: string, method: string, body?: unknown) {
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest(`/libraries/${encodeURIComponent(props.libraryID)}${path}`, method, body)
      await refetch()
      await refreshAccount()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  const candidates = () => {
    const existing = new Set((settled(members) ?? []).map((member) => member.user_id))
    return (settled(people) ?? []).filter((user) => !user.disabled && !existing.has(user.id))
  }
  return (
    <section class={styles.section}>
      <h2>{m.account_members_heading()}</h2>
      <ChangeError error={error()} />
      <Show when={members.error}>
        <Callout tone="danger">{m.account_members_error()}</Callout>
      </Show>
      <ul class={styles.list}>
        <For each={settled(members)}>
          {(member) => (
            <li class={styles.row}>
              <span class={styles.rowLabel}>{member.display_name}</span>
              <Select
                class={styles.chooser}
                size="sm"
                aria-label={m.account_members_role_for({ name: member.display_name })}
                value={member.role}
                disabled={busy()}
                onValueChange={(value) =>
                  void change(`/members/${encodeURIComponent(member.user_id)}`, "PUT", {
                    role: value,
                  })
                }
                items={[
                  { value: "owner", label: m.account_role_owner() },
                  { value: "editor", label: m.account_role_editor() },
                  { value: "viewer", label: m.account_role_viewer() },
                ]}
              />
              <Button
                size="sm"
                variant="outline"
                disabled={busy()}
                aria-label={m.account_members_remove_named({
                  name: member.display_name,
                  library: props.libraryName,
                })}
                onClick={() =>
                  void change(`/members/${encodeURIComponent(member.user_id)}`, "DELETE")
                }
              >
                {m.account_members_remove()}
              </Button>
            </li>
          )}
        </For>
      </ul>
      <form
        class={styles.form}
        onSubmit={(event) => {
          event.preventDefault()
          void change(`/members/${encodeURIComponent(person())}`, "PUT", { role: role() }).then(
            () => setPerson(""),
          )
        }}
      >
        <h3>{m.account_members_add_heading()}</h3>
        <Show
          when={administers()}
          fallback={
            <FormField
              label={m.account_members_user_id()}
              hint={m.account_members_add_hint_owner()}
              required
            >
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
              items={candidates().map((user) => ({ value: user.id, label: user.display_name }))}
            />
          </FormField>
        </Show>
        <FormField label={m.account_members_role()}>
          <Select
            value={role()}
            onValueChange={setRole}
            items={[
              { value: "viewer", label: m.account_role_viewer() },
              { value: "editor", label: m.account_role_editor() },
              { value: "owner", label: m.account_role_owner() },
            ]}
          />
        </FormField>
        <Button type="submit" disabled={busy() || !person()}>
          {m.account_members_add()}
        </Button>
      </form>
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
