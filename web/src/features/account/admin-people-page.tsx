import { Link } from "@tanstack/solid-router"
import { For, Show, createResource, createSignal } from "solid-js"

import { Callout, ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { account, accountRequest, libraries, type AccountUser } from "./account-state"
import { AdminPage } from "./admin-page"
import { ManagedSignInMethods } from "./sign-in-methods"

export function AdminPeoplePage() {
  return (
    <AdminPage title={m.account_admin_people_title()} lede={m.account_admin_people_lede()}>
      <People />
    </AdminPage>
  )
}

function People() {
  const [users, { refetch }] = createResource(
    async () => (await accountRequest<{ users: AccountUser[] }>("/users")).users,
  )
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  async function run(path: string, method: string, body: unknown) {
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest(path, method, body)
      await refetch()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <section class={styles.section}>
        <h2>{m.account_admin_people_heading()}</h2>
        <ChangeError error={error()} />
        <Show when={users.error}>
          <Callout tone="danger">{m.account_admin_people_error()}</Callout>
        </Show>
        <ul class={styles.list}>
          <For each={settled(users)}>
            {(user) => <PersonCard user={user} busy={busy()} run={run} />}
          </For>
        </ul>
      </section>
      <CreatePerson busy={busy()} run={run} />
    </>
  )
}

function PersonCard(props: {
  user: AccountUser
  busy: boolean
  run: (path: string, method: string, body: unknown) => Promise<void>
}) {
  const [password, setPassword] = createSignal("")
  const [disabling, setDisabling] = createSignal(false)
  // An administrator changes their own sign-in where every account does, and
  // cannot lock themselves out from here.
  const self = () => account()?.user?.id === props.user.id
  return (
    <li class={styles.card}>
      <h3>{props.user.display_name}</h3>
      <p class={styles.muted}>
        {[
          props.user.instance_admin ? m.account_admin_person_admin() : "",
          props.user.disabled ? m.account_admin_person_disabled() : "",
          m.account_admin_person_id({ id: props.user.id }),
        ]
          .filter(Boolean)
          .join(" · ")}
      </p>
      <Show
        when={!self()}
        fallback={
          <p class={styles.muted}>
            {m.account_admin_person_self()}{" "}
            <Link to="/account/security">{m.account_nav_security()}</Link>
          </p>
        }
      >
        <ManagedSignInMethods userID={props.user.id} displayName={props.user.display_name} />
        <div class={styles.actions}>
          <Button
            variant="outline"
            disabled={props.busy}
            aria-label={
              props.user.disabled
                ? m.account_admin_enable_named({ name: props.user.display_name })
                : m.account_admin_disable_named({ name: props.user.display_name })
            }
            onClick={() => {
              if (props.user.disabled)
                void props.run(`/users/${encodeURIComponent(props.user.id)}`, "PUT", {
                  disabled: false,
                })
              else setDisabling(true)
            }}
          >
            {props.user.disabled ? m.account_admin_enable() : m.account_admin_disable()}
          </Button>
        </div>
        <Show when={disabling()}>
          <ConfirmDialog
            title={m.account_admin_disable_title({ name: isolate(props.user.display_name) })}
            description={m.account_admin_disable_description()}
            confirmLabel={m.account_admin_disable()}
            cancelLabel={m.common_cancel()}
            busy={props.busy}
            onClose={() => setDisabling(false)}
            onConfirm={() =>
              void props
                .run(`/users/${encodeURIComponent(props.user.id)}`, "PUT", { disabled: true })
                .then(() => setDisabling(false))
            }
          />
        </Show>
        <form
          class={styles.row}
          onSubmit={(event) => {
            event.preventDefault()
            void props
              .run(`/users/${encodeURIComponent(props.user.id)}`, "PUT", { password: password() })
              .then(() => setPassword(""))
          }}
        >
          <FormField
            label={m.account_admin_reset_password({ name: props.user.display_name })}
            required
          >
            <Input
              type="password"
              required
              maxLength={1024}
              autocomplete="new-password"
              value={password()}
              onInput={(event) => setPassword(event.currentTarget.value)}
            />
          </FormField>
          <Button type="submit" variant="outline" disabled={props.busy}>
            {m.account_admin_reset_submit()}
          </Button>
        </form>
      </Show>
    </li>
  )
}

function CreatePerson(props: {
  busy: boolean
  run: (path: string, method: string, body: unknown) => Promise<void>
}) {
  const [login, setLogin] = createSignal("")
  const [name, setName] = createSignal("")
  const [password, setPassword] = createSignal("")
  const [library, setLibrary] = createSignal("")
  const [role, setRole] = createSignal("viewer")
  const owned = () => libraries().filter((entry) => entry.role === "owner")
  return (
    <section class={styles.section}>
      <h2>{m.account_admin_create_heading()}</h2>
      <p class={styles.muted}>{m.account_admin_create_hint()}</p>
      <form
        class={styles.form}
        onSubmit={(event) => {
          event.preventDefault()
          void props
            .run("/users", "POST", {
              login: login(),
              display_name: name(),
              password: password(),
              ...(library() ? { library_id: library(), role: role() } : {}),
            })
            .then(() => setPassword(""))
        }}
      >
        <FormField label={m.account_login_name()} required>
          <Input
            required
            autocomplete="off"
            maxLength={128}
            value={login()}
            onInput={(event) => setLogin(event.currentTarget.value)}
          />
        </FormField>
        <FormField label={m.account_display_name()} required>
          <Input
            required
            maxLength={128}
            value={name()}
            onInput={(event) => setName(event.currentTarget.value)}
          />
        </FormField>
        <FormField label={m.account_admin_initial_password()} required>
          <Input
            type="password"
            required
            maxLength={1024}
            autocomplete="new-password"
            value={password()}
            onInput={(event) => setPassword(event.currentTarget.value)}
          />
        </FormField>
        <Show when={owned().length}>
          <FormField label={m.account_admin_library()}>
            <Select
              value={library()}
              onValueChange={setLibrary}
              items={[
                { value: "", label: m.account_admin_library_none() },
                ...owned().map((entry) => ({ value: entry.id, label: entry.name })),
              ]}
            />
          </FormField>
          <Show when={library()}>
            <FormField label={m.account_admin_library_role()}>
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
          </Show>
        </Show>
        <Button type="submit" disabled={props.busy}>
          {m.account_admin_create_submit()}
        </Button>
      </form>
    </section>
  )
}
