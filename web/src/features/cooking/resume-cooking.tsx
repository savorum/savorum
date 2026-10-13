import { Link } from "@tanstack/solid-router"
import { Utensils } from "lucide-solid"
import { Show, createEffect, createSignal, onCleanup, onMount } from "solid-js"
import { css } from "styled-system/css"

import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { ConfirmDialog } from "@/components/ui/dialog"
import { isolate } from "@/lib/direction"
import { m } from "@/lib/messages.ts"

import { cookingOwner } from "./cooking-owner"
import { activeCookingDishes, type CookingSession, finishCookingSession } from "./cooking-session"

export function ResumeCooking() {
  const [dish, setDish] = createSignal<CookingSession>()
  const [discarding, setDiscarding] = createSignal(false)
  const refresh = () => {
    const owner = cookingOwner()
    setDish(owner ? activeCookingDishes(owner)[0] : undefined)
  }
  createEffect(refresh)
  onMount(() => {
    window.addEventListener("storage", refresh)
    window.addEventListener("savorum-cooking-changed", refresh)
    onCleanup(() => {
      window.removeEventListener("storage", refresh)
      window.removeEventListener("savorum-cooking-changed", refresh)
    })
  })
  return (
    <Show when={dish()}>
      {(current) => (
        <aside
          class={css({
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: "3",
            flexWrap: "wrap",
            paddingBlock: "2",
            paddingInline: { base: "4", md: "8" },
            background: "bg.muted",
            borderBottomWidth: "1px",
            borderColor: "border.subtle",
          })}
        >
          <span
            class={css({
              alignItems: "center",
              display: "inline-flex",
              fontWeight: "medium",
              gap: "2",
              overflowWrap: "anywhere",
            })}
          >
            <Utensils aria-hidden="true" size={15} />
            {m.cooking_mode_cooking({ value1: isolate(current().snapshot?.title ?? "") })}
          </span>
          <span class={css({ display: "flex", flexWrap: "wrap", gap: "2" })}>
            <Button variant="plain" size="sm" onClick={() => setDiscarding(true)}>
              {m.cooking_discard()}
            </Button>
            <Link
              to="/recipes/$recipeId/cook"
              params={{ recipeId: current().recipeID }}
              class={buttonLink({ variant: "outline", size: "sm" })}
            >
              {m.create_recipe_reader_resume_cooking()}
            </Link>
          </span>
          <Show when={discarding()}>
            <ConfirmDialog
              title={m.cooking_discard_title()}
              description={m.cooking_discard_description({
                name: isolate(current().snapshot?.title ?? ""),
              })}
              confirmLabel={m.cooking_discard_confirm()}
              cancelLabel={m.cooking_keep_cooking()}
              onClose={() => setDiscarding(false)}
              onConfirm={() => {
                setDiscarding(false)
                finishCookingSession(current())
              }}
            />
          </Show>
        </aside>
      )}
    </Show>
  )
}
