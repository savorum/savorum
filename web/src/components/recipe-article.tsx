import { For, type JSX, Show, createMemo } from "solid-js"
import { css } from "styled-system/css"
import { recipePage } from "styled-system/recipes"

import { type Recipe, assetURL, fallbackToOriginalImage } from "@/api/client"
import { RecipeArticleBody } from "@/components/recipe-article/body"
import { MediaDetails } from "@/components/recipe-article/facts"
import { RecipeScaleControls } from "@/components/recipe-article/scale-controls"
import { languageDirection } from "@/lib/direction"
import { formatDuration } from "@/lib/duration-format"
import { interfaceAttributes } from "@/lib/i18n"
import { measurementSystem, personalPreferences } from "@/lib/preferences"
import { type Rational, rational } from "@/lib/rational"
import { displayYield, otherYields } from "@/lib/recipe-facts"
import {
  type Presentation,
  recipeMeasurementRegion,
  yieldAmount,
  yieldLine,
} from "@/lib/recipe-format"
import { safeSourceURL, sourceHost } from "@/lib/source-url"
import { summarizeTimes, timeSummaryLine, timingLine } from "@/lib/timing-summary"

import { m } from "../lib/messages.ts"

const styles = {
  dot: css({ color: "border.default" }),
  link: css({
    textDecoration: "underline",
    textDecorationColor: "border.default",
    textUnderlineOffset: "3px",
    _hover: { color: "accent.default" },
  }),
  gallery: css({
    order: 5,
    display: "grid",
    gap: "4",
    gridTemplateColumns: "repeat(auto-fit, minmax(14rem, 1fr))",
    marginBottom: { base: "6", md: "8" },
  }),
  galleryItem: css({ margin: "0" }),
  imageFrame: css({ position: "relative" }),
  imageDetails: css({
    color: "white",
    insetBlockStart: "2",
    insetInlineEnd: "2",
    opacity: "0.4",
    position: "absolute",
    transition: "opacity 150ms ease",
    _hover: { background: "transparent", opacity: "1" },
    _active: { background: "transparent" },
    _on: { background: "transparent" },
    _focusVisible: { opacity: "1" },
    _expanded: { opacity: "1" },
  }),
  alsoMakes: css({ color: "fg.muted", fontSize: "sm", margin: "0", marginTop: "1" }),
  galleryImage: css({
    aspectRatio: "4 / 3",
    borderRadius: "l2",
    objectFit: "cover",
    width: "100%",
  }),
  galleryCaption: css({
    color: "fg.muted",
    fontSize: "xs",
    marginTop: "1.5",
  }),
  phases: css({
    alignItems: "center",
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "xs",
    rowGap: "1",
    columnGap: "3",
    marginTop: "2",
  }),
  phase: css({
    background: "bg.muted",
    borderRadius: "l1",
    paddingBlock: "0.5",
    paddingInline: "2",
  }),
  footer: css({ order: 6 }),
  disputedTotal: css({ flexBasis: "100%", margin: "0" }),
}

const regionNames: Record<string, () => string> = {
  metric: () => m.recipe_article_metric_measures(),
  us: () => m.recipe_article_us_measures(),
  au: () => m.recipe_article_australian_measures(),
  imperial: () => m.recipe_article_imperial_measures(),
}

export interface RecipeArticleProps {
  recipe: Recipe
  /**
   * The library entry this recipe is read under, which addresses the recipe
   * page; the recipe's own `id` names no route.
   */
  entryID?: string
  /** Navigation and actions rendered above the recipe. */
  toolbar?: JSX.Element
  /** Library organization rendered after the recipe. */
  footer?: JSX.Element
  /** The recipe's other languages, shown beside its secondary details. */
  languages?: JSX.Element
  /** Set for a read-only look at an earlier version. */
  readOnly?: boolean
  scale?: Rational
  onScaleChange?: (scale: Rational) => void
  /** The yield this reader asked Savorum to open at, when they set one. */
  rememberedYield?: Rational
  rememberedScale?: Rational
  /** Saves or clears the remembered batch. Absent when it cannot be saved. */
  onRemember?: (batch: { yield?: Rational; scale?: Rational } | undefined) => void
  /** Position of the authored yield this reader shows first and scales by. */
  preferredYield?: number
  onPreferredYieldChange?: (index: number) => void
}

/** The reading presentation of a recipe. Everything here is recipe content. */
export const RecipeArticle = (props: RecipeArticleProps) => {
  const recipe = () => props.recipe
  const scale = () => props.scale ?? rational(1n)
  const presentation = (): Presentation => ({
    system: measurementSystem(),
    scale: scale(),
  })
  const assetByID = (id: string) => recipe().assets?.find((asset) => asset.id === id)
  const primaryImage = () =>
    recipe().primary_image ? assetByID(recipe().primary_image ?? "") : undefined
  const galleryImages = () =>
    (recipe().image_refs ?? [])
      .filter((id) => id !== recipe().primary_image)
      .map(assetByID)
      .filter((asset) => asset !== undefined)
  const sourceURL = () => safeSourceURL(recipe().source?.url)
  const sourceName = () => recipe().source?.name ?? sourceHost(recipe().source?.url)
  const region = createMemo(() => recipeMeasurementRegion(recipe()))
  const layout = createMemo(() => recipePage({ illustrated: Boolean(primaryImage()) }))
  const shownYield = createMemo(() => displayYield(recipe(), props.preferredYield))
  const writtenYield = createMemo(() => yieldAmount(shownYield().value))
  const alsoMakes = createMemo(() =>
    otherYields(recipe(), shownYield().index)
      .map((value) => yieldLine(value, presentation()))
      .filter(Boolean),
  )
  /**
   * Recipe content takes its direction from the recipe's language, apart from
   * the interface around it. Without a language, each authored text decides.
   */
  const direction = createMemo(() => languageDirection(recipe().language))
  const content = () => (direction() ? { lang: recipe().language, dir: direction() } : {})
  const textDir = () => (direction() ? undefined : "auto")

  // Declared before the memo that reads it: createMemo evaluates immediately.
  const referencedAssertionIDs = createMemo(() => {
    const referenced = new Set<string>()
    for (const section of recipe().instruction_sections ?? [])
      for (const step of section.steps ?? [])
        for (const ref of step.timing_refs ?? []) referenced.add(ref)
    return referenced
  })
  /** The assertions no instruction points at, which are the recipe's own clock. */
  const phases = createMemo(() => {
    return (recipe().times?.assertions ?? [])
      .filter((assertion) => !referencedAssertionIDs().has(assertion.id))
      .map(timingLine)
      .filter(Boolean)
  })
  /** A total the recipe's own method disproves, quoted but never the headline. */
  const disputedTotal = createMemo(() => formatDuration(summarizeTimes(recipe()).writtenTotal))

  const meta = createMemo(() => {
    const entries: string[] = []
    const clock = timeSummaryLine(recipe())
    if (clock) entries.push(clock)
    // A recipe whose yield is a number gets its own control below, so it is
    // not also repeated here.
    if (!writtenYield()) {
      const yielded = yieldLine(shownYield().value, presentation())
      if (yielded) entries.push(yielded)
    }
    if (recipe().courses?.length) entries.push(recipe().courses!.join(", "))
    if (recipe().cuisines?.length) entries.push(recipe().cuisines!.join(", "))
    if (recipe().authors?.length)
      entries.push(
        recipe()
          .authors!.map((author) => author.name)
          .join(", "),
      )
    const written = region()
    if (written && measurementSystem() === "original") entries.push(regionNames[written]())
    return entries
  })

  return (
    <article
      class={layout().root}
      style={{ "line-height": personalPreferences().recipe_view === "compact" ? "1.4" : undefined }}
    >
      {props.toolbar}

      <header class={layout().masthead} {...content()}>
        <div class={layout().headline}>
          <h1 class={layout().title} dir={textDir()}>
            {recipe().title}
          </h1>
          <Show when={recipe().description}>
            <p class={layout().description} dir={textDir()}>
              {recipe().description}
            </p>
          </Show>

          <div class={layout().meta}>
            <For each={meta()}>
              {(entry, index) => (
                <>
                  <Show when={index() > 0}>
                    <span class={styles.dot} aria-hidden="true">
                      {" "}
                      {m.common_dot_separator()}{" "}
                    </span>
                  </Show>
                  {/* Formatted values such as "10 min" keep their own order in either direction. */}
                  <bdi>{entry}</bdi>
                </>
              )}
            </For>
            <Show when={sourceName()}>
              {(name) => (
                <>
                  <Show when={meta().length > 0}>
                    <span class={styles.dot} aria-hidden="true">
                      {" "}
                      {m.common_dot_separator()}{" "}
                    </span>
                  </Show>
                  <Show when={sourceURL()} fallback={<span>{name()}</span>}>
                    <a class={styles.link} href={sourceURL()} target="_blank" rel="noreferrer">
                      {name()}
                    </a>
                  </Show>
                </>
              )}
            </Show>
          </div>

          <Show when={props.readOnly && alsoMakes().length > 0}>
            <p class={styles.alsoMakes}>
              {m.recipe_article_also_makes({ yields: alsoMakes().join(" · ") })}
            </p>
          </Show>

          {props.languages}

          <Show when={phases().length > 0 || disputedTotal()}>
            <div class={styles.phases} aria-label={m.recipe_article_time_phases()}>
              <For each={phases()}>{(phase) => <bdi class={styles.phase}>{phase}</bdi>}</For>
              <Show when={disputedTotal()}>
                {(total) => (
                  <p class={styles.disputedTotal}>
                    {m.recipe_article_written_total_shorter({ value1: total() })}
                  </p>
                )}
              </Show>
            </div>
          </Show>

          <Show when={!props.readOnly}>
            <RecipeScaleControls
              class={layout().controls}
              interface={interfaceAttributes()}
              recipe={recipe()}
              yieldIndex={shownYield().index}
              onYieldIndexChange={props.onPreferredYieldChange}
              scale={props.scale}
              onScaleChange={props.onScaleChange}
              rememberedYield={props.rememberedYield}
              rememberedScale={props.rememberedScale}
              onRemember={props.onRemember}
            />
          </Show>
        </div>

        <Show when={primaryImage()}>
          {(asset) => (
            <div class={styles.imageFrame}>
              <img
                class={layout().image}
                src={assetURL(
                  `sha256:${asset().sha256}`,
                  { recipeID: props.entryID ?? "unavailable" },
                  "reader",
                )}
                onError={(event) => fallbackToOriginalImage(event.currentTarget)}
                alt={asset().alt ?? ""}
                fetchpriority="high"
                decoding="async"
              />
              <MediaDetails asset={asset()} class={styles.imageDetails} />
            </div>
          )}
        </Show>
      </header>

      <RecipeArticleBody
        content={content()}
        textDir={textDir()}
        recipe={recipe()}
        entryID={props.entryID}
        layout={layout()}
        presentation={presentation()}
      />

      <Show when={galleryImages().length > 0}>
        <section
          class={styles.gallery}
          aria-label={m.recipe_article_recipe_photos()}
          {...content()}
        >
          <For each={galleryImages()}>
            {(asset) => (
              <figure class={`${styles.galleryItem} ${styles.imageFrame}`}>
                <img
                  onError={(event) => fallbackToOriginalImage(event.currentTarget)}
                  class={styles.galleryImage}
                  src={assetURL(
                    `sha256:${asset.sha256}`,
                    { recipeID: props.entryID ?? "unavailable" },
                    "thumb",
                  )}
                  alt={asset.alt ?? ""}
                  loading="lazy"
                />
                <MediaDetails asset={asset} class={styles.imageDetails} />
                <Show when={asset.caption}>
                  <figcaption class={styles.galleryCaption}>{asset.caption}</figcaption>
                </Show>
              </figure>
            )}
          </For>
        </section>
      </Show>

      {/* Wrapped so the footer takes an explicit order in the column. */}
      <div class={styles.footer}>{props.footer}</div>
    </article>
  )
}
