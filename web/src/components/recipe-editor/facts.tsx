import { Plus, X } from "lucide-solid"
import { Index, Show } from "solid-js"
import type { SetStoreFunction } from "solid-js/store"
import { css } from "styled-system/css"

import type { AllergenDeclaration, DietaryClaim, Recipe } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/disclosure"
import { FieldGroup, FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { removeAt } from "@/lib/lists"
import { type Diet, type Presence, dietName, presenceLabel } from "@/lib/recipe-facts"

import { m } from "../../lib/messages.ts"

const styles = {
  block: css({ display: "grid", gap: "6", paddingBottom: "4" }),
  list: css({ display: "grid", gap: "3" }),
  statement: css({
    background: "bg.subtle",
    borderColor: "border.subtle",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "3",
    padding: "3",
  }),
  wording: css({
    alignItems: "end",
    display: "grid",
    gap: "2",
    gridTemplateColumns: "minmax(0, 1fr) auto",
  }),
  fields: css({
    alignItems: "start",
    display: "grid",
    gap: "3",
    gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 12rem), 1fr))",
  }),
}

const diets: Diet[] = [
  "vegan",
  "vegetarian",
  "gluten_free",
  "halal",
  "kosher",
  "hindu",
  "diabetic",
  "low_calorie",
  "low_fat",
  "low_lactose",
  "low_salt",
]

/** Choices for scoping a claim to one variant; unnamed variants get an ID on use. */
function variantChoices(recipe: Recipe) {
  return [
    { value: "", label: m.claims_editor_whole_recipe() },
    ...(recipe.variants ?? []).map((variant, index) => ({
      value: variant.id ?? `#${index}`,
      label: variant.title?.trim() || m.claims_variant_number({ number: index + 1 }),
    })),
  ]
}

function scopeTo(
  recipe: Recipe,
  setDraft: SetStoreFunction<Recipe>,
  value: string,
): string | undefined {
  if (!value) return undefined
  if (!value.startsWith("#")) return value
  const index = Number(value.slice(1))
  const used = new Set((recipe.variants ?? []).map((variant) => variant.id).filter(Boolean))
  let id = `variant-${index + 1}`
  for (let suffix = 2; used.has(id); suffix++) id = `variant-${index + 1}-${suffix}`
  setDraft("variants", index, "id", id)
  return id
}

export function RecipeClaimsEditor(props: { draft: Recipe; setDraft: SetStoreFunction<Recipe> }) {
  const claims = () => props.draft.dietary_claims ?? []
  const declarations = () => props.draft.allergen_declarations ?? []
  const setClaim = (index: number, claim: DietaryClaim) =>
    props.setDraft(
      "dietary_claims",
      claims().map((current, at) => (at === index ? claim : current)),
    )
  const setDeclaration = (index: number, declaration: AllergenDeclaration) =>
    props.setDraft(
      "allergen_declarations",
      declarations().map((current, at) => (at === index ? declaration : current)),
    )
  const presences: Presence[] = ["contains", "may_contain", "free_from"]
  return (
    <Disclosure
      label={m.claims_editor_title()}
      hint={[...claims().map((claim) => claim.text), ...declarations().map((item) => item.text)]
        .filter(Boolean)
        .join(" · ")}
    >
      <div class={styles.block}>
        <FieldGroup label={m.claims_editor_dietary()}>
          <div class={styles.list}>
            <Index each={claims()}>
              {(claim, index) => (
                <div class={styles.statement}>
                  <div class={styles.wording}>
                    <FormField label={m.claims_editor_statement()}>
                      <Input
                        size="sm"
                        placeholder={m.claims_editor_dietary_placeholder()}
                        value={claim().text}
                        onInput={(event) =>
                          setClaim(index, { ...claim(), text: event.currentTarget.value })
                        }
                      />
                    </FormField>
                    <IconButton
                      aria-label={m.claims_editor_remove()}
                      size="xs"
                      variant="plain"
                      onClick={() => props.setDraft("dietary_claims", removeAt(claims(), index))}
                    >
                      <X aria-hidden="true" size={14} />
                    </IconButton>
                  </div>
                  <div class={styles.fields}>
                    <FormField label={m.claims_editor_suitability()}>
                      <Select
                        size="sm"
                        value={claim().suitability}
                        onValueChange={(value) =>
                          setClaim(index, {
                            ...claim(),
                            suitability: value as DietaryClaim["suitability"],
                          })
                        }
                        items={[
                          { value: "suitable", label: m.claims_editor_suitable() },
                          { value: "unsuitable", label: m.claims_editor_unsuitable() },
                        ]}
                      />
                    </FormField>
                    <FormField label={m.claims_editor_diet()}>
                      <Select
                        size="sm"
                        value={claim().diet ?? ""}
                        onValueChange={(value) =>
                          setClaim(index, { ...claim(), diet: (value || undefined) as Diet })
                        }
                        items={[
                          { value: "", label: m.claims_editor_diet_other() },
                          ...diets.map((diet) => ({ value: diet, label: dietName(diet) })),
                        ]}
                      />
                    </FormField>
                    <FormField label={m.claims_editor_condition()}>
                      <Input
                        size="sm"
                        placeholder={m.claims_editor_condition_placeholder()}
                        value={claim().condition ?? ""}
                        onInput={(event) =>
                          setClaim(index, { ...claim(), condition: event.currentTarget.value })
                        }
                      />
                    </FormField>
                    <Show when={props.draft.variants?.length}>
                      <FormField label={m.claims_editor_applies_to()}>
                        <Select
                          size="sm"
                          value={claim().variant_ref ?? ""}
                          onValueChange={(value) =>
                            setClaim(index, {
                              ...claim(),
                              variant_ref: scopeTo(props.draft, props.setDraft, value),
                            })
                          }
                          items={variantChoices(props.draft)}
                        />
                      </FormField>
                    </Show>
                  </div>
                </div>
              )}
            </Index>
          </div>
          <div>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                props.setDraft("dietary_claims", [
                  ...claims(),
                  { text: "", suitability: "suitable" },
                ])
              }
            >
              <Plus aria-hidden="true" size={14} />
              {m.claims_editor_add_dietary()}
            </Button>
          </div>
        </FieldGroup>

        <FieldGroup label={m.claims_editor_allergens()}>
          <div class={styles.list}>
            <Index each={declarations()}>
              {(declaration, index) => (
                <div class={styles.statement}>
                  <div class={styles.wording}>
                    <FormField label={m.claims_editor_statement()}>
                      <Input
                        size="sm"
                        placeholder={m.claims_editor_allergen_placeholder()}
                        value={declaration().text}
                        onInput={(event) =>
                          setDeclaration(index, {
                            ...declaration(),
                            text: event.currentTarget.value,
                          })
                        }
                      />
                    </FormField>
                    <IconButton
                      aria-label={m.claims_editor_remove()}
                      size="xs"
                      variant="plain"
                      onClick={() =>
                        props.setDraft("allergen_declarations", removeAt(declarations(), index))
                      }
                    >
                      <X aria-hidden="true" size={14} />
                    </IconButton>
                  </div>
                  <div class={styles.fields}>
                    <FormField label={m.claims_editor_presence()}>
                      <Select
                        size="sm"
                        value={declaration().presence}
                        onValueChange={(value) =>
                          setDeclaration(index, { ...declaration(), presence: value as Presence })
                        }
                        items={presences.map((presence) => ({
                          value: presence,
                          label: presenceLabel(presence),
                        }))}
                      />
                    </FormField>
                    <FormField label={m.claims_editor_substance()}>
                      <Input
                        size="sm"
                        value={declaration().substance}
                        onInput={(event) =>
                          setDeclaration(index, {
                            ...declaration(),
                            substance: event.currentTarget.value,
                          })
                        }
                      />
                    </FormField>
                    <FormField label={m.claims_editor_condition()}>
                      <Input
                        size="sm"
                        value={declaration().condition ?? ""}
                        onInput={(event) =>
                          setDeclaration(index, {
                            ...declaration(),
                            condition: event.currentTarget.value,
                          })
                        }
                      />
                    </FormField>
                    <Show when={props.draft.variants?.length}>
                      <FormField label={m.claims_editor_applies_to()}>
                        <Select
                          size="sm"
                          value={declaration().variant_ref ?? ""}
                          onValueChange={(value) =>
                            setDeclaration(index, {
                              ...declaration(),
                              variant_ref: scopeTo(props.draft, props.setDraft, value),
                            })
                          }
                          items={variantChoices(props.draft)}
                        />
                      </FormField>
                    </Show>
                  </div>
                </div>
              )}
            </Index>
          </div>
          <div>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                props.setDraft("allergen_declarations", [
                  ...declarations(),
                  { text: "", substance: "", presence: "contains" },
                ])
              }
            >
              <Plus aria-hidden="true" size={14} />
              {m.claims_editor_add_allergen()}
            </Button>
          </div>
        </FieldGroup>
      </div>
    </Disclosure>
  )
}
