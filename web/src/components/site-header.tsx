import { Link, useLocation, useNavigate } from "@tanstack/solid-router"
import { CircleHelp, Keyboard, MoreHorizontal } from "lucide-solid"
import { createSignal, For, Show } from "solid-js"
import { css } from "styled-system/css"

import { KeyboardShortcuts } from "@/components/keyboard-shortcuts"
import { Problem } from "@/components/status"
import { IconButton } from "@/components/ui/icon-button"
import {
  Menu,
  MenuItem,
  MenuItemGroup,
  MenuItemGroupLabel,
  MenuSeparator,
} from "@/components/ui/menu"
import { Select } from "@/components/ui/select"
import { account, accountRequest, administers, authEnabled } from "@/features/account/account-state"
import { SignOutDialog } from "@/features/account/sign-out-dialog"
import { ResumeCooking } from "@/features/cooking/resume-cooking"
import { TimerStatus } from "@/features/cooking/timer-status"
import { recipeEditorOpen } from "@/features/recipe-editing/editing-scope"

import { m } from "../lib/messages.ts"

const styles = {
  banner: css({
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
  }),
  header: css({
    alignItems: "center",
    display: "flex",
    gap: { base: "2", md: "6" },
    flexWrap: "wrap",
    marginInline: "auto",
    maxWidth: "92rem",
    minHeight: "3.75rem",
    paddingInline: { base: "3", md: "8", lg: "10" },
  }),
  problem: css({
    marginInline: "auto",
    maxWidth: "92rem",
    paddingInline: { base: "3", md: "8", lg: "10" },
  }),
  brand: css({
    display: "inline-flex",
    alignItems: "center",
    flexShrink: "0",
    "@media (pointer: coarse)": { minHeight: "11", minWidth: "11" },
    textDecoration: "none",
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "3px" },
  }),
  logo: css({
    display: "block",
    width: "32px",
    height: "32px",
    _dark: { display: "none" },
  }),
  logoDark: css({
    display: "none",
    width: "32px",
    height: "32px",
    _dark: { display: "block" },
  }),
  nav: css({
    alignItems: "center",
    color: "fg.muted",
    columnGap: { base: "2", md: "5" },
    display: "flex",
    flex: "1 0 auto",
    flexWrap: "wrap",
    fontSize: { base: "sm", md: "md" },
    lineHeight: "1",
    maxWidth: "100%",
    minHeight: "8",
    rowGap: "1",
  }),
  actions: css({
    alignItems: "center",
    display: "flex",
    gap: "2",
    maxWidth: "100%",
    minHeight: "8",
    minWidth: "0",
  }),
  library: css({ minWidth: "0", width: "min(12rem, 60vw)" }),
  link: css({
    display: "inline-flex",
    alignItems: "center",
    minHeight: "8",
    borderRadius: "l1",
    paddingBlock: "1",
    textDecoration: "none",
    _hover: { color: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "3px" },
    "@media (pointer: coarse)": { minHeight: "11" },
    "&[data-status=active], &[aria-current=true], &[aria-current=page]": {
      color: "fg.default",
      fontWeight: "medium",
      textDecorationColor: "accent.default",
      textDecorationLine: "underline",
      textDecorationThickness: "2px",
      textUnderlineOffset: "0.45em",
    },
  }),
}

const destinations = [
  {
    to: "/",
    within: ["/recipes", "/imports", "/tags", "/saved-searches", "/trash"],
    get label() {
      return m.site_header_recipes()
    },
  },
  {
    to: "/books",
    get label() {
      return m.site_header_books()
    },
  },
  {
    to: "/plan",
    get label() {
      return m.site_header_plan()
    },
  },
  {
    to: "/shopping",
    get label() {
      return m.shopping_title()
    },
  },
] as const

export function SiteHeader() {
  const [signingOut, setSigningOut] = createSignal(false)
  const [shortcutsOpen, setShortcutsOpen] = createSignal(false)
  const navigate = useNavigate()
  const location = useLocation()
  const [switchFailed, setSwitchFailed] = createSignal<string>()
  const libraries = () => account()?.libraries ?? []
  return (
    <header class={styles.banner}>
      <SignOutDialog open={signingOut()} onClose={() => setSigningOut(false)} />
      <KeyboardShortcuts
        open={shortcutsOpen()}
        onOpen={() => setShortcutsOpen(true)}
        onClose={() => setShortcutsOpen(false)}
      />
      <div class={styles.header}>
        <Link class={styles.brand} to="/" aria-label={m.site_header_savorum()}>
          <img class={styles.logo} src="/brand/icon-mono.svg" width="32" height="32" alt="" />
          <img class={styles.logoDark} src="/brand/icon-dark.svg" width="32" height="32" alt="" />
        </Link>
        <nav class={styles.nav} aria-label={m.site_header_primary()}>
          <For each={destinations}>
            {(destination) => (
              <Link
                class={styles.link}
                to={destination.to}
                activeOptions={{ exact: destination.to === "/" }}
                aria-current={
                  "within" in destination &&
                  destination.within.some((path) => location().pathname.startsWith(path))
                    ? "true"
                    : undefined
                }
              >
                {destination.label}
              </Link>
            )}
          </For>
        </nav>
        <div class={styles.actions}>
          <Show when={libraries().length > 1}>
            <Select
              class={styles.library}
              size="sm"
              aria-label={m.site_header_active_library()}
              value={account()?.active_library_id ?? ""}
              disabled={recipeEditorOpen()}
              title={recipeEditorOpen() ? m.editor_finish_before_switching() : undefined}
              items={libraries().map((library) => ({ value: library.id, label: library.name }))}
              onValueChange={(libraryID) => {
                setSwitchFailed(undefined)
                void accountRequest("/account/library", "POST", { library_id: libraryID })
                  .then(() => window.location.assign("/"))
                  .catch((error: Error) => setSwitchFailed(error.message))
              }}
            />
          </Show>
          <TimerStatus />
          <Menu
            onSelect={(details) => {
              if (details.value === "sign-out") setSigningOut(true)
              else if (details.value === "shortcuts") setShortcutsOpen(true)
              else if (details.value === "help")
                window.open("https://docs.savorum.app/", "_blank", "noopener")
              else void navigate({ to: details.value })
            }}
            trigger={(triggerProps) => (
              <IconButton
                {...triggerProps()}
                aria-label={m.site_header_more()}
                variant="plain"
                size="sm"
              >
                <MoreHorizontal aria-hidden="true" size={18} />
              </IconButton>
            )}
          >
            <MenuItemGroup>
              <MenuItemGroupLabel>{m.site_header_your_library()}</MenuItemGroupLabel>
              <MenuItem value="/imports">{m.site_header_imports()}</MenuItem>
              <MenuItem value="/tags">{m.site_header_tags_and_categories()}</MenuItem>
              <MenuItem value="/saved-searches">{m.site_header_saved_searches()}</MenuItem>
              <MenuItem value="/trash">{m.site_header_trash()}</MenuItem>
            </MenuItemGroup>
            <MenuItemGroup>
              <MenuItemGroupLabel>{m.site_header_you()}</MenuItemGroupLabel>
              <MenuItem value="/account">{m.account_nav_account_menu()}</MenuItem>
              <MenuItem value="/preferences">{m.site_header_preferences()}</MenuItem>
              <MenuItem value="/account/integrations">{m.integrations_title()}</MenuItem>
              <Show when={administers()}>
                <MenuItem value="/admin">{m.account_admin_nav_label()}</MenuItem>
              </Show>
            </MenuItemGroup>
            <MenuSeparator />
            <MenuItem value="shortcuts">
              <Keyboard aria-hidden="true" size={16} />
              {m.keyboard_shortcuts_title()}
            </MenuItem>
            <MenuItem value="help">
              <CircleHelp aria-hidden="true" size={16} />
              {m.site_header_help()}
            </MenuItem>
            <Show when={authEnabled() && account()?.user}>
              <MenuSeparator />
              <MenuItem value="sign-out">{m.account_overview_sign_out()}</MenuItem>
            </Show>
          </Menu>
        </div>
      </div>
      <Show when={switchFailed()}>
        <div class={styles.problem}>
          <Problem message={switchFailed()} />
        </div>
      </Show>
      <Show when={!location().pathname.endsWith("/cook")}>
        <ResumeCooking />
      </Show>
    </header>
  )
}
