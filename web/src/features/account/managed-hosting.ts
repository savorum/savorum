import { m } from "../../lib/messages.ts"
import { accountRequest } from "./account-state"

type Provider = {
  kind: string
  account_origin?: string
  tenant_id?: string
}

export type ManagedHosting = { origin: string; tenantID: string }

export async function readManagedHosting(): Promise<ManagedHosting | undefined> {
  const result = await accountRequest<{ providers: Provider[] }>("/auth/providers")
  const provider = result.providers.find((item) => item.kind === "managed")
  if (!provider) return undefined
  if (!provider.account_origin || !provider.tenant_id)
    throw new Error(m.account_state_the_request_could_not_be_completed())
  const origin = new URL(provider.account_origin)
  if (origin.protocol !== "https:" || origin.username || origin.password) {
    throw new Error(m.account_state_the_request_could_not_be_completed())
  }
  return { origin: origin.origin, tenantID: provider.tenant_id }
}

export function hostingAccountURL(hosting: ManagedHosting, libraryID?: string) {
  const target = new URL("/", hosting.origin)
  target.searchParams.set("tenant", hosting.tenantID)
  target.searchParams.set("sharing", "1")
  if (libraryID) target.searchParams.set("library", libraryID)
  return target.href
}
