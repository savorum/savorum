import { Show, createSignal, onCleanup, onMount } from "solid-js"

import { type PreviewImage, fetchPreviewImage } from "@/api/import-jobs"

export interface ImportThumbnailProps {
  /** The address the preview offers for this recipe's leading photograph, or
   *  nothing when the recipe names none. */
  source?: string
  class?: string
}

/**
 * A preview photograph, fetched when the row comes into view. Its alternative
 * text is empty because the recipe's title is beside it.
 */
export function ImportThumbnail(props: ImportThumbnailProps) {
  const [image, setImage] = createSignal<PreviewImage>()
  let element: HTMLDivElement | undefined
  let controller: AbortController | undefined
  let objectURL: string | undefined

  const load = () => {
    const source = props.source
    if (!source || image() || controller) return
    controller = new AbortController()
    fetchPreviewImage(source, controller.signal)
      .then((result) => {
        if (result.url) objectURL = result.url
        setImage(result)
      })
      .catch((error: unknown) => {
        // A photograph that will not load leaves the placeholder as it is.
        // Whether one could be had at all is already stated in the row's facts.
        if (error instanceof DOMException && error.name === "AbortError") return
      })
      .finally(() => {
        controller = undefined
      })
  }

  onMount(() => {
    if (!props.source) return
    if (typeof IntersectionObserver !== "function" || !element) {
      load()
      return
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) return
        observer.disconnect()
        load()
      },
      { rootMargin: "200px" },
    )
    observer.observe(element)
    onCleanup(() => observer.disconnect())
  })

  onCleanup(() => {
    controller?.abort()
    if (objectURL) URL.revokeObjectURL(objectURL)
  })

  // A picture or a blank space; availability is stated in the row's facts.
  return (
    <div
      ref={(node) => {
        element = node
      }}
      class={props.class}
      data-import-thumbnail={image()?.state ?? "loading"}
    >
      <Show when={image()?.state === "available" && image()?.url}>
        {(url) => (
          <img
            src={url()}
            alt=""
            style={{ height: "100%", width: "100%", "object-fit": "cover" }}
          />
        )}
      </Show>
    </div>
  )
}
