import { Link as RouterLink } from "@tanstack/solid-router"
import { ChevronDown, FileUp, History, Link, NotepadText, Plus, Upload } from "lucide-solid"
import { For, Show } from "solid-js"

import { Button } from "@/components/ui/button"
import {
  Menu,
  MenuIndicator,
  MenuItem,
  MenuItemDescription,
  MenuItemGroup,
  MenuItemGroupLabel,
  MenuItemMedia,
  MenuItemText,
  MenuSeparator,
} from "@/components/ui/menu"
import { shortcutAria } from "@/lib/keyboard-shortcuts"

import { capabilities } from "../api/capabilities"
import { m } from "../lib/messages.ts"

/** The ways a recipe gets into the library, each with a name, a description, and a picture. */
const ways = [
  {
    value: "files",
    icon: FileUp,
    get label() {
      return m.import_menu_from_a_file()
    },
    get detail() {
      const extensions = capabilities()?.importers?.flatMap((format) => format.extensions ?? [])
      return extensions?.includes(".pdf") && extensions.includes(".png")
        ? m.import_menu_recipe_collections_pdfs_and_photos()
        : undefined
    },
  },
  {
    value: "url",
    icon: Link,
    get label() {
      return m.import_menu_from_a_website()
    },
    get detail() {
      return m.import_menu_a_recipe_you_found_online()
    },
  },
  {
    value: "text",
    icon: NotepadText,
    get label() {
      return m.import_menu_from_text()
    },
    get detail() {
      return m.import_menu_a_copied_or_written_recipe()
    },
  },
] as const

export const ImportMenu = (props: {
  busy: boolean
  write?: boolean
  /** The trigger's accessible name, where a page offers this menu twice. */
  label?: string
  /**
   * Renders the trigger as the disclosure arrow alone, for a group where the
   * button beside it already carries the words. The accessible name stays.
   */
  compact?: boolean
  variant?: "solid" | "outline"
  class?: string
  onFiles: () => void
  onURL: () => void
  onText: () => void
  onHistory?: () => void
}) => (
  <Menu
    positioning={{ placement: "bottom-end", gutter: 8 }}
    onSelect={({ value }) => {
      if (value === "files") props.onFiles()
      if (value === "url") props.onURL()
      if (value === "text") props.onText()
      if (value === "history") props.onHistory?.()
    }}
    trigger={(triggerProps) => (
      <Button
        {...triggerProps()}
        data-shortcut={props.write ? "add-recipes" : "import-recipes"}
        aria-keyshortcuts={shortcutAria(props.write ? "add-recipes" : "import-recipes")}
        class={props.class}
        variant={props.variant ?? "outline"}
        colorPalette={props.variant === "solid" ? "accent" : undefined}
        size="sm"
        px={props.compact ? "0" : undefined}
        h="auto"
        minH="9"
        maxW="100%"
        whiteSpace="normal"
        loading={props.busy}
        aria-label={
          props.write
            ? m.library_page_add_recipes()
            : (props.label ?? m.import_menu_import_recipes())
        }
      >
        <Show
          when={props.compact && !props.write}
          fallback={
            <>
              <Show when={props.write} fallback={<Upload aria-hidden="true" size={16} />}>
                <Plus aria-hidden="true" size={16} />
              </Show>
              <span>{props.write ? m.library_page_add_recipes() : m.import_menu_import()}</span>
              <MenuIndicator>
                <ChevronDown aria-hidden="true" size={14} />
              </MenuIndicator>
            </>
          }
        >
          <MenuIndicator>
            <ChevronDown aria-hidden="true" size={16} />
          </MenuIndicator>
        </Show>
      </Button>
    )}
  >
    <Show when={props.write}>
      <MenuItem
        value="new"
        asChild={(itemProps) => (
          <RouterLink {...itemProps()} to="/recipes/new">
            {m.library_page_new_recipe()}
          </RouterLink>
        )}
      />
      <MenuSeparator />
    </Show>
    <MenuItemGroup>
      <MenuItemGroupLabel>{m.import_menu_bring_recipes_in()}</MenuItemGroupLabel>
      <For each={ways}>
        {(way) => (
          <MenuItem value={way.value} valueText={way.label} aria-label={way.label}>
            <MenuItemMedia>
              <way.icon size={21} />
            </MenuItemMedia>
            <MenuItemText>
              {way.label}
              <Show when={way.detail}>
                <MenuItemDescription>{way.detail}</MenuItemDescription>
              </Show>
            </MenuItemText>
          </MenuItem>
        )}
      </For>
    </MenuItemGroup>
    <Show when={props.onHistory}>
      <MenuSeparator />
      <MenuItem value="history">
        <History aria-hidden="true" size={16} /> {m.import_menu_recent_imports()}
      </MenuItem>
    </Show>
  </Menu>
)
