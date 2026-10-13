import { Minus, Plus } from "lucide-solid"
import { For, Show, createMemo } from "solid-js"
import { css } from "styled-system/css"
import { recipePage } from "styled-system/recipes"

import type { Recipe } from "@/api/client"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { normalizeNumericInput } from "@/lib/locale-format"
import { measurementSystem, setMeasurementSystem } from "@/lib/preferences"
import { type Rational, compare, divide, multiply, rational } from "@/lib/rational"
import { displayYield, otherYields } from "@/lib/recipe-facts"
import {
  cookNumber,
  ingredientsKeptAsWritten,
  yieldAmount,
  yieldLine,
  yieldNoun,
} from "@/lib/recipe-format"
import type { MeasurementSystem } from "@/lib/units"

import { m } from "../../lib/messages.ts"

const styles = {
  scale: css({ alignItems: "center", display: "flex", gap: "1" }),
  scaleValue: css({
    color: "fg.default",
    fontSize: "sm",
    fontVariantNumeric: "tabular-nums",
    minWidth: "3.5rem",
    textAlign: "center",
  }),
  yieldInput: css({ fontVariantNumeric: "tabular-nums", textAlign: "center", width: "4.5rem" }),
  yieldNoun: css({ color: "fg.default", fontSize: "sm" }),
  factor: css({
    color: "fg.muted",
    fontSize: "xs",
    fontVariantNumeric: "tabular-nums",
  }),
  kept: css({
    color: "fg.muted",
    fontSize: "xs",
    lineHeight: "1.4",
    margin: "0",
    maxWidth: "24rem",
  }),
  batchGroup: css({ alignItems: "center", flexDirection: "row", columnGap: "3", rowGap: "1.5" }),
  batchLine: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "2" }),
  yieldChoices: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "1.5" }),
  yieldChoice: css({
    background: "bg.muted",
    borderRadius: "l1",
    color: "fg.muted",
    cursor: "pointer",
    fontSize: "xs",
    paddingBlock: "1",
    paddingInline: "2",
    _hover: { color: "fg.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default" },
    _pressed: { background: "bg.surface", color: "fg.default", boxShadow: "xs" },
  }),
  alsoMakes: css({ color: "fg.muted", fontSize: "xs", margin: "0" }),
  measureGroup: css({ columnGap: "1" }),
  remember: css({
    background: "transparent",
    color: "fg.muted",
    cursor: "pointer",
    fontSize: "xs",
    textDecoration: "underline",
    textDecorationColor: "border.default",
    textUnderlineOffset: "3px",
    _hover: { color: "accent.default" },
    _focusVisible: {
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "2px",
    },
  }),
}

const measurementOptions: { value: MeasurementSystem; label: string }[] = [
  {
    value: "original",
    get label() {
      return m.scale_controls_as_written()
    },
  },
  {
    value: "metric",
    get label() {
      return m.scale_controls_metric()
    },
  },
  {
    value: "us",
    get label() {
      return m.scale_controls_us()
    },
  },
  {
    value: "au",
    get label() {
      return m.scale_controls_australian()
    },
  },
]

const scaleSteps: Rational[] = [
  rational(1n, 4n),
  rational(1n, 3n),
  rational(1n, 2n),
  rational(2n, 3n),
  rational(1n),
  rational(3n, 2n),
  rational(2n),
  rational(3n),
  rational(4n),
]

function parseYieldEntry(text: string): Rational | undefined {
  const trimmed = normalizeNumericInput(text).trim()
  const fraction = trimmed.match(/^(\d+)\s*\/\s*(\d+)$/)
  if (fraction) {
    const denominator = BigInt(fraction[2])
    return denominator > 0n ? rational(BigInt(fraction[1]), denominator) : undefined
  }
  const decimal = trimmed.match(/^(\d+)(?:[.](\d+))?$/)
  if (!decimal) return undefined
  const decimals = decimal[2] ?? ""
  const value = rational(BigInt(`${decimal[1]}${decimals}`), 10n ** BigInt(decimals.length))
  return value.numerator > 0n ? value : undefined
}

export function RecipeScaleControls(props: {
  class: string
  /** The controls keep the interface's direction inside recipe content. */
  interface?: { lang: string; dir: "ltr" | "rtl" }
  recipe: Recipe
  /** Position of the yield to scale by; the first numeric yield when absent. */
  yieldIndex?: number
  onYieldIndexChange?: (index: number) => void
  scale?: Rational
  onScaleChange?: (scale: Rational) => void
  rememberedYield?: Rational
  rememberedScale?: Rational
  onRemember?: (batch: { yield?: Rational; scale?: Rational } | undefined) => void
}) {
  const layout = recipePage()
  const scale = () => props.scale ?? rational(1n)
  const scaleIndex = () => scaleSteps.findIndex((step) => compare(step, scale()) === 0)
  const scaled = () => compare(scale(), rational(1n)) !== 0
  const shown = createMemo(() => displayYield(props.recipe, props.yieldIndex))
  const writtenYield = createMemo(() => yieldAmount(shown().value))
  const scalableYields = createMemo(() =>
    (props.recipe.yields ?? [])
      .map((value, index) => ({ value, index }))
      .filter(({ value }) => yieldAmount(value) !== undefined),
  )
  const alsoMakes = createMemo(() =>
    otherYields(props.recipe, shown().index)
      .filter(
        (value) =>
          !(
            props.onYieldIndexChange &&
            scalableYields().length > 1 &&
            yieldAmount(value) !== undefined
          ),
      )
      .map((value) => yieldLine(value, { system: measurementSystem(), scale: scale() }))
      .filter(Boolean),
  )
  const wantedYield = createMemo(() => {
    const written = writtenYield()
    return written ? multiply(written, scale()) : undefined
  })
  const noun = () => yieldNoun(shown().value, wantedYield())

  const stepScale = (direction: number) => {
    const index = scaleIndex()
    const next = scaleSteps[(index === -1 ? 4 : index) + direction]
    if (next) props.onScaleChange?.(next)
  }
  const setYield = (wanted: Rational | undefined) => {
    const written = writtenYield()
    if (!written || !wanted || wanted.numerator <= 0n) return
    props.onScaleChange?.(divide(wanted, written))
  }
  const stepYield = (direction: number) => {
    const wanted = wantedYield()
    if (!wanted) return
    const next =
      wanted.denominator === 1n
        ? rational(wanted.numerator + BigInt(direction))
        : rational(wanted.numerator + BigInt(direction) * wanted.denominator, wanted.denominator)
    if (next.numerator > 0n) setYield(next)
  }

  const keptAsWritten = createMemo(() =>
    ingredientsKeptAsWritten(props.recipe, {
      system: measurementSystem(),
      scale: scale(),
    }),
  )

  const remembered = () => props.rememberedYield ?? props.rememberedScale
  const rememberedIsCurrent = () => {
    const wanted = wantedYield()
    if (props.rememberedYield && wanted) return compare(props.rememberedYield, wanted) === 0
    if (props.rememberedScale) return compare(props.rememberedScale, scale()) === 0
    return false
  }
  const rememberCurrent = () => {
    if (!scaled()) {
      props.onRemember?.(undefined)
      return
    }
    const wanted = wantedYield()
    props.onRemember?.(wanted ? { yield: wanted } : { scale: scale() })
  }

  return (
    <div class={props.class} {...props.interface}>
      {/* Only "Always start here" persists; the rest of this group is view state. */}
      <div class={`${layout.controlGroup} ${styles.batchGroup}`}>
        <div class={styles.batchLine}>
          <Show when={props.onScaleChange}>
            <Show
              when={wantedYield()}
              fallback={
                <div class={styles.scale} role="group" aria-label={m.scale_controls_batch_size()}>
                  <IconButton
                    aria-label={m.scale_controls_make_less()}
                    size="xs"
                    variant="outline"
                    disabled={scaleIndex() <= 0}
                    onClick={() => stepScale(-1)}
                  >
                    <Minus aria-hidden="true" size={14} />
                  </IconButton>
                  <span class={styles.scaleValue} aria-live="polite">
                    {scaled() ? `${cookNumber(scale())}×` : m.scale_controls_one_batch()}
                  </span>
                  <IconButton
                    aria-label={m.scale_controls_make_more()}
                    size="xs"
                    variant="outline"
                    disabled={scaleIndex() >= scaleSteps.length - 1}
                    onClick={() => stepScale(1)}
                  >
                    <Plus aria-hidden="true" size={14} />
                  </IconButton>
                </div>
              }
            >
              {(wanted) => (
                <div
                  class={styles.scale}
                  role="group"
                  aria-label={m.scale_yield_group({ yieldLabel: noun() })}
                >
                  <span class={styles.yieldNoun}>{m.scale_controls_makes()}</span>
                  <Show when={scaled()}>
                    <span class={styles.factor} title={m.scale_controls_batch_size()}>
                      {cookNumber(scale())}
                      {m.common_times_symbol()}
                    </span>
                  </Show>
                  <IconButton
                    aria-label={m.scale_controls_one_fewer({ value1: noun() })}
                    size="xs"
                    variant="outline"
                    disabled={compare(wanted(), rational(1n)) <= 0}
                    onClick={() => stepYield(-1)}
                  >
                    <Minus aria-hidden="true" size={14} />
                  </IconButton>
                  <Input
                    size="sm"
                    class={styles.yieldInput}
                    type="text"
                    inputmode="decimal"
                    aria-label={m.scale_controls_how_many({ value1: noun() })}
                    value={cookNumber(wanted())}
                    onChange={(event) => {
                      const typed = parseYieldEntry(event.currentTarget.value)
                      if (typed) setYield(typed)
                      else event.currentTarget.value = cookNumber(wanted())
                    }}
                  />
                  <IconButton
                    aria-label={m.scale_controls_one_more({ value1: noun() })}
                    size="xs"
                    variant="outline"
                    onClick={() => stepYield(1)}
                  >
                    <Plus aria-hidden="true" size={14} />
                  </IconButton>
                  <span class={styles.yieldNoun}>{noun()}</span>
                </div>
              )}
            </Show>
          </Show>
        </div>
        <Show when={props.onYieldIndexChange && scalableYields().length > 1}>
          <div class={styles.yieldChoices} role="group" aria-label={m.scale_controls_scale_by()}>
            <span class={layout.controlLabel}>{m.scale_controls_scale_by()}</span>
            <For each={scalableYields()}>
              {({ value, index }) => (
                <button
                  type="button"
                  class={styles.yieldChoice}
                  aria-pressed={shown().index === index}
                  onClick={() => props.onYieldIndexChange?.(index)}
                >
                  {yieldLine(value, { system: measurementSystem(), scale: rational(1n) })}
                </button>
              )}
            </For>
          </div>
        </Show>
        <Show when={alsoMakes().length > 0}>
          <p class={styles.alsoMakes}>
            {m.recipe_article_also_makes({ yields: alsoMakes().join(" · ") })}
          </p>
        </Show>

        <Show when={keptAsWritten() > 0}>
          <p class={styles.kept} role="status">
            {m.scale_controls_kept_as_written({ count: keptAsWritten() })}
          </p>
        </Show>

        <Show when={props.onRemember && (scaled() || remembered())}>
          <Show
            when={!rememberedIsCurrent()}
            fallback={
              <button
                type="button"
                class={styles.remember}
                onClick={() => props.onRemember?.(undefined)}
              >
                {" "}
                {m.scale_controls_back_to_what_the_recipe_makes()}{" "}
              </button>
            }
          >
            <button type="button" class={styles.remember} onClick={rememberCurrent}>
              {scaled()
                ? m.scale_controls_always_start_here()
                : m.scale_controls_back_to_what_the_recipe_makes()}
            </button>
          </Show>
        </Show>
      </div>

      <div class={`${layout.controlGroup} ${styles.measureGroup}`}>
        <Select
          size="sm"
          quiet
          aria-label={m.scale_controls_measurements()}
          value={measurementSystem()}
          onValueChange={(value) => setMeasurementSystem(value as MeasurementSystem)}
          items={measurementOptions}
        />
      </div>
    </div>
  )
}
