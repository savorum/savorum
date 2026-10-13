import { RatingGroup as ArkRating } from "@ark-ui/solid/rating-group"
import { Star } from "lucide-solid"
import { For, Show, createSignal } from "solid-js"
import { createSlotRecipeContext } from "styled-system/jsx"
import { mark, ratingGroup } from "styled-system/recipes"

import { m } from "../../lib/messages.ts"
import { Button } from "./button"

const { withProvider, withContext } = createSlotRecipeContext(ratingGroup)
const Root = withProvider(ArkRating.Root, "root")
const Control = withContext(ArkRating.Control, "control")
const Label = withContext(ArkRating.Label, "label")
const Item = withContext(ArkRating.Item, "item")

export interface RatingProps {
  label: string
  value?: number
  size?: number
  disabled?: boolean
  clearable?: boolean
  onChange: (rating: number | undefined) => void
}

// Park UI's Ark/Panda composition provides roving focus and keyboard selection.
// Filled ink fades within one warm color instead of flashing to a different hue.
export const Rating = (props: RatingProps) => {
  const [preview, setPreview] = createSignal<number>()
  return (
    <Root
      count={5}
      value={props.value ?? 0}
      disabled={props.disabled}
      onValueChange={(event) => props.onChange(event.value > 0 ? event.value : undefined)}
      translations={{ ratingValueText: (value) => `${value} of 5` }}
    >
      <Label>{props.label}</Label>
      <Control onPointerLeave={() => setPreview(undefined)} onKeyDown={() => setPreview(undefined)}>
        <For each={[1, 2, 3, 4, 5]}>
          {(value) => (
            <Item
              index={value}
              aria-checked={props.value === value}
              class={mark()}
              data-active={value <= (preview() ?? props.value ?? 0) ? "" : undefined}
              onPointerEnter={(event) =>
                event.pointerType !== "touch" && !props.disabled && setPreview(value)
              }
            >
              <Star aria-hidden="true" size={props.size ?? 20} />
            </Item>
          )}
        </For>
      </Control>
      <ArkRating.HiddenInput />
      <Show when={props.clearable && props.value !== undefined && !props.disabled}>
        <Button size="xs" variant="plain" onClick={() => props.onChange(undefined)}>
          {m.rating_clear()}
        </Button>
      </Show>
    </Root>
  )
}
