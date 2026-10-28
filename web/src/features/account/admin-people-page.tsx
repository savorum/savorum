import { Link } from "@tanstack/solid-router"
import { For, Show, createMemo, createResource, createSignal, createUniqueId } from "solid-js"
import { css } from "styled-system/css"

import { SkeletonRows } from "@/components/skeleton"
import { Callout, ChangeError, Notice } from "@/components/status"
import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/clipboard"
import { Dialog } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
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
  const people = createMemo<AccountUser[]>((previous) => settled(users) ?? previous, [])
  const [creating, setCreating] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  async function run(path: string, method: string, body: unknown) {
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest(path, method, body)
      await refetch()
      return true
    } catch (failure) {
      setError(failure)
      return false
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <section class={styles.section}>
        <div
          class={css({
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: "3",
          })}
        >
          <h2 class={css({ fontFamily: "editorial", fontSize: "xl" })}>
            {m.account_admin_people_heading()}
          </h2>
          <Button
            size="sm"
            onClick={() => {
              setError(undefined)
              setCreating(true)
            }}
          >
            {m.account_admin_create_heading()}
          </Button>
        </div>
        <Show when={users.error}>
          <Callout
            tone="danger"
            actions={
              <Button variant="outline" onClick={() => void refetch()}>
                {m.account_retry()}
              </Button>
            }
          >
            {m.account_admin_people_error()}
          </Callout>
        </Show>
        <Show when={users.loading && people().length === 0}>
          <SkeletonRows count={3} />
        </Show>
        <ul class={styles.rows}>
          <For each={people().map((user) => user.id)}>
            {(id) => {
              const initial = people().find((user) => user.id === id)!
              return (
                <PersonRow
                  user={people().find((user) => user.id === id) ?? initial}
                  busy={busy()}
                  error={error() ?? users.error}
                  onClearError={() => setError(undefined)}
                  run={run}
                />
              )
            }}
          </For>
        </ul>
      </section>
      <Show when={creating()}>
        <CreatePerson
          busy={busy()}
          error={error()}
          run={run}
          onClose={() => {
            setCreating(false)
            setError(undefined)
          }}
        />
      </Show>
    </>
  )
}

type PersonAction = (path: string, method: string, body: unknown) => Promise<boolean>

function PersonRow(props: {
  user: AccountUser
  busy: boolean
  error: unknown
  onClearError: () => void
  run: PersonAction
}) {
  const [managing, setManaging] = createSignal(false)
  const self = () => account()?.user?.id === props.user.id
  return (
    <li class={styles.listRow}>
      <div class={styles.rowBody}>
        <h3>{props.user.display_name}</h3>
        <p class={styles.muted}>
          {[
            props.user.instance_admin ? m.account_admin_person_admin() : "",
            props.user.disabled ? m.account_admin_person_disabled() : m.account_admin_active(),
            self() ? m.site_header_you() : "",
          ]
            .filter(Boolean)
            .join(" · ")}
        </p>
      </div>
      <Show
        when={!self()}
        fallback={
          <Link class={styles.back} to="/account/security">
            {m.account_nav_security()}
          </Link>
        }
      >
        <div class={styles.actions}>
          <Button
            size="sm"
            variant="outline"
            disabled={props.busy}
            aria-label={m.account_admin_manage_named({ name: props.user.display_name })}
            onClick={() => {
              props.onClearError()
              setManaging(true)
            }}
          >
            {m.account_admin_manage()}
          </Button>
        </div>
        <Show when={managing()}>
          <ManagePerson
            user={props.user}
            busy={props.busy}
            error={props.error}
            run={props.run}
            onClose={() => {
              setManaging(false)
              props.onClearError()
            }}
          />
        </Show>
      </Show>
    </li>
  )
}

function ManagePerson(props: {
  user: AccountUser
  busy: boolean
  error: unknown
  run: PersonAction
  onClose: () => void
}) {
  const [password, setPassword] = createSignal("")
  const [saved, setSaved] = createSignal(false)
  const passwordHintID = createUniqueId()
  let passwordInput: HTMLInputElement | undefined
  const [disabling, setDisabling] = createSignal(false)
  return (
    <Dialog
      open
      initialFocusEl={() => passwordInput ?? false}
      title={m.account_admin_manage_named({ name: props.user.display_name })}
      onClose={props.onClose}
      closeOnEscape={!props.busy}
      closeOnInteractOutside={!props.busy}
      contentClass={css({ maxWidth: "34rem" })}
      footer={
        <Button disabled={props.busy} onClick={props.onClose}>
          {m.dialog_done()}
        </Button>
      }
    >
      <div class={css({ display: "grid", gap: "5", marginTop: "5" })}>
        <div
          class={css({
            display: "flex",
            flexWrap: "wrap",
            alignItems: "center",
            justifyContent: "space-between",
            gap: "3",
          })}
        >
          <p>
            {props.user.disabled ? m.account_admin_person_disabled() : m.account_admin_active()}
          </p>
          <Show
            when={disabling()}
            fallback={
              <div class={styles.actions}>
                <Button
                  size="sm"
                  variant={props.user.disabled ? "outline" : "plain"}
                  colorPalette={props.user.disabled ? "accent" : "gray"}
                  disabled={props.busy}
                  aria-label={
                    props.user.disabled
                      ? m.account_admin_enable_named({ name: props.user.display_name })
                      : m.account_admin_disable_named({ name: props.user.display_name })
                  }
                  onClick={() => {
                    if (props.user.disabled) {
                      setSaved(false)
                      void props.run(`/users/${encodeURIComponent(props.user.id)}`, "PUT", {
                        disabled: false,
                      })
                    } else setDisabling(true)
                  }}
                >
                  {props.user.disabled ? m.account_admin_enable() : m.account_admin_disable()}
                </Button>
              </div>
            }
          >
            <p class={css({ flexBasis: "100%", color: "fg.muted", fontSize: "sm" })}>
              {m.account_admin_disable_description()}
            </p>
            <div class={styles.actions}>
              <Button
                colorPalette="red"
                disabled={props.busy}
                onClick={() => {
                  setSaved(false)
                  void props
                    .run(`/users/${encodeURIComponent(props.user.id)}`, "PUT", { disabled: true })
                    .then((ok) => {
                      if (ok) setDisabling(false)
                    })
                }}
              >
                {m.account_admin_disable()}
              </Button>
              <Button variant="outline" disabled={props.busy} onClick={() => setDisabling(false)}>
                {m.common_cancel()}
              </Button>
            </div>
          </Show>
        </div>
        <ChangeError error={props.error} />
        <Notice message={saved() ? m.account_admin_password_reset_notice() : ""} />
        <div
          class={css({
            display: "grid",
            gap: "2",
            borderTopWidth: "1px",
            borderColor: "border.subtle",
            paddingTop: "5",
          })}
        >
          <form
            class={css({
              display: "grid",
              gap: "3",
              alignItems: "end",
              gridTemplateColumns: { base: "1fr", sm: "minmax(0, 1fr) auto" },
            })}
            onSubmit={(event) => {
              event.preventDefault()
              setSaved(false)
              void props
                .run(`/users/${encodeURIComponent(props.user.id)}`, "PUT", { password: password() })
                .then((ok) => {
                  if (ok) {
                    setPassword("")
                    setSaved(true)
                  }
                  passwordInput?.focus()
                })
            }}
          >
            <FormField label={m.account_password_new()} required>
              <Input
                ref={(element) => {
                  passwordInput = element
                }}
                type="password"
                aria-label={m.account_admin_reset_password({ name: props.user.display_name })}
                aria-describedby={passwordHintID}
                required
                disabled={props.busy}
                maxLength={1024}
                autocomplete="new-password"
                value={password()}
                onInput={(event) => setPassword(event.currentTarget.value)}
              />
            </FormField>
            <Button
              class={css({ justifySelf: "start" })}
              type="submit"
              variant="outline"
              disabled={props.busy}
            >
              {m.account_admin_reset_action()}
            </Button>
          </form>
          <p id={passwordHintID} class={styles.muted}>
            {m.account_admin_reset_hint()}
          </p>
        </div>
        <ManagedSignInMethods userID={props.user.id} displayName={props.user.display_name} />
        <div
          class={css({
            display: "grid",
            gap: "2",
            borderTopWidth: "1px",
            borderColor: "border.subtle",
            paddingTop: "4",
          })}
        >
          <div
            class={css({
              display: "flex",
              gap: "3",
              justifyContent: "space-between",
              alignItems: "center",
            })}
          >
            <p class={styles.muted}>{m.account_overview_user_id()}</p>
            <CopyButton
              size="xs"
              value={props.user.id}
              copyLabel={m.account_copy()}
              copiedLabel={m.account_copied()}
            />
          </div>
          <code class={css({ overflowWrap: "anywhere", fontSize: "xs", color: "fg.muted" })}>
            {props.user.id}
          </code>
        </div>
      </div>
    </Dialog>
  )
}

function CreatePerson(props: {
  busy: boolean
  error: unknown
  run: PersonAction
  onClose: () => void
}) {
  const [login, setLogin] = createSignal("")
  const [name, setName] = createSignal("")
  const [password, setPassword] = createSignal("")
  const [library, setLibrary] = createSignal("")
  const [role, setRole] = createSignal("viewer")
  const formID = createUniqueId()
  const owned = () => libraries().filter((entry) => entry.role === "owner")
  return (
    <Dialog
      open
      title={m.account_admin_create_heading()}
      description={m.account_admin_create_hint()}
      onClose={props.onClose}
      closeOnEscape={!props.busy}
      closeOnInteractOutside={!props.busy}
      contentClass={css({ maxWidth: "34rem" })}
      footer={
        <>
          <Button variant="outline" disabled={props.busy} onClick={props.onClose}>
            {m.common_cancel()}
          </Button>
          <Button type="submit" form={formID} loading={props.busy}>
            {m.account_admin_create_submit()}
          </Button>
        </>
      }
    >
      <ChangeError error={props.error} />
      <form
        id={formID}
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
            .then((saved) => {
              if (saved) props.onClose()
            })
        }}
      >
        <FormField label={m.account_login_name()} required>
          <Input
            required
            disabled={props.busy}
            autocomplete="off"
            maxLength={128}
            value={login()}
            onInput={(event) => setLogin(event.currentTarget.value)}
          />
        </FormField>
        <FormField label={m.account_display_name()} required>
          <Input
            required
            disabled={props.busy}
            value={name()}
            onInput={(event) => setName(event.currentTarget.value)}
          />
        </FormField>
        <FormField label={m.account_admin_initial_password()} required>
          <Input
            type="password"
            required
            disabled={props.busy}
            maxLength={1024}
            autocomplete="new-password"
            value={password()}
            onInput={(event) => setPassword(event.currentTarget.value)}
          />
        </FormField>
        <Show when={owned().length}>
          <FormField label={m.account_admin_library()}>
            <Select
              disabled={props.busy}
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
                disabled={props.busy}
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
      </form>
    </Dialog>
  )
}
