import { m } from "../../lib/messages.ts"
import { accountStyles as styles } from "./account-layout"
import { AdminPage } from "./admin-page"
import { AuditView } from "./audit-view"

export function AdminAuditPage() {
  return (
    <AdminPage title={m.account_admin_audit_title()} lede={m.account_admin_audit_lede()}>
      <section class={styles.section}>
        <AuditView />
      </section>
    </AdminPage>
  )
}
