import { calendarDate } from "@/lib/date-format"
import { m } from "@/lib/messages"

export type AuditEvent = {
  id: number
  created_at: number
  actor_id: string
  action: string
  target_type: string
  target_id: string
  outcome: string
  library_id?: string
  credential_id?: string
  correlation_id?: string
}

const actions: Record<string, () => string> = {
  "auth.login.succeeded": () => m.account_audit_sign_in(),
  "auth.login.failed": () => m.account_audit_sign_in(),
  "auth.password.changed": () => m.account_audit_password_change(),
  "auth.passkey.created": () => m.book_history_created(),
  "auth.passkey.renamed": () => m.account_audit_renamed(),
  "auth.passkey.revoked": () => m.account_tokens_state_revoked(),
  "auth.recovery.regenerated": () => m.account_audit_recovery_replaced(),
  "auth.recovery.login": () => m.account_audit_recovery_sign_in(),
  "auth.session.revoked": () => m.account_sessions_revoked(),
  "auth.sessions.revoked": () => m.account_sessions_others_revoked(),
  "auth.provider.disabled": () => m.account_admin_person_disabled(),
  "user.bootstrapped": () => m.book_history_created(),
  "user.created": () => m.book_history_created(),
  "user.disabled": () => m.account_admin_person_disabled(),
  "user.enabled": () => m.account_audit_enabled(),
  "user.deleted": () => m.account_audit_deleted(),
  "library.created": () => m.book_history_created(),
  "library.deleted": () => m.account_audit_retired(),
  "library.member.role_changed": () => m.account_audit_access_changed(),
  "library.member.removed": () => m.account_audit_access_changed(),
  "library.ownership.recovered": () => m.account_admin_recovered(),
  "recipe.trashed": () => m.account_audit_trashed(),
  "recipe.restored": () => m.recipe_history_page_restored(),
  "recipe.merged": () => m.account_audit_merged(),
  "recipe.purged": () => m.account_audit_purged(),
  "book.trashed": () => m.account_audit_trashed(),
  "book.restored": () => m.recipe_history_page_restored(),
  "book.purged": () => m.account_audit_purged(),
  "import.completed": () => m.import_job_presentation_import_complete(),
  "settings.changed": () => m.recipe_history_page_changed(),
  "token.created": () => m.book_history_created(),
  "token.revoked": () => m.account_tokens_state_revoked(),
  "identity.link": () => m.account_audit_linked(),
  "identity.unlink": () => m.account_audit_unlinked(),
  "identity.force-unlink": () => m.account_audit_forced_unlink(),
  "provider.mapping.set": () => m.recipe_history_page_changed(),
  "provider.mapping.delete": () => m.account_audit_deleted(),
  "provider.membership.reconcile": () => m.account_audit_access_changed(),
  "invitation.create": () => m.book_history_created(),
  "invitation.revoke": () => m.account_invitation_state_revoked(),
  "invitation.accept": () => m.account_invitation_state_accepted(),
  "hosting-release": () => m.recipe_history_page_changed(),
  "hosting-member-removed": () => m.account_audit_access_changed(),
  "webhook.settings": () => m.integrations_saved(),
  "webhook.rotation": () => m.account_audit_secret_rotated(),
  "webhook.deleted": () => m.account_audit_deleted(),
  "webhook.redelivery-requested": () => m.account_audit_redelivery(),
  "webhook.test-requested": () => m.account_audit_test_requested(),
  "webhook.automatically-paused": () => m.account_audit_paused(),
}

/** Unknown actions remain readable as their exact code across server versions. */
export function auditActionLabel(action: string): string {
  return Object.hasOwn(actions, action) ? actions[action]!() : action
}

export function auditOutcome(outcome: string) {
  if (outcome === "success" || outcome === "succeeded")
    return { label: m.account_audit_succeeded(), tone: "success" as const }
  if (outcome === "failed") return { label: m.account_audit_failed(), tone: "failure" as const }
  return { label: outcome, tone: "neutral" as const }
}

export function auditActor(id: string, names: Record<string, string>): string {
  if (Object.hasOwn(names, id)) return names[id]!
  if (id === "system") return m.account_audit_system()
  if (id === "instance-owner") return m.account_audit_owner()
  if (id === "hosting") return m.account_audit_service()
  return compactAuditID(id)
}

export function compactAuditID(id: string): string {
  return id.length > 20 ? `${id.slice(0, 8)}…${id.slice(-6)}` : id
}

/** Adjacent days preserve the server's record order even after a clock change. */
export function groupAuditDays(events: AuditEvent[]) {
  const groups: { key: number; date: string; events: AuditEvent[] }[] = []
  for (const event of events) {
    const date = calendarDate(new Date(event.created_at * 1000))
    const previous = groups.at(-1)
    if (previous?.date === date) previous.events.push(event)
    else groups.push({ key: event.id, date, events: [event] })
  }
  return groups
}
