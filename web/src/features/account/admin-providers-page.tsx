import { For, Show, createResource, createSignal } from "solid-js"

import { Callout, ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { accountRequest, libraries, roleName, type AccountLibrary } from "./account-state"
import { AdminPage } from "./admin-page"
import { readProviders, type Provider } from "./sign-in-methods"

type Mapping = { id: string; provider_id: string; group: string; library_id: string; role: string }

export function AdminProvidersPage() {
  const [providers] = createResource(readProviders)
  return (
    <AdminPage title={m.account_admin_providers_title()} lede={m.account_admin_providers_lede()}>
      <Show
        when={settled(providers)?.length}
        fallback={
          <Show when={settled(providers)}>
            <p class={styles.section}>{m.account_admin_providers_none()}</p>
          </Show>
        }
      >
        <GroupMappings providers={settled(providers) ?? []} />
        <ProviderDependencies providers={settled(providers) ?? []} />
      </Show>
    </AdminPage>
  )
}

function GroupMappings(props: { providers: Provider[] }) {
  const [items, { refetch }] = createResource(
    async () => (await accountRequest<{ mappings: Mapping[] }>("/account/group-mappings")).mappings,
  )
  const [provider, setProvider] = createSignal("")
  const [group, setGroup] = createSignal("")
  const [library, setLibrary] = createSignal("")
  const [role, setRole] = createSignal("viewer")
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  function providerName(id: string): string {
    return props.providers.find((entry) => entry.id === id)?.display_name ?? id
  }
  function libraryName(id: string): string {
    return libraries().find((entry) => entry.id === id)?.name ?? id
  }
  async function create(event: SubmitEvent) {
    event.preventDefault()
    setBusy(true)
    setError(undefined)
    try {
      await accountRequest("/account/group-mappings", "POST", {
        provider_id: provider(),
        group: group(),
        library_id: library(),
        role: role(),
      })
      setGroup("")
      await refetch()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  async function remove(id: string) {
    setError(undefined)
    try {
      await accountRequest(`/account/group-mappings/${encodeURIComponent(id)}`, "DELETE")
      await refetch()
    } catch (failure) {
      setError(failure)
    }
  }
  return (
    <section class={styles.section}>
      <h2>{m.account_admin_mappings_heading()}</h2>
      <p class={styles.muted}>{m.account_admin_mapping_hint()}</p>
      <ChangeError error={error() ?? items.error} />
      <Show
        when={settled(items)?.length}
        fallback={settled(items) ? <p>{m.account_admin_mapping_none()}</p> : undefined}
      >
        <ul class={styles.list}>
          <For each={settled(items)}>
            {(item) => (
              <li class={styles.row}>
                <span class={styles.rowLabel}>
                  {m.account_admin_mapping_summary({
                    provider: isolate(providerName(item.provider_id)),
                    group: item.group,
                    library: isolate(libraryName(item.library_id)),
                    role: roleName(item.role as AccountLibrary["role"]),
                  })}
                </span>
                <Button
                  size="sm"
                  variant="outline"
                  aria-label={m.account_admin_mapping_remove_named({
                    group: item.group,
                    library: libraryName(item.library_id),
                  })}
                  onClick={() => void remove(item.id)}
                >
                  {m.account_admin_mapping_remove()}
                </Button>
              </li>
            )}
          </For>
        </ul>
      </Show>
      <form class={styles.form} onSubmit={(event) => void create(event)}>
        <FormField label={m.account_admin_mapping_provider()} required>
          <Select
            required
            placeholder={m.account_admin_mapping_choose_provider()}
            value={provider()}
            onValueChange={setProvider}
            items={props.providers.map((item) => ({ value: item.id, label: item.display_name }))}
          />
        </FormField>
        <FormField
          label={m.account_admin_mapping_group()}
          hint={m.account_admin_mapping_group_hint()}
          required
        >
          <Input
            required
            maxLength={4096}
            value={group()}
            onInput={(event) => setGroup(event.currentTarget.value)}
          />
        </FormField>
        <FormField label={m.account_admin_mapping_library()} required>
          <Select
            required
            placeholder={m.account_admin_mapping_choose_library()}
            value={library()}
            onValueChange={setLibrary}
            items={libraries().map((item) => ({ value: item.id, label: item.name }))}
          />
        </FormField>
        <FormField label={m.account_admin_mapping_role()}>
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
        <Button type="submit" disabled={busy()}>
          {m.account_admin_mapping_add()}
        </Button>
      </form>
    </section>
  )
}

function ProviderDependencies(props: { providers: Provider[] }) {
  return (
    <section class={styles.section}>
      <h2>{m.account_admin_dependencies_heading()}</h2>
      <ul class={styles.list}>
        <For each={props.providers}>{(provider) => <Dependency provider={provider} />}</For>
      </ul>
    </section>
  )
}

function Dependency(props: { provider: Provider }) {
  const [users] = createResource(
    () => props.provider.id,
    async (id) =>
      (
        await accountRequest<{ users: { user_id: string; display_name: string }[] }>(
          `/account/providers/${encodeURIComponent(id)}/dependencies`,
        )
      ).users,
  )
  return (
    <li class={styles.card}>
      <h3>{props.provider.display_name}</h3>
      <Show when={users.error}>
        <Callout tone="danger">{(users.error as Error | undefined)?.message}</Callout>
      </Show>
      <Show
        when={settled(users)?.length}
        fallback={
          <Show when={settled(users)}>
            <p class={styles.muted}>{m.account_admin_dependencies_safe()}</p>
          </Show>
        }
      >
        <Callout tone="warning">
          {m.account_admin_dependencies_users({
            names: isolate((settled(users) ?? []).map((user) => user.display_name).join(", ")),
          })}
        </Callout>
      </Show>
    </li>
  )
}
