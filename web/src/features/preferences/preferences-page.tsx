import { createSignal, For, onCleanup, onMount, Show } from "solid-js"
import { css, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import {
  type InterfaceLocale,
  readPreferences,
  replacePreferences,
  type PreferenceValues,
} from "@/api/preferences"
import { Notice, Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { authEnabled } from "@/features/account/account-state"
import { languageName } from "@/lib/direction"
import { languageChoices, recipeLanguageChoices } from "@/lib/languages"
import { formatNumber, presentationContext } from "@/lib/locale-format"
import { applyPreferences } from "@/lib/preferences"

import { loadMessages, m } from "../../lib/messages.ts"

const layout = pageShell({ width: "list" })

interface ChoiceField {
  key:
    | "measurement"
    | "temperature"
    | "print_page_size"
    | "print_image"
    | "recipe_view"
    | "reduced_motion"
    | "appearance"
  label: string
  options: readonly (readonly [string, () => string])[]
}

/** The settings, grouped by occasion: reading, cooking, and printing. */
const readingFields: ChoiceField[] = [
  {
    key: "measurement",
    get label() {
      return m.preferences_page_measurement_presentation()
    },
    options: [
      ["original", () => m.preferences_page_as_authored()],
      ["metric", () => m.preferences_metric()],
      ["us", () => m.preferences_page_united_states()],
      ["au", () => m.preferences_australia()],
    ],
  },
  {
    key: "temperature",
    get label() {
      return m.preferences_page_temperature_display()
    },
    options: [
      ["original", () => m.preferences_page_as_authored()],
      ["celsius", () => m.preferences_celsius()],
      ["fahrenheit", () => m.preferences_fahrenheit()],
    ],
  },
  {
    key: "recipe_view",
    get label() {
      return m.preferences_page_recipe_spacing()
    },
    options: [
      ["comfortable", () => m.preferences_comfortable()],
      ["compact", () => m.preferences_compact()],
    ],
  },
]

const printingFields: ChoiceField[] = [
  {
    key: "print_page_size",
    get label() {
      return m.preferences_page_default_print_paper()
    },
    options: [
      ["automatic", () => m.preferences_automatic()],
      ["a4", () => m.preferences_a4()],
      ["letter", () => m.preferences_letter()],
    ],
  },
  {
    key: "print_image",
    get label() {
      return m.preferences_page_default_print_images()
    },
    options: [
      ["automatic", () => m.preferences_automatic()],
      ["include", () => m.preferences_include()],
      ["omit", () => m.preferences_omit()],
    ],
  },
]

const appearanceFields: ChoiceField[] = [
  {
    key: "appearance",
    get label() {
      return m.preferences_page_color_scheme()
    },
    options: [
      ["system", () => m.preferences_page_follow_device_setting()],
      ["light", () => m.preferences_light()],
      ["dark", () => m.preferences_dark()],
    ],
  },
]

const accessibilityFields: ChoiceField[] = [
  {
    key: "reduced_motion",
    get label() {
      return m.preferences_page_motion()
    },
    options: [
      ["system", () => m.preferences_page_follow_device_setting()],
      ["reduce", () => m.preferences_page_reduce_motion()],
    ],
  },
]

export function PreferencesPage() {
  const [draft, setDraft] = createSignal<PreferenceValues>({})
  const [effective, setEffective] = createSignal<PreferenceValues>({})
  const [locales, setLocales] = createSignal<InterfaceLocale[]>([
    { tag: "en", status: "supported" },
  ])
  const [revision, setRevision] = createSignal(0)
  const [ready, setReady] = createSignal(false)
  const [saving, setSaving] = createSignal(false)
  const [notice, setNotice] = createSignal("")
  const [problem, setProblem] = createSignal("")
  let active = true
  onCleanup(() => {
    active = false
  })
  onMount(() => {
    void readPreferences()
      .then((value) => {
        if (!active) return
        setDraft(value.overrides)
        setEffective(value.effective)
        if (value.interface_locales?.length) setLocales(value.interface_locales)
        setRevision(value.revision)
        setReady(true)
      })
      .catch(() => setProblem(m.preferences_page_could_not_load_your_preferences()))
  })
  async function save(event: SubmitEvent) {
    event.preventDefault()
    setSaving(true)
    setNotice("")
    setProblem("")
    try {
      const value = await replacePreferences(revision(), draft())
      if (!active) return
      setRevision(value.revision)
      setDraft(value.overrides)
      setEffective(value.effective)
      await loadMessages(value.effective.ui_locale)
      if (!active) return
      applyPreferences(value)
      setNotice(m.preferences_page_preferences_saved())
    } catch {
      setProblem(m.preferences_page_could_not_save_preferences_reload_if_they_changed_on_an())
    } finally {
      setSaving(false)
    }
  }
  /** Names the inherited default, once the server has resolved it. */
  function inheritedLabel(field: ChoiceField): string {
    const value = effective()[field.key]
    const named = field.options.find(([option]) => option === value)
    return named
      ? m.preferences_default_is({ value1: named[1]() })
      : m.preferences_page_use_default()
  }
  function ChoiceControl(props: { field: ChoiceField }) {
    return (
      <Select
        label={props.field.label}
        value={draft()[props.field.key] ?? ""}
        onValueChange={(value) => setDraft({ ...draft(), [props.field.key]: value })}
        items={[
          { value: "", label: inheritedLabel(props.field) },
          ...props.field.options.map(([value, label]) => ({ value, label: label() })),
        ]}
      />
    )
  }
  /**
   * Supported interface languages, then previews marked as such. A stored
   * choice this build does not ship stays visible so saving keeps it.
   */
  const interfaceLocaleChoices = () => {
    const shipped = locales()
    const current = draft().ui_locale
    const supported = shipped.filter((locale) => locale.status === "supported")
    const preview = shipped.filter((locale) => locale.status === "preview")
    return [
      { value: "", label: m.preferences_page_use_automatic_default() },
      ...languageChoices(supported.map((locale) => locale.tag)),
      ...languageChoices(preview.map((locale) => locale.tag)).map((choice) => ({
        value: choice.value,
        label: m.preferences_interface_preview({ language: choice.label }),
      })),
      ...(current && !shipped.map((locale) => locale.tag).includes(current)
        ? [
            {
              value: current,
              label: m.preferences_catalog_unavailable({
                language: languageName(current, presentationContext().locale),
              }),
            },
          ]
        : []),
    ]
  }
  function RecipeLanguageChoice(props: { rank: number }) {
    return (
      <Select
        label={
          props.rank === 0
            ? m.preferences_page_preferred_recipe_language()
            : m.preferences_page_recipe_language_fallback({
                value1: formatNumber(props.rank),
              })
        }
        value={draft().content_languages?.[props.rank] ?? ""}
        onValueChange={(value) => {
          const next = [...(draft().content_languages ?? [])]
          next[props.rank] = value
          setDraft({ ...draft(), content_languages: next.filter(Boolean) })
        }}
        items={[
          {
            value: "",
            label: props.rank === 0 ? m.preferences_page_use_default() : m.preferences_none(),
          },
          ...recipeLanguageChoices(draft().content_languages?.[props.rank]),
        ]}
      />
    )
  }
  return (
    <main
      class={cx(
        layout.page,
        css({
          "& fieldset": { display: "grid", gap: "4", marginBlock: "6", minWidth: "0" },
          "& legend": { fontSize: "lg", fontWeight: "semibold", marginBottom: "4" },
        }),
      )}
    >
      <h1 class={layout.title}>{m.preferences_page_your_preferences()}</h1>
      {/* Local mode has one shared account, so these settings are not personal. */}
      <p class={css({ color: "fg.muted", marginTop: "2" })}>
        <Show when={authEnabled()} fallback={m.preferences_shared_scope()}>
          {m.preferences_account_scope()}
        </Show>
      </p>
      <Notice message={notice()} />
      <Problem message={problem()} />
      <Show when={ready()}>
        <form
          onSubmit={(event) => {
            void save(event)
          }}
        >
          <fieldset disabled={saving()}>
            <legend>{m.preferences_page_reading()}</legend>
            <For each={readingFields}>{(field) => <ChoiceControl field={field} />}</For>
          </fieldset>
          <fieldset disabled={saving()}>
            <legend>{m.preferences_page_cooking()}</legend>
            <FormField
              label={`${m.preferences_page_microwave_output()} (W)`}
              hint={m.preferences_page_microwave_output_hint()}
            >
              <Input
                type="number"
                inputmode="numeric"
                min="100"
                max="5000"
                step="1"
                value={draft().microwave_output_watts || ""}
                placeholder={m.preferences_page_microwave_output_placeholder()}
                onInput={(event) =>
                  setDraft({
                    ...draft(),
                    microwave_output_watts: event.currentTarget.valueAsNumber || undefined,
                  })
                }
              />
            </FormField>
          </fieldset>
          <fieldset disabled={saving()}>
            <legend>{m.preferences_page_printing()}</legend>
            <For each={printingFields}>{(field) => <ChoiceControl field={field} />}</For>
          </fieldset>
          <fieldset disabled={saving()}>
            <legend>{m.preferences_page_language_and_region()}</legend>
            <Select
              label={m.preferences_page_interface_language()}
              value={draft().ui_locale ?? ""}
              onValueChange={(value) => setDraft({ ...draft(), ui_locale: value })}
              items={interfaceLocaleChoices()}
            />
            <Show when={locales().some((locale) => locale.status === "preview")}>
              <p class={css({ color: "fg.muted", fontSize: "xs", margin: "0" })}>
                {m.preferences_preview_language_hint()}
              </p>
            </Show>
            <RecipeLanguageChoice rank={0} />
            <Disclosure label={m.preferences_language_fallbacks()}>
              <div class={css({ display: "grid", gap: "4" })}>
                <For each={[1, 2]}>{(rank) => <RecipeLanguageChoice rank={rank} />}</For>
              </div>
            </Disclosure>
            <FormField label={m.preferences_page_timezone()} hint={m.preferences_timezone_hint()}>
              <Input
                autocomplete="off"
                spellcheck={false}
                value={draft().timezone ?? ""}
                placeholder={m.preferences_page_use_default_utc()}
                list="preference-timezones"
                onInput={(event) => setDraft({ ...draft(), timezone: event.currentTarget.value })}
              />
            </FormField>
            <datalist id="preference-timezones">
              <For each={Intl.supportedValuesOf("timeZone")}>
                {(zone) => <option value={zone} />}
              </For>
            </datalist>
          </fieldset>
          <fieldset disabled={saving()}>
            <legend>{m.preferences_page_appearance()}</legend>
            <For each={appearanceFields}>{(field) => <ChoiceControl field={field} />}</For>
          </fieldset>
          <fieldset disabled={saving()}>
            <legend>{m.preferences_page_accessibility()}</legend>
            <For each={accessibilityFields}>{(field) => <ChoiceControl field={field} />}</For>
          </fieldset>
          <Button type="submit" loading={saving()} loadingText={m.preferences_saving()}>
            {m.preferences_page_save_preferences()}
          </Button>
        </form>
      </Show>
    </main>
  )
}
