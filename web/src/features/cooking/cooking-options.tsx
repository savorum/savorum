import { RotateCcw, SlidersHorizontal } from "lucide-solid"
import { Show, createSignal } from "solid-js"
import { css } from "styled-system/css"
import { recipePage } from "styled-system/recipes"

import type { Recipe } from "@/api/client"
import { RecipeScaleControls } from "@/components/recipe-article/scale-controls"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Popover } from "@/components/ui/popover"
import type { Rational } from "@/lib/rational"

import { m } from "../../lib/messages.ts"

const styles = {
  panel: css({ display: "grid", gap: "4", marginTop: "3", maxWidth: "28rem", minWidth: "16rem" }),
  group: css({
    borderTopColor: "border.default",
    borderTopWidth: "1px",
    display: "grid",
    gap: "2",
    paddingTop: "3",
  }),
  groupTitle: css({ color: "fg.muted", fontSize: "xs", fontWeight: "semibold", margin: "0" }),
  check: css({
    alignItems: "center",
    display: "flex",
    fontSize: "sm",
    gap: "3",
    lineHeight: "1.5",
    minHeight: "2.75rem",
  }),
  note: css({ color: "fg.muted", fontSize: "xs", lineHeight: "1.5", margin: "0" }),
  restart: css({ justifySelf: "start" }),
}

/**
 * Settings a cook decides once: batch, measures, keeping the screen lit, and
 * restarting the session, in a non-modal panel.
 */
export function CookingOptions(props: {
  recipe: Recipe
  scale: Rational
  onScaleChange: (scale: Rational) => void
  keepAwake: boolean
  onKeepAwake: (enabled: boolean) => void
  wakeMessage: string
  onStartOver: () => void
}) {
  const layout = recipePage()
  const [open, setOpen] = createSignal(false)
  return (
    <Popover
      title={m.cooking_options_cooking_options()}
      open={open()}
      onOpenChange={(details) => setOpen(details.open)}
      positioning={{ placement: "bottom-end", gutter: 8 }}
      trigger={(triggerProps) => (
        <Button {...triggerProps()} variant="outline" size="sm">
          <SlidersHorizontal aria-hidden="true" size={15} />
          {m.cooking_options_cooking_options()}
        </Button>
      )}
    >
      <div class={styles.panel}>
        <RecipeScaleControls
          class={layout.controls}
          recipe={props.recipe}
          scale={props.scale}
          onScaleChange={props.onScaleChange}
        />
        <div class={styles.group}>
          <h3 class={styles.groupTitle}>{m.cooking_options_while_you_cook()}</h3>
          <label class={styles.check}>
            <Checkbox
              checked={props.keepAwake}
              onChange={(event) => props.onKeepAwake(event.currentTarget.checked)}
            />
            <span>{m.cooking_options_keep_screen_awake_while_cooking()}</span>
          </label>
          <Show when={props.wakeMessage}>
            <p class={styles.note}>{props.wakeMessage}</p>
          </Show>
        </div>
        {/* Starting over changes the page behind the panel, so the panel gets
            out of the way rather than leaving a cook to dismiss it. */}
        <div class={styles.group}>
          <Button
            class={styles.restart}
            variant="outline"
            size="sm"
            onClick={() => {
              setOpen(false)
              props.onStartOver()
            }}
          >
            <RotateCcw aria-hidden="true" size={15} />
            {m.cooking_options_start_over()}
          </Button>
        </div>
      </div>
    </Popover>
  )
}
