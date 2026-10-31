import { useFieldContext } from "@ark-ui/solid/field"
import { LocaleProvider } from "@ark-ui/solid/locale"
import {
  createListCollection,
  type ListCollection,
  Select as ArkSelect,
} from "@ark-ui/solid/select"
import { Check, ChevronDown } from "lucide-solid"
import {
  type Component,
  type ComponentProps,
  createMemo,
  createUniqueId,
  For,
  type JSX,
  Show,
} from "solid-js"
import { css } from "styled-system/css"
import { createSlotRecipeContext } from "styled-system/jsx"
import { select } from "styled-system/recipes"

import { interfaceAttributes } from "@/lib/i18n"

import { PresencePortal } from "./presence-portal"

const { withRootProvider, withContext } = createSlotRecipeContext(select)

/** A name that belongs to the control rather than to the screen. */
const visuallyHidden = css({ srOnly: true })

/**
 * A chooser that reads as a line of text: its name and its value side by side,
 * without a box, for a setting that is rarely changed.
 */
const quietRoot = css({
  alignItems: "center",
  display: "inline-flex",
  flexDirection: "row",
  flexWrap: "wrap",
  gap: "1.5",
  maxWidth: "100%",
  width: "auto",
  "& [data-part=label]": { color: "fg.muted", fontSize: "xs", fontWeight: "normal" },
  "& [data-part=control]": { maxWidth: "100%", width: "auto" },
  "& [data-part=trigger]": {
    background: "transparent",
    borderColor: "transparent",
    borderRadius: "l1",
    color: "fg.muted",
    fontSize: "xs",
    gap: "1",
    height: "auto",
    minHeight: "1.75rem",
    maxWidth: "100%",
    paddingBlock: "0.5",
    paddingInline: "1.5",
    width: "auto",
    _hover: { background: "bg.muted", color: "fg.default" },
  },
})

const StyledRoot = withRootProvider(ArkSelect.Root, {
  defaultProps: () => ({
    lazyMount: true,
    unmountOnExit: true,
    positioning: { sameWidth: false, fitViewport: true },
  }),
})

/**
 * Ark's root is generic over the item type. The style context that wraps it is
 * not, so it widens the collection to `unknown` and every caller would have to
 * assert its way back. The item type is restored once, here.
 */
const Root = StyledRoot as unknown as Component<
  Omit<ComponentProps<typeof StyledRoot>, "collection"> & {
    collection: ListCollection<SelectOption>
  }
>

const Label = withContext(ArkSelect.Label, "label")
const Control = withContext(ArkSelect.Control, "control")
const Trigger = withContext(ArkSelect.Trigger, "trigger")
const ValueText = withContext(ArkSelect.ValueText, "valueText")
const Indicator = withContext(ArkSelect.Indicator, "indicator")
const Positioner = withContext(ArkSelect.Positioner, "positioner")
const Content = withContext(ArkSelect.Content, "content")
const List = withContext(ArkSelect.List, "list")
const Item = withContext(ArkSelect.Item, "item")
const ItemText = withContext(ArkSelect.ItemText, "itemText")
const ItemIndicator = withContext(ArkSelect.ItemIndicator, "itemIndicator")
const ItemGroup = withContext(ArkSelect.ItemGroup, "itemGroup")
const ItemGroupLabel = withContext(ArkSelect.ItemGroupLabel, "itemGroupLabel")

/** One choice. `label` is what a reader sees; `value` is what gets stored. */
export interface SelectOption {
  value: string
  label: string
  disabled?: boolean
}

/** A named group of options. */
export interface SelectGroup {
  label: string
  options: SelectOption[]
}

/**
 * What a chooser offers: options, named groups of options, or both, so a list
 * can open with "No measure" and then name its volumes, weights and counts.
 */
export type SelectItems = readonly (SelectOption | SelectGroup)[]

function isGroup(entry: SelectOption | SelectGroup): entry is SelectGroup {
  return "options" in entry
}

function flatten(items: SelectItems): SelectOption[] {
  return items.flatMap((entry) => (isGroup(entry) ? [...entry.options] : [entry]))
}

const Choice = (props: { option: SelectOption }) => (
  <Item item={props.option}>
    <ItemText>{props.option.label}</ItemText>
    <ItemIndicator>
      <Check aria-hidden="true" size={16} />
    </ItemIndicator>
  </Item>
)

const Entry = (props: { entry: SelectOption | SelectGroup }) => (
  <Show
    when={isGroup(props.entry) ? props.entry : undefined}
    fallback={<Choice option={props.entry as SelectOption} />}
  >
    {(group) => (
      <ItemGroup>
        <ItemGroupLabel>{group().label}</ItemGroupLabel>
        <For each={group().options}>{(option) => <Choice option={option} />}</For>
      </ItemGroup>
    )}
  </Show>
)

export interface SelectProps {
  /** The name of the control, written above it. */
  label?: string
  /**
   * The name of the control where it is already clear on screen. It takes
   * precedence over the field around the control.
   */
  "aria-label"?: string
  value: string
  onValueChange: (value: string) => void
  items: SelectItems
  /**
   * What the trigger reads while nothing is chosen. A value with no matching
   * option, usually `""`, counts as nothing chosen.
   */
  placeholder?: string
  disabled?: boolean
  invalid?: boolean
  required?: boolean
  size?: "sm" | "md" | "lg"
  /** Reads as a line of text rather than a boxed field. */
  quiet?: boolean
  /** Present the chooser to a submitted form as a native control of this name. */
  name?: string
  /** Why the control is unavailable, for a chooser that is currently disabled. */
  title?: string
  /** Marks the trigger as the control an editor row returns focus to. */
  "data-focus"?: string
  class?: string
  children?: JSX.Element
}

/**
 * A chooser drawn by the application rather than the native dropdown. Ark
 * works in arrays of values; callers here work in one string.
 */
export const Select = (props: SelectProps) => {
  // Memoized: a fresh collection per read is a changed prop, so Ark re-renders
  // without settling.
  const collection = createMemo(() => createListCollection({ items: flatten(props.items) }))
  // The field's control id may belong to another input it wraps, so the chooser
  // takes the field's name but keeps its own id.
  const field = useFieldContext()
  const hiddenSelectID = createUniqueId()
  // Ark otherwise labels every chooser in a field with the field's label.
  const named = () => props.label ?? props["aria-label"]
  return (
    <LocaleProvider locale={interfaceAttributes().lang}>
      <Root
        class={
          [props.class, props.quiet ? quietRoot : undefined].filter(Boolean).join(" ") || undefined
        }
        size={props.size}
        ids={{
          label: named() ? undefined : field?.().ids.label,
          hiddenSelect: hiddenSelectID,
        }}
        collection={collection()}
        value={[props.value]}
        onValueChange={(details) => props.onValueChange(details.value[0] ?? "")}
        disabled={props.disabled}
        invalid={props.invalid}
        required={props.required}
        name={props.name}
      >
        <Show when={named()}>
          {(name) => <Label class={props.label ? undefined : visuallyHidden}>{name()}</Label>}
        </Show>
        <Show when={props.name}>
          <ArkSelect.HiddenSelect />
        </Show>
        <Control>
          <Trigger title={props.title} data-focus={props["data-focus"]}>
            <ValueText placeholder={props.placeholder} />
            <Indicator>
              <ChevronDown aria-hidden="true" size={16} />
            </Indicator>
          </Trigger>
        </Control>
        <PresencePortal>
          <Positioner>
            <Content>
              <List>
                <For each={props.items}>{(entry) => <Entry entry={entry} />}</For>
              </List>
            </Content>
          </Positioner>
        </PresencePortal>
      </Root>
    </LocaleProvider>
  )
}
