import { type JSX, Show, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"

import { m } from "../lib/messages.ts"

export interface TextImportProps {
  onPreview: (files: readonly File[]) => void | Promise<void>
  initialText?: string
  submitLabel?: string
  /** Rendered beside the actions so a surface that can be left says so. */
  cancel?: JSX.Element
}

const styles = {
  panel: css({ display: "grid", gap: "3", marginTop: "4" }),
  text: css({ minHeight: "16rem" }),
  actions: css({ display: "flex", flexWrap: "wrap", gap: "2" }),
  file: css({ display: "none" }),
}

/** Editable plain-text staging surface. The textarea keeps its DOM identity
 * while typing so paste, caret movement, and IME composition are preserved. */
export function TextImport(props: TextImportProps) {
  const [source, setSource] = createSignal(props.initialText ?? "")
  const [reading, setReading] = createSignal(false)
  const [submitting, setSubmitting] = createSignal(false)
  const [error, setError] = createSignal("")
  let editVersion = 0
  let fileInput: HTMLInputElement | undefined
  const previewText = async () => {
    const value = source()
    if (!value.trim() || submitting()) return
    setSubmitting(true)
    setError("")
    try {
      await props.onPreview([new File([value], "pasted-recipe.txt", { type: "text/plain" })])
    } catch {
      setError(m.text_import_the_recipe_could_not_be_previewed_try_again())
    } finally {
      setSubmitting(false)
    }
  }
  const chooseFile = (event: Event) => {
    const files = (event.currentTarget as HTMLInputElement).files
    const file = files?.[0]
    if (file) {
      if (file.size > 8 * 1024 * 1024) {
        setError(m.text_import_choose_a_text_file_smaller_than_8_mb())
        return
      }
      setReading(true)
      setError("")
      const version = ++editVersion
      void file
        .text()
        .then((text) => {
          if (version === editVersion) setSource(text)
        })
        .catch(() => setError(m.text_import_the_text_file_could_not_be_read()))
        .finally(() => setReading(false))
    }
    if (fileInput) fileInput.value = ""
  }
  return (
    <section class={styles.panel} aria-label={m.text_import_import_plain_text_recipe()}>
      <label for="plain-text-recipe">{m.text_import_paste_or_edit_recipe_text()}</label>
      <Textarea
        id="plain-text-recipe"
        class={styles.text}
        value={source()}
        onInput={(event) => {
          editVersion += 1
          setSource(event.currentTarget.value)
        }}
        placeholder={m.text_import_recipe_template_placeholder()}
      />
      <Problem message={error()} />
      <div class={styles.actions}>
        <Button
          size="sm"
          onClick={() => void previewText()}
          loading={submitting()}
          disabled={reading() || source().trim() === ""}
        >
          {props.submitLabel ?? m.text_import_preview_pasted_recipe()}
        </Button>
        <Button size="sm" variant="outline" loading={reading()} onClick={() => fileInput?.click()}>
          {m.text_import_load_txt_file()}
        </Button>
        <input
          ref={(node) => {
            fileInput = node
          }}
          class={styles.file}
          type="file"
          accept=".txt,text/plain"
          onChange={chooseFile}
        />
        <Show when={props.cancel}>{(cancel) => cancel()}</Show>
      </div>
    </section>
  )
}
