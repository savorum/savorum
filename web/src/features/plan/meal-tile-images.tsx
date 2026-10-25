import { UtensilsCrossed } from "lucide-solid"
import { For, Show } from "solid-js"

import { dishImages, tiles } from "./meal-tiles"

/** Up to three recipe photographs, overlapped; a plain mark when there are none. */
export function TileImages(props: { dishes: { recipe_image_url?: string | null }[] }) {
  return (
    <Show
      when={dishImages(props.dishes).length}
      fallback={
        <span class={tiles.placeholder}>
          <UtensilsCrossed aria-hidden="true" size={18} />
        </span>
      }
    >
      <div class={tiles.thumbs}>
        <For each={dishImages(props.dishes)}>
          {(src) => (
            <img class={tiles.thumb} src={`${src}/derivatives/thumb`} alt="" loading="lazy" />
          )}
        </For>
      </div>
    </Show>
  )
}
