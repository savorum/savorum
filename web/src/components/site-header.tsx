import { Link, useLocation, useNavigate } from "@tanstack/solid-router"
import { MoreHorizontal } from "lucide-solid"
import { createSignal, For, Show } from "solid-js"
import { css } from "styled-system/css"

import { Problem } from "@/components/status"
import { IconButton } from "@/components/ui/icon-button"
import { Menu, MenuItem, MenuItemGroup, MenuItemGroupLabel } from "@/components/ui/menu"
import { Select } from "@/components/ui/select"
import { account, accountRequest, administers } from "@/features/account/account-state"
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
    display: "flex",
    flex: "1",
    fontSize: { base: "sm", md: "md" },
    lineHeight: "1",
    minHeight: "8",
    gap: { base: "2", md: "5" },
  }),
  actions: css({
    display: "flex",
    alignItems: "center",
    gap: "2",
    minHeight: "8",
  }),
  library: css({ minWidth: "12rem" }),
  link: css({
    display: "inline-flex",
    alignItems: "center",
    minHeight: "8",
    borderRadius: "l1",
    paddingBlock: "1",
    textDecoration: "none",
    _hover: { color: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "3px" },
    "&[data-status=active]": { color: "fg.default", fontWeight: "medium" },
  }),
}

/** The three independent stages of the everyday cooking workflow. */
const destinations = [
  {
    to: "/",
    get label() {
      return m.site_header_recipes()
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
  const navigate = useNavigate()
  const location = useLocation()
  const [switchFailed, setSwitchFailed] = createSignal<string>()
  const libraries = () => account()?.libraries ?? []
  return (
    <header class={styles.banner}>
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
            onSelect={(details) => void navigate({ to: details.value })}
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
              <MenuItem value="/preferences">{m.site_header_preferences()}</MenuItem>
              <MenuItem value="/account">{m.account_nav_account_menu()}</MenuItem>
              <Show when={administers()}>
                <MenuItem value="/admin">{m.account_admin_nav_label()}</MenuItem>
              </Show>
            </MenuItemGroup>
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
