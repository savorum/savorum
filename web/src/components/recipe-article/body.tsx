import { useRouterState } from "@tanstack/solid-router"
import { For, Show, createEffect, createMemo, onCleanup } from "solid-js"
import { css } from "styled-system/css"
import { recipePage } from "styled-system/recipes"

import { type Recipe, type TimingAssertion, assetURL, fallbackToOriginalImage } from "@/api/client"
import type { components } from "@/api/schema"
import { formatSourceDate } from "@/lib/date-format"
import { formatNumber } from "@/lib/locale-format"
import {
  type Presentation,
  asWritten,
  ingredientLine,
  stepAnchorID,
  showQuantity,
  visibleStepTitle,
} from "@/lib/recipe-format"
import { timerEligible, timingsWorthStating } from "@/lib/timing"
import { timingIsNamed, timingLine } from "@/lib/timing-summary"

import { m } from "../../lib/messages.ts"
import { DependencyNote, RecipeClaims, RecipeNutrition } from "./facts"
import { MicrowaveTiming } from "./microwave-timing"
import { StepTimer } from "./step-timer"

type Step = components["schemas"]["Step"]
type RecipeLayout = ReturnType<typeof recipePage>

const styles = {
  authoredText: css({ overflowWrap: "anywhere", whiteSpace: "pre-wrap" }),
  note: css({
    color: "fg.muted",
    display: "block",
    fontSize: "xs",
    marginTop: "0.5",
    overflowWrap: "anywhere",
    whiteSpace: "pre-wrap",
  }),
  stepMeta: css({
    color: "fg.muted",
    display: "block",
    // Interface-formatted facts, such as a temperature, keep their own order.
    unicodeBidi: "plaintext",
    fontFamily: "body",
    fontSize: "sm",
    marginTop: "1",
  }),
  stepTitle: css({
    display: "block",
    fontFamily: "body",
    fontSize: "sm",
    fontWeight: "semibold",
    marginBottom: "0.5",
    marginTop: "0",
  }),
  timers: css({
    // Top-aligned, so a microwave adjustment does not stretch its neighbours.
    alignItems: "flex-start",
    display: "inline-flex",
    flexWrap: "wrap",
    gap: "1",
    marginInlineStart: "2",
    verticalAlign: "baseline",
  }),
  stepImages: css({
    display: "grid",
    gap: "3",
    gridTemplateColumns: "repeat(auto-fit, minmax(12rem, 1fr))",
    marginTop: "3",
    maxWidth: "44rem",
  }),
  stepImage: css({
    borderRadius: "l2",
    maxHeight: "18rem",
    objectFit: "cover",
    width: "100%",
  }),
  notes: css({
    color: "fg.muted",
    fontSize: "sm",
    lineHeight: "1.65",
    marginTop: "8",
    maxWidth: "46rem",
  }),
  notesTitle: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
    margin: "0",
    marginBottom: "2",
  }),
  notesList: css({
    margin: "0",
    overflowWrap: "anywhere",
    paddingInlineStart: "4",
    whiteSpace: "pre-wrap",
  }),
  optional: css({
    color: "fg.muted",
    fontSize: "xs",
    fontStyle: "italic",
    marginInlineStart: "1.5",
  }),
  asWritten: css({
    color: "fg.muted",
    fontSize: "xs",
    marginInlineStart: "1.5",
    whiteSpace: "nowrap",
  }),
  // Between the branches of a choice, in the column the names are in.
  choiceJoiner: css({
    color: "fg.muted",
    fontSize: "sm",
    fontStyle: "italic",
    gridColumn: "2",
  }),
  choiceNote: css({
    color: "fg.muted",
    display: "block",
    fontSize: "sm",
    gridColumn: "2",
    overflowWrap: "anywhere",
    whiteSpace: "pre-wrap",
  }),
  choiceTail: css({ gridColumn: "2", "&:empty": { display: "none" } }),
  visuallyHidden: css({ srOnly: true }),
  variantList: css({ display: "grid", gap: "3", listStyle: "none", margin: "0", padding: "0" }),
  variant: css({ display: "grid", gap: "1" }),
  variantTitle: css({ fontWeight: "semibold" }),
  variantText: css({ margin: "0", whiteSpace: "pre-wrap" }),
  authoredMetadata: css({
    color: "fg.muted",
    fontSize: "xs",
    lineHeight: "1.55",
    marginTop: "6",
    maxWidth: "46rem",
  }),
  authoredMetadataList: css({
    display: "grid",
    gap: "1",
    gridTemplateColumns: "max-content minmax(0, 1fr)",
    margin: "0",
  }),
  authoredMetadataTerm: css({ fontWeight: "semibold" }),
  authoredMetadataValue: css({ margin: "0" }),
  equipmentList: css({ display: "grid", gap: "2", listStyle: "none", margin: "0", padding: "0" }),
  equipmentNote: css({
    color: "fg.muted",
    display: "block",
    fontSize: "xs",
    overflowWrap: "anywhere",
    whiteSpace: "pre-wrap",
  }),
}

function authoredMetadata(recipe: Recipe) {
  const entries: { label: string; value: string }[] = []
  const add = (label: string, values?: string[] | null) => {
    if (values?.length) entries.push({ label, value: values.join(", ") })
  }
  add(m.recipe_fields_categories(), recipe.categories)
  add(m.body_cooking_methods(), recipe.cooking_methods)
  add(m.recipe_fields_keywords(), recipe.keywords)
  if (recipe.published)
    entries.push({
      get label() {
        return m.body_published()
      },
      get value() {
        return formatSourceDate(recipe.published!)
      },
    })
  if (recipe.modified)
    entries.push({
      get label() {
        return m.body_source_updated()
      },
      get value() {
        return formatSourceDate(recipe.modified!)
      },
    })
  return entries
}

export function RecipeArticleBody(props: {
  recipe: Recipe
  entryID?: string
  layout: RecipeLayout
  presentation: Presentation
  /** "auto" when the recipe's language is unknown, so each authored text finds its own direction. */
  textDir?: "auto"
  content?: { lang?: string; dir?: "ltr" | "rtl" }
}) {
  const assetByID = (id: string) => props.recipe.assets?.find((asset) => asset.id === id)
  const methodSections = createMemo(() => props.recipe.instruction_sections ?? [])
  const methodIsSectioned = createMemo(() => methodSections().length > 1)
  const phasesByStep = createMemo(() => {
    const byID = new Map(
      (props.recipe.times?.assertions ?? []).map((assertion) => [assertion.id, assertion]),
    )
    const matches = new Map<string, TimingAssertion[]>()
    for (const section of props.recipe.instruction_sections ?? [])
      for (const step of section.steps ?? [])
        for (const ref of step.timing_refs ?? []) {
          const assertion = byID.get(ref)
          if (assertion) matches.set(step.id, [...(matches.get(step.id) ?? []), assertion])
        }
    return matches
  })
  const timeableFor = (stepID: string) =>
    (phasesByStep().get(stepID) ?? []).filter((assertion) =>
      timerEligible(assertion, { stepAssociated: true }),
    )
  const factsFor = (step: Step) =>
    timingsWorthStating(phasesByStep().get(step.id) ?? [], timeableFor(step.id), step.text ?? "")
      .map(timingLine)
      .filter(Boolean)
      .join(" · ")

  const routeHash = useRouterState({
    select: (state) => state.location.hash,
  })
  createEffect(() => {
    const anchor = routeHash()
    if (!anchor || typeof document === "undefined") return
    let attempts = 0
    let pending: ReturnType<typeof setTimeout> | undefined
    const settle = () => {
      const step = document.getElementById(anchor)
      if (step) {
        step.scrollIntoView({ block: "start" })
        return
      }
      if (attempts++ < 30) pending = setTimeout(settle, 32)
    }
    settle()
    onCleanup(() => clearTimeout(pending))
  })

  const details = createMemo(() => authoredMetadata(props.recipe))

  return (
    <div class={props.layout.body} {...props.content}>
      <aside class={props.layout.ingredients}>
        <Show when={(props.recipe.ingredient_sections ?? []).length > 0}>
          <h2 class={props.layout.columnTitle}>{m.body_ingredients()}</h2>
        </Show>
        <For each={props.recipe.ingredient_sections ?? []}>
          {(section) => (
            <section class={props.layout.ingredientSection}>
              <Show when={section.title}>
                <h3 class={props.layout.sectionTitle} dir={props.textDir}>
                  {section.title}
                </h3>
              </Show>
              <ul class={props.layout.ingredientList} role="list">
                <For each={section.ingredients ?? []}>
                  {(ingredient) => {
                    const line = () => ingredientLine(ingredient, props.presentation)
                    const choice = () => line().choice
                    // The amount column carries the amount when the branches share one.
                    const stacked = () => {
                      const shown = choice()
                      return shown && !shown.sharedAmount ? shown : undefined
                    }
                    const anchors = (
                      <For each={ingredient.alternatives ?? []}>
                        {(branch) => <span id={`ingredient-${branch.id}`} aria-hidden="true" />}
                      </For>
                    )
                    const extras = (
                      <>
                        <Show when={ingredient.recipe}>
                          {(dependency) => <DependencyNote dependency={dependency()} />}
                        </Show>
                        {/* Optionality belongs to the occurrence, not the ingredient. */}
                        <Show when={ingredient.optional}>
                          <span class={styles.optional}>{m.body_optional()}</span>
                        </Show>
                        {/* SREF requires an amount that cannot be recalculated to be reported as unscaled. */}
                        <Show when={line().keptAmount}>
                          <span class={styles.asWritten}>
                            {m.body_amount_as_written()}
                            <span class={styles.visuallyHidden}>
                              {" "}
                              {m.body_amount_as_written_hint()}
                            </span>
                          </span>
                        </Show>
                        <Show when={line().keptWording}>
                          <span class={styles.asWritten}>
                            {m.body_wording_as_written()}
                            <span class={styles.visuallyHidden}>
                              {" "}
                              {m.body_wording_as_written_hint()}
                            </span>
                          </span>
                        </Show>
                      </>
                    )
                    return (
                      <li class={props.layout.ingredient} id={`ingredient-${ingredient.id}`}>
                        <Show
                          when={stacked()}
                          fallback={
                            <>
                              <span
                                class={props.layout.amount}
                                data-empty={line().amount ? undefined : "true"}
                              >
                                {line().amount}
                                <Show when={line().amount}> </Show>
                              </span>
                              <span class={styles.authoredText} dir={props.textDir}>
                                {anchors}
                                <Show when={choice()} fallback={line().text}>
                                  {(shown) => (
                                    <For each={shown().branches}>
                                      {(branch, index) => (
                                        <>
                                          <Show when={index() > 0}>
                                            {" "}
                                            <span class={styles.choiceJoiner}>
                                              {shown().joiner}
                                            </span>{" "}
                                          </Show>
                                          {branch.text}
                                        </>
                                      )}
                                    </For>
                                  )}
                                </Show>
                                {extras}
                                <Show when={choice()?.note}>
                                  {(note) => <span class={styles.choiceNote}>{note()}</span>}
                                </Show>
                              </span>
                            </>
                          }
                        >
                          {(shown) => (
                            <>
                              <Show when={shown().lead}>
                                <span class={styles.choiceJoiner} dir={props.textDir}>
                                  {shown().lead}
                                </span>
                              </Show>
                              <For each={shown().branches}>
                                {(branch, index) => (
                                  <>
                                    <Show when={index() > 0}>
                                      <span class={styles.choiceJoiner} dir={props.textDir}>
                                        {shown().joiner}
                                      </span>
                                    </Show>
                                    <span
                                      class={props.layout.amount}
                                      data-empty={branch.amount ? undefined : "true"}
                                    >
                                      {branch.amount}
                                      <Show when={branch.amount}> </Show>
                                    </span>
                                    <span class={styles.authoredText} dir={props.textDir}>
                                      <Show when={index() === 0}>{anchors}</Show>
                                      {branch.text}
                                    </span>
                                  </>
                                )}
                              </For>
                              <Show when={shown().note}>
                                {(note) => (
                                  <span class={styles.choiceNote} dir={props.textDir}>
                                    {note()}
                                  </span>
                                )}
                              </Show>
                              <span class={styles.choiceTail}>{extras}</span>
                            </>
                          )}
                        </Show>
                      </li>
                    )
                  }}
                </For>
              </ul>
            </section>
          )}
        </For>
        <Show when={props.recipe.equipment?.length}>
          <section class={props.layout.section} aria-label={m.body_equipment()}>
            <h2 class={props.layout.columnTitle}>{m.body_equipment()}</h2>
            <ul class={styles.equipmentList} role="list">
              <For each={props.recipe.equipment ?? []}>
                {(equipment) => {
                  // Equipment quantities are authored requirements and do not
                  // follow the reader's recipe-yield scaling control.
                  const amount = () => {
                    const shown = showQuantity(equipment.quantity, asWritten)
                    if (!shown) return ""
                    return shown.approximate
                      ? m.body_about_amount({ amount: shown.text })
                      : shown.text
                  }
                  return (
                    <li dir={props.textDir}>
                      <Show when={amount()}>{(value) => <>{value()} </>}</Show>
                      {equipment.name}
                      <Show when={equipment.note}>
                        <span class={styles.equipmentNote}>{equipment.note}</span>
                      </Show>
                    </li>
                  )
                }}
              </For>
            </ul>
          </section>
        </Show>
      </aside>

      <div class={props.layout.instructions}>
        <Show when={methodSections().length > 0}>
          <h2 class={props.layout.columnTitle}>{m.body_method()}</h2>
        </Show>
        <For each={methodSections()}>
          {(section, sectionIndex) => (
            <section class={props.layout.section}>
              <Show when={section.title}>
                <h3 class={props.layout.sectionTitle} dir={props.textDir}>
                  {section.title}
                </h3>
              </Show>
              <ol class={props.layout.steps} role="list">
                <For each={section.steps ?? []}>
                  {(step, index) => (
                    <li class={props.layout.step} id={stepAnchorID(step.id)}>
                      <span class={props.layout.stepNumber} aria-hidden="true">
                        {formatNumber(index() + 1)}
                      </span>
                      <div>
                        <Show when={visibleStepTitle(step.title, step.text)}>
                          {(title) => (
                            <h4 class={styles.stepTitle} dir={props.textDir}>
                              {title()}
                            </h4>
                          )}
                        </Show>
                        <Show when={step.text}>
                          <p
                            class={css({
                              margin: "0",
                              overflowWrap: "anywhere",
                              whiteSpace: "pre-wrap",
                            })}
                            dir={props.textDir}
                          >
                            {step.text}
                          </p>
                        </Show>
                      </div>
                      <Show when={factsFor(step)}>
                        {(facts) => <span class={styles.stepMeta}>{facts()}</span>}
                      </Show>
                      <Show when={timeableFor(step.id).length > 0}>
                        {/* aria-label is ignored on a div without a role. */}
                        <div class={styles.timers} role="group" aria-label={m.body_timers()}>
                          <For each={timeableFor(step.id)}>
                            {(phase) =>
                              phase.microwave ? (
                                <MicrowaveTiming
                                  phase={phase}
                                  entryID={props.entryID}
                                  snapshot={props.recipe}
                                  scale={props.presentation.scale}
                                  recipeTitle={props.recipe.title}
                                  stepID={step.id}
                                  stepNumber={index() + 1}
                                  sectionTitle={methodIsSectioned() ? section.title : undefined}
                                  sectionNumber={
                                    methodIsSectioned() ? sectionIndex() + 1 : undefined
                                  }
                                  named={timingIsNamed(phase)}
                                />
                              ) : (
                                <StepTimer
                                  phase={phase}
                                  entryID={props.entryID}
                                  snapshot={props.recipe}
                                  scale={props.presentation.scale}
                                  recipeTitle={props.recipe.title}
                                  stepID={step.id}
                                  stepNumber={index() + 1}
                                  sectionTitle={methodIsSectioned() ? section.title : undefined}
                                  sectionNumber={
                                    methodIsSectioned() ? sectionIndex() + 1 : undefined
                                  }
                                  named={timingIsNamed(phase)}
                                />
                              )
                            }
                          </For>
                        </div>
                      </Show>
                      <Show when={step.image_refs?.length}>
                        <div class={styles.stepImages}>
                          <For each={step.image_refs ?? []}>
                            {(imageID) => {
                              const asset = assetByID(imageID)
                              return asset ? (
                                <img
                                  onError={(event) => fallbackToOriginalImage(event.currentTarget)}
                                  class={styles.stepImage}
                                  src={assetURL(
                                    `sha256:${asset.sha256}`,
                                    {
                                      recipeID: props.entryID ?? "unavailable",
                                    },
                                    "thumb",
                                  )}
                                  alt={asset.alt ?? ""}
                                  loading="lazy"
                                />
                              ) : null
                            }}
                          </For>
                        </div>
                      </Show>
                    </li>
                  )}
                </For>
              </ol>
            </section>
          )}
        </For>

        <Show when={props.recipe.variants?.length}>
          <aside class={styles.notes} aria-label={m.body_variations()}>
            <h2 class={styles.notesTitle}>{m.body_variations()}</h2>
            <ul class={styles.variantList} role="list">
              <For each={props.recipe.variants ?? []}>
                {(variant) => (
                  <li class={styles.variant} dir={props.textDir}>
                    <Show when={variant.title}>
                      <strong class={styles.variantTitle}>{variant.title}</strong>
                    </Show>
                    <p class={styles.variantText}>{variant.text}</p>
                  </li>
                )}
              </For>
            </ul>
          </aside>
        </Show>

        <RecipeClaims recipe={props.recipe} textDir={props.textDir} />

        <Show when={props.recipe.notes?.length}>
          <aside class={styles.notes}>
            <h2 class={styles.notesTitle}>{m.body_notes()}</h2>
            <ul class={styles.notesList}>
              <For each={props.recipe.notes ?? []}>
                {(note) => <li dir={props.textDir}>{note}</li>}
              </For>
            </ul>
          </aside>
        </Show>

        <RecipeNutrition
          recipe={props.recipe}
          scale={props.presentation.scale}
          textDir={props.textDir}
        />

        <Show when={details().length > 0}>
          <aside class={styles.authoredMetadata} aria-label={m.body_recipe_details()}>
            <dl class={styles.authoredMetadataList}>
              <For each={details()}>
                {(entry) => (
                  <>
                    <dt class={styles.authoredMetadataTerm}>{entry.label}</dt>
                    <dd class={styles.authoredMetadataValue} dir={props.textDir}>
                      {entry.value}
                    </dd>
                  </>
                )}
              </For>
            </dl>
          </aside>
        </Show>
      </div>
    </div>
  )
}
