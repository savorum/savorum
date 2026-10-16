import { Link } from "@tanstack/solid-router"
import { type ComponentProps, For, type JSX, Show } from "solid-js"
import { css } from "styled-system/css"

import { m } from "../../lib/messages.ts"
import { administers, authEnabled } from "./account-state"

export const accountStyles = {
  page: css({
    display: "grid",
    gap: "8",
    marginInline: "auto",
    maxWidth: "48rem",
    minWidth: "0",
    padding: "6",
  }),
  heading: css({ fontFamily: "editorial", fontSize: "3xl", lineHeight: "1.2" }),
  lede: css({ color: "fg.muted", maxWidth: "34rem" }),
  header: css({ display: "grid", gap: "2", minWidth: "0" }),
  nav: css({
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "flex",
    flexWrap: "wrap",
    gap: "5",
    paddingBottom: "3",
  }),
  navLink: css({
    borderRadius: "l1",
    color: "fg.muted",
    fontSize: "sm",
    paddingBlock: "1",
    textDecoration: "none",
    _hover: { color: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "3px" },
    "&[data-status=active]": { color: "fg.default", fontWeight: "medium" },
  }),
  back: css({
    color: "fg.muted",
    fontSize: "sm",
    textDecoration: "underline",
    textUnderlineOffset: "3px",
    width: "fit-content",
    _hover: { color: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "3px" },
  }),
  section: css({
    display: "grid",
    gap: "4",
    minWidth: "0",
    overflowWrap: "anywhere",
    "& > h2": { fontFamily: "editorial", fontSize: "xl", lineHeight: "1.3" },
    "& > h3": { fontSize: "md", fontWeight: "semibold", lineHeight: "1.4" },
    "&:not(:first-of-type)": {
      borderTopWidth: "1px",
      borderColor: "border.default",
      paddingTop: "8",
    },
  }),
  card: css({
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "3",
    minWidth: "0",
    overflowWrap: "anywhere",
    padding: "4",
    // The reset gives text elements break-word, which does not shrink their
    // intrinsic width; an identifier has to be allowed to break anywhere.
    "& h3, & h4, & p, & span, & time": { overflowWrap: "anywhere" },
    "& > h3": { fontSize: "md", fontWeight: "semibold" },
  }),
  list: css({
    display: "grid",
    gap: "3",
    listStyle: "none",
    margin: "0",
    minWidth: "0",
    padding: "0",
  }),
  form: css({
    display: "grid",
    gap: "3",
    minWidth: "0",
    "& > button": { justifySelf: "start" },
  }),
  row: css({ display: "flex", flexWrap: "wrap", gap: "3", alignItems: "center" }),
  rowLabel: css({ flex: "1 1 8rem", minWidth: "0", overflowWrap: "anywhere" }),
  actions: css({ display: "flex", flexWrap: "wrap", gap: "2", alignItems: "center" }),
  // A chooser standing in a row of its own rather than filling a form field.
  chooser: css({ width: "auto", minWidth: "9rem" }),
  muted: css({ color: "fg.muted", fontSize: "sm", overflowWrap: "anywhere" }),
  codes: css({
    background: "bg.muted",
    borderRadius: "l2",
    fontFamily: "mono",
    margin: "0",
    padding: "4",
    whiteSpace: "pre-wrap",
  }),
}

export interface ScopePageProps {
  title: string
  description?: JSX.Element
  children: JSX.Element
}

/** One scope, one page: a title, what it covers, and the tasks it owns. */
export const ScopePage = (props: ScopePageProps) => (
  <main class={accountStyles.page}>
    <div class={accountStyles.header}>
      <h1 class={accountStyles.heading}>{props.title}</h1>
      <Show when={props.description}>
        <p class={accountStyles.lede}>{props.description}</p>
      </Show>
    </div>
    {props.children}
  </main>
)

interface Destination {
  to: ComponentProps<typeof Link>["to"]
  label: string
}

const ScopeNav = (props: { label: string; items: Destination[] }) => (
  <Show when={props.items.length > 1}>
    <nav class={accountStyles.nav} aria-label={props.label}>
      <For each={props.items}>
        {(item) => (
          <Link class={accountStyles.navLink} to={item.to} activeOptions={{ exact: true }}>
            {item.label}
          </Link>
        )}
      </For>
    </nav>
  </Show>
)

/** Your own account and its sign-in security, without anyone else's access. */
export const AccountNav = () => (
  <ScopeNav
    label={m.account_nav_label()}
    items={[
      { to: "/account", label: m.account_nav_account() },
      { to: "/account/integrations", label: m.integrations_title() },
      ...(authEnabled()
        ? [
            { to: "/account/security", label: m.account_nav_security() },
            { to: "/account/tokens", label: m.account_nav_tokens() },
          ]
        : []),
    ]}
  />
)

/** Instance-wide administration, reached only through its own navigation. */
export const AdministrationNav = () => (
  <Show when={administers()}>
    <ScopeNav
      label={m.account_admin_nav_label()}
      items={[
        { to: "/admin", label: m.account_admin_nav_people() },
        { to: "/admin/libraries", label: m.account_admin_nav_libraries() },
        { to: "/admin/providers", label: m.account_admin_nav_providers() },
        { to: "/admin/connect", label: m.connect_title() },
        { to: "/admin/audit", label: m.account_admin_nav_audit() },
      ]}
    />
  </Show>
)
