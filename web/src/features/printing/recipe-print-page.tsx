import { Link } from "@tanstack/solid-router"
import { ArrowLeft } from "lucide-solid"
import { createResource } from "solid-js"

import { getOrganization } from "@/api/organization"
import { getRecipe } from "@/api/recipes"
import { Loading, ResourceState } from "@/components/status"
import { directionalIcon } from "@/components/ui/directional"
import { PrintWorkspace } from "@/features/printing/print-workspace"
import { usePageTitle } from "@/lib/page-title"
import { measurementSystem, personalPreferences } from "@/lib/preferences"
import {
  type PrintSearch,
  type PrintSettings,
  settingsFromSearch,
  toRequestOptions,
} from "@/lib/print-options"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"

export function RecipePrintPage(props: {
  recipeId: () => string
  search: () => PrintSearch
  onSettingsChange: (next: Partial<PrintSettings>) => void
}) {
  const [entry, { refetch }] = createResource(() => props.recipeId(), getRecipe)
  const [organization] = createResource(
    () => props.recipeId(),
    (id) => getOrganization(id).catch(() => undefined),
  )

  const settings = (): PrintSettings => settingsFromSearch(props.search())
  usePageTitle(() =>
    entry.error || !entry.latest
      ? undefined
      : m.page_title_named({
          name: m.print_workspace_print_named({ value1: entry.latest.value.recipe.title }),
        }),
  )

  const presentation = () => ({
    scale: props.search().batch,
    measurement: measurementSystem(),
    microwaveOutputWatts: (settled(entry)?.value.recipe.times?.assertions ?? []).some(
      (timing) => timing.microwave,
    )
      ? personalPreferences().microwave_output_watts
      : undefined,
  })

  const preferred = () => {
    const state = settled(organization)?.value.user_state
    return Boolean(state?.preferred_yield ?? state?.preferred_scale)
  }

  return (
    <>
      <ResourceState
        resource={entry}
        loading={<Loading>{m.recipe_loading()}</Loading>}
        fallback={m.recipes_the_recipe_could_not_be_loaded()}
        onRetry={() => void refetch()}
      >
        {(loaded) => (
          <PrintWorkspace
            title={loaded().value.recipe.title}
            backTo={
              <Link
                to="/recipes/$recipeId"
                params={{ recipeId: props.recipeId() }}
                style={{ display: "inline-flex", "align-items": "center", gap: "0.5rem" }}
              >
                <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />
                {loaded().value.recipe.title}
              </Link>
            }
            source={{ recipes: [props.recipeId()] }}
            settings={settings()}
            onSettingsChange={props.onSettingsChange}
            presentation={presentation()}
            currentBatch={props.search().batch}
            hasPreferredBatch={preferred()}
            request={() => toRequestOptions(settings(), presentation())}
          />
        )}
      </ResourceState>
    </>
  )
}
