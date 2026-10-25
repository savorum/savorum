import { ChevronDown } from "lucide-solid"
import { For, Show } from "solid-js"
import { css } from "styled-system/css"

import { Menu, MenuRadioItem, MenuRadioItemGroup } from "@/components/ui/menu"
import { m } from "@/lib/messages"
import { rational } from "@/lib/rational"
import { defaultStyle, styleOf, unitGroups } from "@/lib/unit-choices"
import { convertToUnit, type MeasurementSystem } from "@/lib/units"

/** Unit changes affect this reading only; the recipe's authored quantities stay intact. */
export function IngredientAmount(props: {
  amount: string
  name: string
  sourceUnit?: string
  selectedUnit?: string
  system: MeasurementSystem
  onUnit: (unit: string | undefined) => void
}) {
  let triggerElement: HTMLButtonElement | undefined
  let restoreFocus = false
  const choices = () => {
    const source = props.sourceUnit
    if (!source) return []
    const style = props.system === "original" ? styleOf([source], defaultStyle()) : props.system
    return unitGroups(style, source)
      .flatMap((group) => group.choices)
      .filter((choice) => convertToUnit(rational(1n), source, choice.value))
      .map((choice) => ({
        ...choice,
        label:
          choice.value === source
            ? `${choice.label} · ${m.scale_controls_as_written()}`
            : choice.label,
      }))
  }
  return (
    <Show when={props.amount && choices().length > 1} fallback={props.amount}>
      <Menu
        positioning={{ placement: "bottom-start", gutter: 4 }}
        onSelect={() => {
          restoreFocus = true
        }}
        onExitComplete={() => {
          if (!restoreFocus) return
          restoreFocus = false
          if (!triggerElement) return
          const active = triggerElement.ownerDocument.activeElement
          const menu = active?.closest('[role="menu"]')
          if (
            active === triggerElement.ownerDocument.body ||
            menu?.getAttribute("aria-labelledby") === triggerElement.id
          )
            triggerElement.focus({ preventScroll: true })
        }}
        trigger={(trigger) => (
          <button
            {...trigger()}
            ref={(element) => {
              triggerElement = element
            }}
            type="button"
            class={styles.trigger}
            aria-label={`${props.name}: ${props.amount} · ${m.ingredient_unit()}`}
          >
            {props.amount}
            <ChevronDown aria-hidden="true" size={12} />
          </button>
        )}
      >
        <MenuRadioItemGroup
          value={props.selectedUnit ?? ""}
          onValueChange={({ value }) => props.onUnit(value || undefined)}
        >
          <MenuRadioItem value="">
            {
              {
                original: m.scale_controls_as_written(),
                metric: m.scale_controls_metric(),
                us: m.scale_controls_us(),
                au: m.scale_controls_australian(),
              }[props.system]
            }
          </MenuRadioItem>
          <For each={choices()}>
            {(choice) => <MenuRadioItem value={choice.value}>{choice.label}</MenuRadioItem>}
          </For>
        </MenuRadioItemGroup>
      </Menu>
    </Show>
  )
}

const styles = {
  trigger: css({
    alignItems: "baseline",
    background: "transparent",
    borderRadius: "l1",
    color: "inherit",
    cursor: "pointer",
    display: "inline-flex",
    font: "inherit",
    gap: "1",
    paddingBlock: "0.5",
    textAlign: "end",
    textDecoration: "underline",
    textDecorationColor: "border.default",
    textDecorationStyle: "dotted",
    textUnderlineOffset: "0.2em",
    "& svg": { alignSelf: "center", flexShrink: "0", color: "fg.muted" },
    "@media (pointer: coarse)": { minHeight: "11", minWidth: "11" },
    _hover: { color: "accent.default", textDecorationColor: "currentColor" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "2px" },
  }),
}
