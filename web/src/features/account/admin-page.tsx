import { Show, type JSX } from "solid-js"

import { m } from "../../lib/messages.ts"
import { AdministrationNav, accountStyles as styles } from "./account-layout"
import { administers } from "./account-state"

export interface AdminPageProps {
  title: string
  lede: string
  children: JSX.Element
}

/**
 * Administration is reached only through its own navigation, and the hidden
 * navigation is never the check: every route below is enforced by the server.
 */
export const AdminPage = (props: AdminPageProps) => (
  <main class={styles.page}>
    <div class={styles.header}>
      <h1 class={styles.heading}>{props.title}</h1>
      <p class={styles.lede}>{props.lede}</p>
    </div>
    <AdministrationNav />
    <Show
      when={administers()}
      fallback={<p class={styles.section}>{m.account_admin_unavailable()}</p>}
    >
      {props.children}
    </Show>
  </main>
)
