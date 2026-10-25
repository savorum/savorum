import { ArrowLeft, Check, ChevronLeft } from "lucide-solid"
import {
  type JSX,
  Show,
  createEffect,
  createMemo,
  createSignal,
  onCleanup,
  onMount,
} from "solid-js"
import { css } from "styled-system/css"

import type { Recipe } from "@/api/client"
import { Callout } from "@/components/status"
import { Button } from "@/components/ui/button"
import { directionalIcon } from "@/components/ui/directional"
import { IconButton } from "@/components/ui/icon-button"
import { CookingIngredients } from "@/features/cooking/cooking-ingredients"
import { CookingOptions } from "@/features/cooking/cooking-options"
import type { CookingSession } from "@/features/cooking/cooking-session"
import { updateCookingSession } from "@/features/cooking/cooking-session"
import { CookingStep, cookingPlan } from "@/features/cooking/cooking-step"
import { CookingWakeLock } from "@/features/cooking/wake-lock"
import { contentAttributes, inlineArrowStep, isolate } from "@/lib/direction"
import { keyTaken } from "@/lib/keyboard"
import { shortcutsEnabled } from "@/lib/keyboard-shortcuts"
import { measurementSystem } from "@/lib/preferences"
import { formatRational, parseRational, rational } from "@/lib/rational"
import type { Presentation } from "@/lib/recipe-format"

import { m } from "../../lib/messages.ts"

const styles = {
  page: css({
    display: "flex",
    flexDirection: "column",
    gap: "5",
    marginInline: "auto",
    maxWidth: "80rem",
    // Cooking fills the window so the controls that move it forward rest at
    // the bottom of the screen rather than wherever a short step happens to
    // end. The subtraction is the application header above it.
    minHeight: "calc(100dvh - 3.75rem)",
    paddingBlock: { base: "4", md: "6" },
    paddingInline: { base: "4", md: "8" },
  }),
  toolbar: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    justifyContent: "space-between",
  }),
  // The recipe is the context, not the headline: what a cook reads from here
  // is the step, so the title stays the size of a label above it.
  title: css({
    fontFamily: "editorial",
    fontSize: { base: "lg", md: "xl" },
    fontWeight: "normal",
    margin: "0",
    minWidth: "0",
    overflowWrap: "anywhere",
  }),
  // The step leads, with the ingredients beside it where there is room.
  stage: css({
    alignItems: "start",
    display: "grid",
    flex: "1",
    gap: { base: "5", md: "6" },
    gridTemplateColumns: { base: "1fr", lg: "minmax(0, 1.7fr) minmax(16rem, 1fr)" },
  }),
  layout: css({ alignItems: "start", display: "grid", flex: "1", gap: "6" }),
  layoutWithDishes: css({
    alignItems: "start",
    display: "grid",
    flex: "1",
    gap: "6",
    gridTemplateColumns: { base: "minmax(0, 1fr)", xl: "16rem minmax(0, 1fr)" },
  }),
  // Always within reach, whatever the step's length has done to the page.
  actions: css({
    background: "bg.canvas",
    borderTopColor: "border.default",
    borderTopWidth: "1px",
    bottom: "0",
    display: "grid",
    gap: "2",
    marginInline: { base: "-4", md: "-8" },
    paddingBottom: "max(token(spacing.3), env(safe-area-inset-bottom))",
    paddingInline: { base: "4", md: "8" },
    paddingTop: "3",
    position: "sticky",
    zIndex: "docked",
  }),
  advance: css({ alignItems: "center", display: "flex", gap: "3" }),
  primary: css({ flex: "1" }),
  // Beside the step on a wide screen; after it on a narrow one, where the step
  // is what the cook opened the screen for.
  workspace: css({
    order: { base: 1, xl: 0 },
    background: { base: "transparent", xl: "bg.surface" },
    borderColor: "border.subtle",
    borderRadius: { base: "0", xl: "l2" },
    borderWidth: { base: "0", xl: "1px" },
    borderTopWidth: "1px",
    paddingBlock: { base: "4", xl: "4" },
    paddingInline: { base: "0", xl: "4" },
  }),
  note: css({ color: "fg.muted", fontSize: "sm", lineHeight: "1.5", margin: "0" }),
  // Between the toolbar and the step, without the margin a callout carries
  // when it stands on its own in a page.
  finishProblem: css({ marginBlock: "0", marginInline: "auto", maxWidth: "48rem" }),
}

export function CookingMode(props: {
  workspace?: JSX.Element
  actions?: JSX.Element
  recipe: Recipe
  entryID: string
  session: CookingSession
  onSession: (session: CookingSession) => void
  onExit: () => void
  onStartOver: () => void
  onFinish: () => Promise<void>
  finishing: boolean
  finishError?: unknown
  wakeLock: CookingWakeLock
}) {
  const plan = createMemo(() => cookingPlan(props.recipe))
  const sectioned = createMemo(() => (props.recipe.instruction_sections ?? []).length > 1)
  const current = createMemo(
    () => plan().find((entry) => entry.step.id === props.session.selectedStepID) ?? plan()[0],
  )
  const presentation = (): Presentation => ({
    system: props.session.display?.system ?? measurementSystem(),
    scale: parseRational(props.session.scale) ?? rational(1n),
  })
  const stepIngredientUses = createMemo(() => current()?.step.ingredient_uses ?? [])
  const last = () => {
    const entry = current()
    return !entry || entry.index >= plan().length - 1
  }
  const [wakeMessage, setWakeMessage] = createSignal("")

  const update = (patch: Partial<CookingSession>) =>
    props.onSession(updateCookingSession(props.session, patch))
  const toggleIngredient = (id: string) =>
    update({
      checkedIngredientIDs: props.session.checkedIngredientIDs.includes(id)
        ? props.session.checkedIngredientIDs.filter((item) => item !== id)
        : [...props.session.checkedIngredientIDs, id],
    })
  const selectStep = (id: string) => update({ selectedStepID: id })
  /** Completes the current step and moves to the next, as one action. */
  const advance = () => {
    const initialEntry = current()
    if (!initialEntry) return
    const next = plan()[initialEntry.index + 1]
    update({
      completedStepIDs: props.session.completedStepIDs.includes(initialEntry.step.id)
        ? props.session.completedStepIDs
        : [...props.session.completedStepIDs, initialEntry.step.id],
      selectedStepID: next?.step.id ?? initialEntry.step.id,
    })
  }
  /** Undoes Next: returns to the previous step and counts it as not done. */
  const back = () => {
    const entry = current()
    const initialPrevious = entry ? plan()[entry.index - 1] : undefined
    if (!initialPrevious) return
    const previousID = initialPrevious.step.id
    update({
      completedStepIDs: props.session.completedStepIDs.filter((id) => id !== previousID),
      selectedStepID: previousID,
    })
  }
  /** The last step's primary action: counts it done and opens the finish dialog. */
  const finish = () => {
    advance()
    void props.onFinish()
  }
  const setAwake = (enabled: boolean) => {
    update({ keepAwake: enabled })
    if (enabled) {
      if (!("wakeLock" in navigator)) setWakeMessage(m.cooking_mode_screen_awake_unavailable())
      else {
        setWakeMessage("")
        void props.wakeLock.enable(props.session.id)
      }
    } else {
      props.wakeLock.disable()
      setWakeMessage("")
    }
  }

  createEffect(() => {
    if (typeof navigator !== "undefined" && "mediaSession" in navigator) {
      if (typeof MediaMetadata !== "undefined") {
        const entry = current()
        const stepIndex = entry ? entry.index + 1 : 1
        const totalSteps = plan().length
        navigator.mediaSession.metadata = new MediaMetadata({
          title: props.recipe.title,
          artist: m.cooking_step_step_of({ number: stepIndex, total: totalSteps }),
          album: "Savorum",
        })
      }
      try {
        // oxlint-disable-next-line solid/reactivity
        navigator.mediaSession.setActionHandler("nexttrack", () => advance())
        // oxlint-disable-next-line solid/reactivity
        navigator.mediaSession.setActionHandler("previoustrack", () => back())
      } catch {
        // Unsupported actions throw.
      }
      onCleanup(() => {
        try {
          navigator.mediaSession.setActionHandler("nexttrack", null)
          navigator.mediaSession.setActionHandler("previoustrack", null)
        } catch {
          // Unsupported actions throw.
        }
      })
    }
  })

  onMount(() => {
    const visibility = () => {
      if (document.visibilityState === "visible" && props.session.keepAwake)
        void props.wakeLock.visible(props.session.id)
      else if (document.visibilityState !== "visible") props.wakeLock.hidden()
    }
    const pagehide = () => props.wakeLock.disable()
    const onKeyDown = (event: KeyboardEvent) => {
      if (keyTaken(event)) return
      const target = event.target instanceof Element ? event.target : undefined
      if (
        (event.key === " " || event.code === "Space") &&
        target?.closest("button, a, [role=checkbox]")
      )
        return
      if (!shortcutsEnabled() && event.key.length === 1) return
      // Left and right follow the direction the step controls are laid out in.
      const inline = inlineArrowStep(event.key, document.documentElement)
      if (event.key === " " || event.code === "Space") {
        event.preventDefault()
        if (!last()) advance()
      } else if (event.key === "j" || inline === 1 || event.key === "ArrowDown") {
        event.preventDefault()
        advance()
      } else if (event.key === "k" || inline === -1 || event.key === "ArrowUp") {
        event.preventDefault()
        back()
      } else if (event.key === "t" || event.key === "T") {
        const timerButton = document.querySelector<HTMLButtonElement>(
          "button[data-scope='popover'][data-part='trigger']",
        )
        timerButton?.click()
      }
    }
    document.addEventListener("visibilitychange", visibility)
    window.addEventListener("pagehide", pagehide)
    window.addEventListener("keydown", onKeyDown)
    onCleanup(() => {
      document.removeEventListener("visibilitychange", visibility)
      window.removeEventListener("pagehide", pagehide)
      window.removeEventListener("keydown", onKeyDown)
      props.wakeLock.disable()
    })
  })

  return (
    <main class={styles.page} aria-labelledby="cooking-title">
      <div class={styles.toolbar}>
        <Button variant="plain" onClick={props.onExit}>
          <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />
          {m.cooking_mode_exit_cooking()}
        </Button>
        <h1 id="cooking-title" class={styles.title}>
          {m.cooking_mode_cooking({ value1: isolate(props.recipe.title) })}
        </h1>
        <div class={css({ display: "flex", alignItems: "center", gap: "2" })}>
          <CookingOptions
            recipe={props.recipe}
            scale={presentation().scale}
            onScaleChange={(scale) => update({ scale: formatRational(scale) })}
            keepAwake={props.session.keepAwake}
            onKeepAwake={setAwake}
            wakeMessage={wakeMessage()}
            onStartOver={props.onStartOver}
          />
          {props.actions}
        </div>
      </div>
      <Show when={props.finishError}>
        <Callout class={styles.finishProblem} tone="danger">
          {m.cooking_mode_the_cooking_session_could_not_be_finished_try_again()}
        </Callout>
      </Show>
      <div class={props.workspace ? styles.layoutWithDishes : styles.layout}>
        <Show when={props.workspace}>
          <aside class={styles.workspace}>{props.workspace}</aside>
        </Show>
        <div class={styles.stage} {...contentAttributes(props.recipe.language)}>
          <CookingStep
            recipe={props.recipe}
            entryID={props.entryID}
            dishID={props.session.id}
            display={props.session.display}
            plan={plan()}
            current={current()}
            completedStepIDs={props.session.completedStepIDs}
            sectioned={sectioned()}
            onSelect={selectStep}
          />
          <CookingIngredients
            recipe={props.recipe}
            presentation={presentation()}
            checkedIngredientIDs={props.session.checkedIngredientIDs}
            stepIngredientUses={stepIngredientUses()}
            onToggle={toggleIngredient}
          />
        </div>
      </div>
      <div class={styles.actions}>
        <Show when={plan().length > 0}>
          <div class={styles.advance}>
            <IconButton
              variant="outline"
              size="lg"
              aria-label={m.cooking_mode_previous_step()}
              disabled={(current()?.index ?? 0) <= 0}
              onClick={back}
            >
              <ChevronLeft class={directionalIcon} aria-hidden="true" size={20} />
            </IconButton>
            <Show
              when={last()}
              fallback={
                <Button class={styles.primary} colorPalette="accent" size="lg" onClick={advance}>
                  {m.cooking_mode_next_step()}
                </Button>
              }
            >
              <Button
                class={styles.primary}
                colorPalette="accent"
                size="lg"
                loading={props.finishing}
                onClick={finish}
              >
                <Check aria-hidden="true" size={18} />
                {m.cooking_mode_finish_cooking()}
              </Button>
            </Show>
          </div>
        </Show>
      </div>
    </main>
  )
}
