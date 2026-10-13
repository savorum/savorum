import { Collapsible } from "@ark-ui/solid/collapsible"
import { ChevronRight } from "lucide-solid"
import type { JSX } from "solid-js"
import { createSlotRecipeContext } from "styled-system/jsx"
import { collapsible } from "styled-system/recipes"

const { withProvider, withContext } = createSlotRecipeContext(collapsible)

const Root = withProvider(Collapsible.Root, "root")
const Trigger = withContext(Collapsible.Trigger, "trigger")
const Indicator = withContext(Collapsible.Indicator, "indicator")
const Content = withContext(Collapsible.Content, "content")

export interface DisclosureProps {
  class?: string
  variant?: "inline"
  label: string
  hint?: string
  /** Open from the start, for a section that already has something in it. */
  open?: boolean
  expanded?: boolean
  onExpandedChange?: (open: boolean) => void
  children: JSX.Element
}

/**
 * Keeps uncommon fields out of the way until they are wanted. Disclosures do
 * not nest; nested editing belongs in a dialog.
 */
export const Disclosure = (props: DisclosureProps) => {
  const styles = () => collapsible({ variant: props.variant })
  // The contents stay in the document while shut, for find-in-page.
  return (
    <Root
      class={props.class}
      variant={props.variant}
      defaultOpen={props.open}
      open={props.expanded}
      onOpenChange={(details) => props.onExpandedChange?.(details.open)}
      lazyMount={false}
      unmountOnExit={false}
    >
      <Trigger>
        <Indicator>
          <ChevronRight aria-hidden="true" size={16} />
        </Indicator>
        <span class={styles().label}>{props.label}</span>
        {props.hint ? <span class={styles().hint}>{props.hint}</span> : null}
      </Trigger>
      <Content>
        <div>{props.children}</div>
      </Content>
    </Root>
  )
}
