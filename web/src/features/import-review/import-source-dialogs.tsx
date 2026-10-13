import { Show, untrack } from "solid-js"
import { css } from "styled-system/css"

import { ChangeError } from "@/components/status"
import { TextImport } from "@/components/text-import"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"

import { m } from "../../lib/messages.ts"
import type { ImportWorkflow } from "./create-import-workflow"

export function ImportSourceDialogs(props: {
  workflow: Pick<
    ImportWorkflow,
    | "previewing"
    | "urlImportOpen"
    | "setURLImportOpen"
    | "textImportOpen"
    | "setTextImportOpen"
    | "editingTextItem"
    | "setEditingTextItem"
    | "urlImport"
    | "setURLImport"
    | "urlImportError"
    | "setURLImportError"
    | "stageImportFiles"
    | "stageImportURL"
    | "applyEditedText"
    | "cancelStaging"
  >
}) {
  const {
    previewing,
    urlImportOpen,
    setURLImportOpen,
    textImportOpen,
    setTextImportOpen,
    editingTextItem,
    setEditingTextItem,
    urlImport,
    setURLImport,
    urlImportError,
    setURLImportError,
    stageImportFiles,
    stageImportURL,
    applyEditedText,
    cancelStaging,
  } = untrack(() => props.workflow)

  return (
    <>
      <Show when={urlImportOpen()}>
        <Dialog
          open
          onClose={() => {
            cancelStaging()
            setURLImportOpen(false)
            setURLImportError(undefined)
          }}
          title={m.import_source_dialogs_import_a_recipe_from_the_web()}
          footer={
            <>
              <DialogClose>
                {(closeProps) => (
                  <Button {...closeProps()} variant="outline">
                    {m.common_cancel()}
                  </Button>
                )}
              </DialogClose>
              <Button
                colorPalette="accent"
                loading={previewing()}
                disabled={urlImport().trim() === ""}
                onClick={() => void stageImportURL()}
              >
                {m.common_preview_recipe()}
              </Button>
            </>
          }
        >
          <div class={styles.urlImportFields}>
            <Show when={urlImportError()}>
              <ChangeError error={urlImportError()} />
            </Show>
            <Input
              type="url"
              aria-label={m.import_source_dialogs_recipe_url()}
              placeholder={m.import_source_dialogs_https_example_com_recipes()}
              value={urlImport()}
              onInput={(event) => {
                setURLImport(event.currentTarget.value)
                setURLImportError(undefined)
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.isComposing && event.keyCode !== 229) {
                  event.preventDefault()
                  void stageImportURL()
                }
              }}
            />
          </div>
        </Dialog>
      </Show>
      <Show when={textImportOpen()}>
        <Dialog
          open
          onClose={() => setTextImportOpen(false)}
          title={m.import_source_dialogs_import_recipe_text()}
        >
          <TextImport
            onPreview={(files) => {
              setTextImportOpen(false)
              return stageImportFiles(files)
            }}
            cancel={
              <DialogClose>
                {(closeProps) => (
                  <Button {...closeProps()} size="sm" variant="plain">
                    {m.common_cancel()}
                  </Button>
                )}
              </DialogClose>
            }
          />
        </Dialog>
      </Show>
      <Show when={editingTextItem()}>
        {(item) => (
          <Dialog
            open
            onClose={() => setEditingTextItem(undefined)}
            title={m.import_source_dialogs_edit_recipe_text()}
          >
            <TextImport
              initialText={item().source.extracted_text ?? ""}
              submitLabel={m.import_source_dialogs_update_preview()}
              onPreview={applyEditedText}
              cancel={
                <DialogClose>
                  {(closeProps) => (
                    <Button {...closeProps()} size="sm" variant="plain">
                      {m.common_cancel()}
                    </Button>
                  )}
                </DialogClose>
              }
            />
          </Dialog>
        )}
      </Show>
    </>
  )
}
const styles = {
  urlImportFields: css({ display: "grid", gap: "3", marginTop: "4" }),
}
