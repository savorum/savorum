import { Link } from "@tanstack/solid-router"
import { CookingPot, Ellipsis, Pencil, ShoppingCart } from "lucide-solid"
import { For, Show } from "solid-js"
import { css } from "styled-system/css"

import type { Meal } from "@/api/client"
import { IconButton } from "@/components/ui/icon-button"
import { Menu, MenuItem, MenuItemGroup, MenuSeparator } from "@/components/ui/menu"
import { isolate } from "@/lib/direction"

import { m } from "../../lib/messages.ts"
import { cookLabel, cookable, mealType, scaled, type MealDish } from "./meal-labels"
import { TileImages } from "./meal-tile-images"
import { dishImages, tiles } from "./meal-tiles"

const styles = {
  thumb: css({
    aspectRatio: "1",
    borderRadius: "l1",
    flexShrink: "0",
    gridRow: "span 2",
    objectFit: "cover",
    width: "16",
  }),
  cover: css({
    aspectRatio: "4 / 3",
    borderRadius: "l1",
    objectFit: "cover",
    width: "full",
  }),
  notice: css({ color: "fg.muted", fontSize: "sm", marginTop: "1" }),
  // A photograph beside the meal, with its actions under the words.
  meal: css({
    background: "bg.surface",
    borderColor: "border.subtle",
    borderRadius: "l2",
    borderWidth: "1px",
    columnGap: "3",
    display: "grid",
    gridTemplateColumns: "auto minmax(0, 1fr)",
    marginBottom: "2",
    padding: "2.5",
    "& > :not(img)": { gridColumn: "2" },
    "&:not(:has(> img)) > *": { gridColumn: "1 / -1" },
    '&[data-skipped="true"]': { opacity: "0.7" },
  }),
  // A narrow week column: the photograph above the words.
  mealCompact: css({
    background: "bg.surface",
    borderColor: "border.subtle",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "2",
    marginBottom: "2",
    padding: "2",
    '&[data-skipped="true"]': { opacity: "0.7" },
  }),
  mealBody: css({ display: "grid", gap: "0.5", minWidth: "0" }),
  slot: css({ color: "fg.muted", fontSize: "xs", fontWeight: "medium" }),
  mealTitle: css({ fontWeight: "semibold", lineHeight: "1.3", overflowWrap: "anywhere" }),
  meta: css({ color: "fg.muted", fontSize: "sm", overflowWrap: "anywhere" }),
  iconActions: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    marginInline: "-1.5",
    marginTop: "1",
  }),
}

/** What a meal's selection checkbox, action buttons, and More menu do. */
export interface MealControls {
  selecting: () => boolean
  isSelected: (id: string) => boolean
  select: (id: string, checked: boolean) => void
  starting: () => boolean
  cook: (meal: Meal) => void
  shop: (meal: Meal) => void
  edit: (meal: Meal) => void
  menu: (meal: Meal, action: string) => void
}

const DishLabel = (props: { dish: MealDish }) => (
  <Show
    when={props.dish.entry_id && props.dish.recipe_status === "available"}
    fallback={scaled(props.dish)}
  >
    <Link
      to="/recipes/$recipeId"
      params={{ recipeId: props.dish.entry_id! }}
      aria-label={m.plan_open_recipe({ name: props.dish.display_name })}
    >
      {scaled(props.dish)}
    </Link>
  </Show>
)

function DishList(props: { meal: Meal }) {
  return (
    <For each={props.meal.dishes ?? []}>
      {(dish, index) => (
        <>
          {index() > 0 ? ", " : ""}
          <DishLabel dish={dish} />
        </>
      )}
    </For>
  )
}

function MealSelection(props: { meal: Meal; controls: MealControls }) {
  return (
    <Show when={props.controls.selecting()}>
      <label class={css({ display: "flex", alignItems: "center", gap: "3", minHeight: "12" })}>
        <input
          type="checkbox"
          disabled={props.meal.skipped}
          checked={props.controls.isSelected(props.meal.id)}
          onChange={(event) => props.controls.select(props.meal.id, event.currentTarget.checked)}
        />
        {m.shopping_select_meal({ name: isolate(props.meal.display_title) })}
      </label>
    </Show>
  )
}

/** The frequent actions as a compact row of icons; the rest are under More. */
function MealActionRow(props: { meal: Meal; controls: MealControls; class?: string }) {
  return (
    <Show when={!props.controls.selecting()}>
      <div class={props.class ?? styles.iconActions}>
        <Show when={cookable(props.meal)}>
          <IconButton
            size="xs"
            variant="plain"
            aria-label={cookLabel(props.meal)}
            title={cookLabel(props.meal)}
            disabled={props.controls.starting()}
            onClick={() => props.controls.cook(props.meal)}
          >
            <CookingPot aria-hidden="true" />
          </IconButton>
        </Show>
        <IconButton
          size="xs"
          variant="plain"
          aria-label={m.shopping_review()}
          title={m.shopping_review()}
          onClick={() => props.controls.shop(props.meal)}
        >
          <ShoppingCart aria-hidden="true" />
        </IconButton>
        <IconButton
          size="xs"
          variant="plain"
          aria-label={m.plan_edit()}
          title={m.plan_edit()}
          onClick={() => props.controls.edit(props.meal)}
        >
          <Pencil aria-hidden="true" />
        </IconButton>
        <Menu
          onSelect={({ value }) => props.controls.menu(props.meal, value)}
          trigger={(trigger) => (
            <IconButton
              {...trigger()}
              size="xs"
              variant="plain"
              aria-label={m.plan_more_actions()}
              title={m.plan_more_actions()}
            >
              <Ellipsis aria-hidden="true" />
            </IconButton>
          )}
        >
          <MenuItemGroup>
            <MenuItem value="copy">{m.plan_copy_to()}</MenuItem>
            <MenuItem value="move">{m.plan_move_to()}</MenuItem>
            <MenuItem value="leftovers">{m.plan_leftovers()}</MenuItem>
            <MenuItem value="skip">
              {props.meal.skipped ? m.plan_keep_meal() : m.plan_skip()}
            </MenuItem>
          </MenuItemGroup>
          <MenuSeparator />
          <MenuItem value="save">{m.plan_save_combination()}</MenuItem>
          <MenuItem value="delete">{m.plan_delete()}</MenuItem>
        </Menu>
      </div>
    </Show>
  )
}

function TroubledDishes(props: { meal: Meal }) {
  return (
    <For
      each={(props.meal.dishes ?? []).filter(
        (dish) => dish.recipe_status === "trashed" || dish.recipe_status === "removed",
      )}
    >
      {(dish) => (
        <p class={styles.notice}>
          {dish.recipe_status === "trashed"
            ? m.plan_recipe_trashed({ name: isolate(dish.display_name) })
            : m.plan_recipe_removed({ name: isolate(dish.display_name) })}
        </p>
      )}
    </For>
  )
}

/** A day's meal: its photograph, what is being made, and its actions. */
export function MealCard(props: { meal: Meal; compact: boolean; controls: MealControls }) {
  const image = () => dishImages(props.meal.dishes ?? [])[0]
  return (
    <article
      class={props.compact ? styles.mealCompact : styles.meal}
      data-skipped={props.meal.skipped ? "true" : undefined}
    >
      <MealSelection meal={props.meal} controls={props.controls} />
      <Show when={image()}>
        <img
          class={props.compact ? styles.cover : styles.thumb}
          src={`${image()}/derivatives/card`}
          alt=""
          loading="lazy"
        />
      </Show>
      <div class={styles.mealBody}>
        <Show when={props.meal.meal_type || props.meal.skipped}>
          <div class={styles.slot}>
            {[mealType(props.meal.meal_type), props.meal.skipped ? m.plan_skipped() : ""]
              .filter(Boolean)
              .join(" · ")}
          </div>
        </Show>
        <div class={styles.mealTitle}>
          <Show when={props.meal.name} fallback={<DishList meal={props.meal} />}>
            {props.meal.name}
          </Show>
        </div>
        <Show when={props.meal.name}>
          <div class={styles.meta}>
            <DishList meal={props.meal} />
          </div>
        </Show>
        <TroubledDishes meal={props.meal} />
      </div>
      <MealActionRow meal={props.meal} controls={props.controls} />
    </article>
  )
}

/** A meal without a day, shown as a tile like a saved meal. */
export function MealTile(props: { meal: Meal; controls: MealControls }) {
  const dishes = () => props.meal.dishes ?? []
  return (
    <article class={tiles.tile}>
      <div class={tiles.header}>
        <TileImages dishes={dishes()} />
        <div class={tiles.heading}>
          <h3 class={tiles.title}>{props.meal.display_title}</h3>
          <Show when={props.meal.skipped || props.meal.meal_type}>
            <p class={tiles.meta}>
              {[props.meal.skipped ? m.plan_skipped() : "", mealType(props.meal.meal_type)]
                .filter(Boolean)
                .join(" · ")}
            </p>
          </Show>
        </div>
      </div>
      <div class={css({ alignContent: "start", display: "grid", gap: "2" })}>
        <MealSelection meal={props.meal} controls={props.controls} />
        <ul class={tiles.dishes}>
          <For each={dishes()}>
            {(dish) => (
              <li>
                <DishLabel dish={dish} />
              </li>
            )}
          </For>
        </ul>
        <Show when={props.meal.notes}>
          <p class={tiles.note}>{props.meal.notes}</p>
        </Show>
        <TroubledDishes meal={props.meal} />
      </div>
      <MealActionRow meal={props.meal} controls={props.controls} class={tiles.actions} />
    </article>
  )
}
