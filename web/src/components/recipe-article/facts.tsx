import { Info } from "lucide-solid"
import { For, Show } from "solid-js"
import { css } from "styled-system/css"

import type { Recipe, RecipeAsset, RecipeDependency } from "@/api/client"
import { Disclosure } from "@/components/ui/disclosure"
import { IconButton } from "@/components/ui/icon-button"
import { Popover } from "@/components/ui/popover"
import type { Rational } from "@/lib/rational"
import {
  claimVariantTitle,
  declarationsByPresence,
  hasProvenance,
  nutrientName,
  nutrientValue,
  nutritionAtBatch,
  nutritionBasisLabel,
} from "@/lib/recipe-facts"
import { safeSourceURL } from "@/lib/source-url"

import { m } from "../../lib/messages.ts"

const styles = {
  section: css({
    color: "fg.muted",
    fontSize: "sm",
    lineHeight: "1.6",
    marginTop: "8",
    maxWidth: "46rem",
  }),
  title: css({ fontSize: "xs", fontWeight: "semibold", margin: "0", marginBottom: "2" }),
  hint: css({ fontSize: "xs", margin: "0", marginTop: "2" }),
  statements: css({ display: "grid", gap: "4" }),
  statement: css({ display: "grid", gap: "1" }),
  basis: css({ color: "fg.default", fontWeight: "medium", margin: "0" }),
  values: css({
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) max-content",
    columnGap: "6",
    margin: "0",
    maxWidth: "22rem",
  }),
  value: css({ fontVariantNumeric: "tabular-nums", margin: "0", textAlign: "end" }),
  written: css({ fontSize: "xs" }),
  claims: css({ display: "grid", gap: "1.5", listStyle: "none", margin: "0", padding: "0" }),
  claimLabel: css({ color: "fg.default", fontWeight: "medium" }),
  scope: css({ fontSize: "xs" }),
  details: css({ display: "grid", gap: "1", fontSize: "sm", margin: "0", maxWidth: "20rem" }),
  detailTerm: css({ color: "fg.muted", fontSize: "xs" }),
  detailValue: css({ margin: "0", overflowWrap: "anywhere" }),
  link: css({
    textDecoration: "underline",
    textDecorationColor: "border.default",
    textUnderlineOffset: "3px",
    _hover: { color: "accent.default" },
  }),
}

export function RecipeNutrition(props: { recipe: Recipe; scale: Rational; textDir?: "auto" }) {
  return (
    <Show when={props.recipe.nutrition?.length}>
      <aside class={styles.section}>
        <Disclosure label={m.nutrition_title()}>
          <div class={styles.statements}>
            <For each={props.recipe.nutrition ?? []}>
              {(statement) => {
                const batch = () => nutritionAtBatch(statement, props.scale)
                return (
                  <section class={styles.statement}>
                    <p class={styles.basis} dir={props.textDir}>
                      {batch().scaled ? m.nutrition_basis_batch() : nutritionBasisLabel(statement)}
                    </p>
                    <dl class={styles.values}>
                      <For each={statement.nutrients}>
                        {(nutrient) => (
                          <>
                            <dt dir={props.textDir}>{nutrientName(nutrient)}</dt>
                            <dd class={styles.value}>
                              {nutrientValue(nutrient, batch().factor)}
                              <Show when={batch().scaled}>
                                <span class={styles.written}>
                                  {" "}
                                  {m.nutrition_recipe_value({ value: nutrientValue(nutrient) })}
                                </span>
                              </Show>
                            </dd>
                          </>
                        )}
                      </For>
                    </dl>
                    <Show when={batch().keptAsWritten}>
                      <p class={styles.hint}>
                        {statement.basis.kind === "unspecified"
                          ? m.nutrition_unspecified_not_scaled()
                          : m.nutrition_unchanged_by_batch()}
                      </p>
                    </Show>
                    <Show when={statement.note}>
                      <p class={styles.hint} dir={props.textDir}>
                        {statement.note}
                      </p>
                    </Show>
                  </section>
                )
              }}
            </For>
          </div>
          <p class={styles.hint}>{m.nutrition_source_statement()}</p>
        </Disclosure>
      </aside>
    </Show>
  )
}

export function RecipeClaims(props: { recipe: Recipe; textDir?: "auto" }) {
  const scope = (claim: Parameters<typeof claimVariantTitle>[1]) =>
    claimVariantTitle(props.recipe, claim)
  return (
    <Show when={props.recipe.dietary_claims?.length || props.recipe.allergen_declarations?.length}>
      <aside class={styles.section}>
        <Disclosure label={m.claims_title()}>
          <ul class={styles.claims} role="list">
            <For each={props.recipe.dietary_claims ?? []}>
              {(claim) => (
                <li dir={props.textDir}>
                  {claim.text}
                  <Show when={claim.condition && !claim.text.includes(claim.condition)}>
                    {" "}
                    <span class={styles.scope}>
                      {m.claims_condition({ condition: claim.condition ?? "" })}
                    </span>
                  </Show>
                  <Show when={scope(claim)}>
                    {(variant) => (
                      <span class={styles.scope}>
                        {" "}
                        {m.claims_for_variant({ variant: variant() })}
                      </span>
                    )}
                  </Show>
                </li>
              )}
            </For>
            <For each={declarationsByPresence(props.recipe.allergen_declarations ?? [])}>
              {(group) => (
                <li>
                  <For each={group.declarations}>
                    {(declaration, index) => (
                      <>
                        <Show when={index() > 0}>, </Show>
                        <span dir={props.textDir}>{declaration.text}</span>
                        <Show
                          when={
                            declaration.condition &&
                            !declaration.text.includes(declaration.condition)
                          }
                        >
                          {" "}
                          <span class={styles.scope}>
                            {m.claims_condition({ condition: declaration.condition ?? "" })}
                          </span>
                        </Show>
                        <Show when={scope(declaration)}>
                          {(variant) => (
                            <span class={styles.scope}>
                              {" "}
                              {m.claims_for_variant({ variant: variant() })}
                            </span>
                          )}
                        </Show>
                      </>
                    )}
                  </For>
                </li>
              )}
            </For>
          </ul>
          <p class={styles.hint}>{m.claims_not_verified()}</p>
        </Disclosure>
      </aside>
    </Show>
  )
}

/** The recipe that makes an ingredient, linked when the source gave an address. */
export function DependencyNote(props: { dependency: RecipeDependency }) {
  const url = () => safeSourceURL(props.dependency.url)
  const name = () => props.dependency.title?.trim() || m.dependency_another_recipe()
  return (
    <span class={styles.scope}>
      {" "}
      <Show when={url()} fallback={m.dependency_see({ recipe: name() })}>
        {(href) => (
          <a class={styles.link} href={href()} target="_blank" rel="noreferrer">
            {m.dependency_see({ recipe: name() })}
          </a>
        )}
      </Show>
    </span>
  )
}

export function MediaDetails(props: { asset: RecipeAsset; class?: string }) {
  const license = () => props.asset.license
  const licenseURL = () => safeSourceURL(license()?.url)
  const sourceURL = () => safeSourceURL(props.asset.source_url)
  return (
    <Show when={hasProvenance(props.asset)}>
      <Popover
        title={m.media_details_title()}
        positioning={{ placement: "bottom-end", gutter: 6 }}
        trigger={(triggerProps) => (
          <IconButton
            {...triggerProps()}
            class={props.class}
            aria-label={m.media_details_title()}
            size="xs"
            variant="plain"
          >
            <Info aria-hidden="true" size={14} />
          </IconButton>
        )}
      >
        <dl class={styles.details}>
          <Show when={props.asset.credit}>
            <dt class={styles.detailTerm}>{m.media_details_credit()}</dt>
            <dd class={styles.detailValue}>{props.asset.credit}</dd>
          </Show>
          <Show when={props.asset.creator}>
            <dt class={styles.detailTerm}>{m.media_details_creator()}</dt>
            <dd class={styles.detailValue}>{props.asset.creator}</dd>
          </Show>
          <Show when={props.asset.rights}>
            <dt class={styles.detailTerm}>{m.media_details_rights()}</dt>
            <dd class={styles.detailValue}>{props.asset.rights}</dd>
          </Show>
          <dt class={styles.detailTerm}>{m.media_details_license()}</dt>
          <dd class={styles.detailValue}>
            <Show when={license()} fallback={m.media_details_license_unknown()}>
              <Show when={licenseURL()} fallback={license()?.name}>
                {(href) => (
                  <a class={styles.link} href={href()} target="_blank" rel="noreferrer">
                    {license()?.name || href()}
                  </a>
                )}
              </Show>
            </Show>
          </dd>
          <Show when={sourceURL()}>
            {(href) => (
              <>
                <dt class={styles.detailTerm}>{m.media_details_source()}</dt>
                <dd class={styles.detailValue}>
                  <a class={styles.link} href={href()} target="_blank" rel="noreferrer">
                    {new URL(href()).host}
                  </a>
                </dd>
              </>
            )}
          </Show>
        </dl>
      </Popover>
    </Show>
  )
}
