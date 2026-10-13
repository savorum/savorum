import { Link } from "@tanstack/solid-router"
import { ArrowLeft } from "lucide-solid"
import { Show } from "solid-js"

import { directionalIcon } from "@/components/ui/directional"
import { PrintWorkspace } from "@/features/printing/print-workspace"
import { measurementSystem } from "@/lib/preferences"
import {
  type PrintSearch,
  type PrintSettings,
  settingsFromSearch,
  toRequestOptions,
} from "@/lib/print-options"

import { m } from "../../lib/messages.ts"

/**
 * The print workspace for a selection of recipes. It is the same workspace one
 * recipe uses, because printing several recipes is the same job.
 */
export function SelectionPrintPage(props: {
  search: () => PrintSearch
  onSettingsChange: (next: Partial<PrintSettings>) => void
}) {
  const recipes = () => props.search().recipes ?? []
  const settings = (): PrintSettings => settingsFromSearch(props.search())

  // Each recipe keeps its own remembered batch, so no scale is passed here.
  const presentation = () => ({ measurement: measurementSystem() })

  return (
    <Show
      when={recipes().length > 0}
      fallback={
        <p style={{ padding: "2rem", "text-align": "center" }}>
          <Link to="/">{m.selection_print_page_choose_some_recipes_to_print()}</Link>
        </p>
      }
    >
      <PrintWorkspace
        title={m.selection_print_page_recipes({ count: recipes().length })}
        backTo={
          <Link to="/" style={{ display: "inline-flex", "align-items": "center", gap: "0.5rem" }}>
            <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />{" "}
            {m.selection_print_page_recipes_heading()}{" "}
          </Link>
        }
        source={{ recipes: recipes() }}
        settings={settings()}
        onSettingsChange={props.onSettingsChange}
        presentation={presentation()}
        showScale={false}
        request={() => toRequestOptions(settings(), presentation())}
      />
    </Show>
  )
}
