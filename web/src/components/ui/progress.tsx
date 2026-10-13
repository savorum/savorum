import { Progress as ArkProgress } from "@ark-ui/solid/progress"
import { type JSX, Show } from "solid-js"
import { createSlotRecipeContext } from "styled-system/jsx"
import { progress } from "styled-system/recipes"

const { withProvider, withContext } = createSlotRecipeContext(progress)
const Root = withProvider(ArkProgress.Root, "root")
const Label = withContext(ArkProgress.Label, "label")
const ValueText = withContext(ArkProgress.ValueText, "valueText")
const Track = withContext(ArkProgress.Track, "track")
const Range = withContext(ArkProgress.Range, "range")

export interface ProgressProps {
  /** Names the work. It is the bar's accessible name. */
  label: string
  /**
   * Progress value between 0 and max, or null when indeterminate.
   *
   * Null renders an indeterminate progress bar in Ark and omits `aria-valuenow`.
   */
  value: number | null
  max?: number
  /** Shown beside the label, in the reader's terms rather than a percentage. */
  valueText?: JSX.Element
  size?: "sm" | "md" | "lg"
  /** Whether work is under way; a stopped bar rests rather than animating. */
  running?: boolean
}

/** A labelled progress bar. */
export const Progress = (props: ProgressProps) => (
  <Root size={props.size} value={props.value} max={props.max ?? 100}>
    <Label>{props.label}</Label>
    <Show when={props.valueText}>
      <ValueText>{props.valueText}</ValueText>
    </Show>
    {/* Ark names the progressbar from its value text ("0%") unless labelled here. */}
    <Track aria-label={props.label}>
      <Range data-running={props.running ? "" : undefined} />
    </Track>
  </Root>
)
