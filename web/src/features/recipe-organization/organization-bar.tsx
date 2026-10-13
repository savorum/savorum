import { Heart } from "lucide-solid"
import { Show, createSignal, untrack } from "solid-js"
import { css, cva, cx } from "styled-system/css"
import { mark } from "styled-system/recipes"

import { IconButton } from "@/components/ui/icon-button"
import { Rating } from "@/components/ui/rating"
import type { OrganizationController } from "@/features/recipe-organization/create-organization-controller"

import { m } from "../../lib/messages.ts"
const quietly = (result: Promise<void>) => void result.catch(() => {})

/** The favourite toggle, placed beside Cook. */
export const FavoriteMark = (props: { controller: OrganizationController }) => {
  const state = () => props.controller.value()?.user_state
  return (
    <Show when={state()}>
      {(userState) => (
        <IconButton
          aria-label={
            userState().favorite
              ? m.organization_bar_remove_from_favorites()
              : m.organization_bar_add_to_favorites()
          }
          aria-pressed={userState().favorite}
          size="sm"
          variant="plain"
          class={cx(mark(), styles.heart({ on: userState().favorite }))}
          onClick={() =>
            quietly(
              props.controller.update((input) =>
                untrack(() => ({
                  ...input,
                  user_state: {
                    ...input.user_state,
                    favorite: !input.user_state.favorite,
                  },
                })),
              ),
            )
          }
        >
          <Heart
            aria-hidden="true"
            size={17}
            fill={userState().favorite ? "currentColor" : "none"}
          />
        </IconButton>
      )}
    </Show>
  )
}

/** What this cook thought of the recipe, shown with the cooking log. */
export const RecipeRating = (props: { controller: OrganizationController }) => {
  const [pendingRating, setPendingRating] = createSignal<{ value: number | undefined }>()
  const state = () => props.controller.value()?.user_state
  return (
    <Show when={state()}>
      {(userState) => (
        <div class={styles.bar}>
          {/* The rating group already carries this as its accessible name. */}
          <span class={styles.ratingLabel} aria-hidden="true">
            {m.organization_bar_your_rating()}
          </span>
          <Rating
            label={m.organization_bar_your_rating()}
            size={16}
            clearable={false}
            value={pendingRating() ? pendingRating()!.value : userState().rating}
            disabled={props.controller.saving() && !pendingRating()}
            onChange={(staticRating) => {
              const change = { value: staticRating }
              setPendingRating(change)
              quietly(
                props.controller
                  .update((input) =>
                    untrack(() => ({
                      ...input,
                      user_state: { ...input.user_state, rating: staticRating },
                    })),
                  )
                  .finally(() => {
                    if (untrack(pendingRating) === change) setPendingRating(undefined)
                  }),
              )
            }}
          />
        </div>
      )}
    </Show>
  )
}

const styles = {
  bar: css({
    alignItems: "center",
    display: "flex",
    gap: "2",
  }),
  ratingLabel: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
    letterSpacing: "0.04em",
  }),
  heart: cva({
    base: {
      _hover: { color: "favorite.preview" },
    },
    variants: {
      on: { true: { color: "favorite.default" }, false: { color: "fg.muted" } },
    },
  }),
}
