import { createResource, createSignal } from "solid-js"

import { ChangeError, Notice } from "@/components/status"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { accountRequest, refreshAccount, type AccountUser } from "./account-state"
import { AdminPage } from "./admin-page"

export function AdminLibrariesPage() {
  return (
    <AdminPage title={m.account_admin_libraries_title()} lede={m.account_admin_libraries_lede()}>
      <CreateLibrary />
      <RecoverOwnership />
    </AdminPage>
  )
}

function CreateLibrary() {
  const [name, setName] = createSignal("")
  const [error, setError] = createSignal<unknown>()
  const [notice, setNotice] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  return (
    <section class={styles.section}>
      <h2>{m.account_admin_create_library_heading()}</h2>
      <p class={styles.muted}>{m.account_admin_create_library_hint()}</p>
      <ChangeError error={error()} />
      <Notice message={notice()} />
      <form
        class={styles.form}
        onSubmit={(event) => {
          event.preventDefault()
          setBusy(true)
          setError(undefined)
          setNotice("")
          const staticName = name()
          void accountRequest("/libraries", "POST", { name: staticName })
            .then(async () => {
              setName("")
              setNotice(m.account_admin_library_created({ library: isolate(staticName) }))
              await refreshAccount()
            })
            .catch((failure: unknown) => setError(failure))
            .finally(() => setBusy(false))
        }}
      >
        <FormField label={m.account_admin_library_name()} required>
          <Input
            required
            maxLength={100}
            value={name()}
            onInput={(event) => setName(event.currentTarget.value)}
          />
        </FormField>
        <Button type="submit" disabled={busy()}>
          {m.account_admin_create_library_submit()}
        </Button>
      </form>
    </section>
  )
}

/**
 * The administrator is deliberately not a member of the library being
 * recovered, so it is named by its identifier rather than chosen from a list.
 */
function RecoverOwnership() {
  const [people] = createResource(
    async () => (await accountRequest<{ users: AccountUser[] }>("/users")).users,
  )
  const [library, setLibrary] = createSignal("")
  const [owner, setOwner] = createSignal("")
  const [error, setError] = createSignal<unknown>()
  const [notice, setNotice] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  return (
    <section class={styles.section}>
      <h2>{m.account_admin_recover_heading()}</h2>
      <p class={styles.muted}>{m.account_admin_recover_hint()}</p>
      <ChangeError error={error()} />
      <Notice message={notice()} />
      <form
        class={styles.form}
        onSubmit={(event) => {
          event.preventDefault()
          setBusy(true)
          setError(undefined)
          setNotice("")
          void accountRequest(`/libraries/${encodeURIComponent(library())}/recover`, "POST", {
            user_id: owner(),
          })
            .then(async () => {
              setNotice(m.account_admin_recovered())
              await refreshAccount()
            })
            .catch((failure: unknown) => setError(failure))
            .finally(() => setBusy(false))
        }}
      >
        <FormField
          label={m.account_admin_recover_library()}
          hint={m.account_admin_recover_library_hint()}
          required
        >
          <Input
            required
            maxLength={64}
            value={library()}
            onInput={(event) => setLibrary(event.currentTarget.value)}
          />
        </FormField>
        <FormField label={m.account_admin_recover_owner()} required>
          <Select
            required
            placeholder={m.account_members_choose()}
            value={owner()}
            onValueChange={setOwner}
            items={(settled(people) ?? [])
              .filter((user) => !user.disabled)
              .map((user) => ({ value: user.id, label: user.display_name }))}
          />
        </FormField>
        <Button type="submit" disabled={busy() || !owner() || !library()}>
          {m.account_admin_recover_submit()}
        </Button>
      </form>
    </section>
  )
}
