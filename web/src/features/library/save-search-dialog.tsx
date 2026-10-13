import { Show, untrack } from "solid-js"
import { css } from "styled-system/css"

import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"

import { m } from "../../lib/messages.ts"
import type { LibraryQuery } from "./create-library-query"

export function SaveSearchDialog(props: {
  query: Pick<
    LibraryQuery,
    "namingSearch" | "setNamingSearch" | "savingSearch" | "saveSearchError" | "saveSearch"
  >
}) {
  const { namingSearch, setNamingSearch, savingSearch, saveSearchError, saveSearch } = untrack(
    () => props.query,
  )

  return (
    <>
      <Show when={namingSearch() !== undefined}>
        <Dialog
          open
          onClose={() => setNamingSearch(undefined)}
          title={m.save_search_dialog_save_this_search()}
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
                loading={savingSearch()}
                disabled={(namingSearch() ?? "").trim() === ""}
                onClick={() => void saveSearch(namingSearch() ?? "")}
              >
                {m.save_search_dialog_save()}
              </Button>
            </>
          }
        >
          <div class={styles.saveSearchFields}>
            <ChangeError error={saveSearchError()} />
            <Input
              aria-label={m.save_search_dialog_name_for_this_search()}
              placeholder={m.save_search_dialog_weeknight_favourites()}
              value={namingSearch() ?? ""}
              onInput={(event) => setNamingSearch(event.currentTarget.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.isComposing && event.keyCode !== 229) {
                  event.preventDefault()
                  void saveSearch(namingSearch() ?? "")
                }
              }}
            />
          </div>
        </Dialog>
      </Show>
    </>
  )
}
const styles = {
  saveSearchFields: css({ display: "grid", gap: "3", marginTop: "4" }),
}
