import { Show, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { CopyField } from "@/components/ui/clipboard"
import { Disclosure } from "@/components/ui/disclosure"
import { Input } from "@/components/ui/input"
import { capturedRecipeURL } from "@/lib/capture"

import { m } from "../../lib/messages.ts"

export function CaptureForm(props: {
  initial: { url?: string; text?: string }
  onPreview: (url: string) => void
}) {
  const [url, setURL] = createSignal(
    capturedRecipeURL(props.initial.url) || capturedRecipeURL(props.initial.text),
  )
  const [error, setError] = createSignal("")
  /** Whether this device has a bookmarks bar to drag a link to. */
  const canDrag = () =>
    typeof window !== "undefined" && window.matchMedia("(hover: hover) and (pointer: fine)").matches
  const bookmarklet = () => {
    const destination =
      typeof window === "undefined" ? "/capture?url=" : `${window.location.origin}/capture?url=`
    return `javascript:(()=>{window.open(${JSON.stringify(destination)}+encodeURIComponent(location.href),'_blank','noopener')})()`
  }
  return (
    <main class={css({ marginInline: "auto", maxWidth: "48rem", padding: "8" })}>
      <h1 class={css({ fontFamily: "editorial", fontSize: "3xl", marginBottom: "6" })}>
        {" "}
        {m.capture_page_save_a_recipe()}{" "}
      </h1>
      <form
        class={css({ display: "grid", gap: "3" })}
        onSubmit={(event) => {
          event.preventDefault()
          const captured = capturedRecipeURL(url())
          if (!captured) {
            setError(m.capture_page_enter_a_recipe_link_beginning_with_http_or_https())
            return
          }
          props.onPreview(captured)
        }}
      >
        <label class={css({ display: "grid", gap: "2" })}>
          {" "}
          {m.capture_page_recipe_url()}{" "}
          <Input
            value={url()}
            onInput={(event) => {
              setURL(event.currentTarget.value)
              setError("")
            }}
            required
          />
        </label>
        <Problem message={error()} />
        <Button colorPalette="accent" type="submit">
          {m.common_preview_recipe()}
        </Button>
      </form>
      <Disclosure
        class={css({ marginTop: "8" })}
        label={m.capture_page_save_recipes_from_your_browser()}
      >
        <Show
          when={canDrag()}
          fallback={
            <div class={css({ display: "grid", gap: "3", marginTop: "3" })}>
              <p class={css({ margin: "0" })}>{m.capture_page_touch_instructions()}</p>
              <CopyField
                label={m.capture_page_shortcut_address()}
                value={bookmarklet()}
                copyLabel={m.capture_page_copy_the_shortcut()}
                copiedLabel={m.capture_page_shortcut_copied()}
              />
            </div>
          }
        >
          <p class={css({ marginTop: "3" })}>
            {" "}
            {m.capture_page_drag()} <a href={bookmarklet()}>{m.capture_page_save_with_savorum()}</a>{" "}
            {m.capture_page_to_your_bookmarks_bar()}{" "}
          </p>
        </Show>
      </Disclosure>
    </main>
  )
}
