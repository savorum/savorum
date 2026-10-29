import { Link, useLocation } from "@tanstack/solid-router"
import { Check, Heart, UtensilsCrossed } from "lucide-solid"
import { For, Show, createMemo, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { type RecipeSummary, fallbackToOriginalImage } from "@/api/client"
import { LabelSwatch } from "@/components/label-swatch"
import { formatRelativeDate } from "@/lib/date-format"
import { contentAttributes, isolate, nativeLanguageName } from "@/lib/direction"
import { formatDuration } from "@/lib/duration-format"
import { rankLabels } from "@/lib/label-ranking"
import { formatList, formatNumber } from "@/lib/locale-format"

import { m } from "../lib/messages.ts"

const styles = {
  card: css({
    display: "grid",
    gap: "2.5",
    gridTemplateRows: "auto 1fr",
    gridTemplateColumns: "minmax(0, 1fr)",
    textDecoration: "none",
    _focusVisible: {
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "5px",
      borderRadius: "l2",
    },
    "&:hover .recipe-card-title": { color: "accent.default" },
  }),
  frame: css({
    aspectRatio: "4 / 3",
    background: "bg.muted",
    borderRadius: "l2",
    overflow: "hidden",
    position: "relative",
  }),
  image: css({
    display: "block",
    height: "100%",
    objectFit: "cover",
    transition: "transform 320ms ease",
    width: "100%",
  }),
  placeholder: css({
    alignItems: "center",
    color: "border.default",
    display: "flex",
    height: "100%",
    justifyContent: "center",
  }),
  favorite: css({
    alignItems: "center",
    background: "bg.surface",
    borderRadius: "full",
    boxShadow: "0 1px 3px rgba(0, 0, 0, 0.22)",
    color: "favorite.default",
    display: "flex",
    height: "1.75rem",
    insetBlockStart: "2",
    insetInlineEnd: "2",
    justifyContent: "center",
    position: "absolute",
    width: "1.75rem",
  }),
  title: css({
    color: "fg.default",
    fontFamily: "editorial",
    fontSize: "lg",
    fontWeight: "normal",
    hyphens: "auto",
    lineHeight: "1.25",
    margin: "0",
    transition: "colors",
  }),
  meta: css({ color: "fg.muted", fontSize: "xs", marginTop: "1" }),
  labels: css({
    alignItems: "center",
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "xs",
    gap: "1.5",
    margin: "0",
    marginTop: "1.5",
  }),
  label: css({
    alignItems: "center",
    background: "bg.muted",
    borderRadius: "l1",
    display: "inline-flex",
    gap: "1",
    maxWidth: "10rem",
    overflow: "hidden",
    paddingBlock: "0.5",
    paddingInline: "1.5",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  }),
  moreLabels: css({ color: "fg.subtle" }),
  selectable: css({
    background: "transparent",
    borderWidth: "0",
    cursor: "pointer",
    display: "grid",
    gap: "2.5",
    gridTemplateRows: "auto 1fr",
    gridTemplateColumns: "minmax(0, 1fr)",
    padding: "0",
    textAlign: "start",
    width: "100%",
    _focusVisible: {
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "5px",
      borderRadius: "l2",
    },
  }),
  // The mark appears only in selection mode, so the ordinary library is
  // pictures of food rather than a grid of checkboxes.
  mark: css({
    alignItems: "center",
    background: "bg.surface",
    borderColor: "border.default",
    borderRadius: "full",
    borderWidth: "1px",
    color: "fg.muted",
    display: "flex",
    height: "1.5rem",
    insetBlockStart: "2.5",
    insetInlineStart: "2.5",
    justifyContent: "center",
    position: "absolute",
    width: "1.5rem",
  }),
  marked: css({
    background: "accent.default",
    borderColor: "accent.default",
    color: "bg.surface",
  }),
  dimmed: css({ opacity: "0.55" }),
}

export function recipeTime(recipe: Pick<RecipeSummary, "active_time" | "total_time">): string {
  const active = formatDuration(recipe.active_time)
  const total = formatDuration(recipe.total_time)
  if (active && total && active !== total) return m.recipe_card_active_and_total({ active, total })
  if (total) return total
  return active ? m.recipe_card_active({ active }) : ""
}

export interface RecipeCardProps {
  recipe: RecipeSummary
  labelRecipeCounts?: ReadonlyMap<string, number>
  libraryTotal?: number
  priority?: boolean
  /** True while the library is choosing recipes rather than opening one. */
  selecting?: boolean
  selected?: boolean
  onSelect?: (id: string) => void
}

export const RecipeCard = (props: RecipeCardProps) => {
  const location = useLocation()
  const [libraryScroll, setLibraryScroll] = createSignal({ x: 0, y: 0 })
  const libraryReturn = () => ({
    href: location().href,
    historyIndex: location().state.__TSR_index,
    scrollX: libraryScroll().x,
    scrollY: libraryScroll().y,
  })
  const made = () =>
    props.recipe.last_cooked
      ? m.recipe_card_made({ when: formatRelativeDate(props.recipe.last_cooked) })
      : ""
  /** Only a grouped result names its language, beside the others the set holds. */
  const renditions = () => props.recipe.translations ?? []
  const language = () =>
    renditions().length && props.recipe.language ? nativeLanguageName(props.recipe.language) : ""
  const names = (matched: boolean) =>
    formatList(
      renditions()
        .filter((rendition) => rendition.matched === matched)
        .map((rendition) => isolate(nativeLanguageName(rendition.language))),
    )
  const alternatives = () =>
    [
      renditions().some((rendition) => rendition.matched)
        ? m.recipe_card_also_matches_in({ languages: names(true) })
        : "",
      renditions().some((rendition) => !rendition.matched)
        ? m.recipe_card_also_in({ languages: names(false) })
        : "",
    ]
      .filter(Boolean)
      .join(` ${m.common_dot_separator()} `)
  const meta = () =>
    [recipeTime(props.recipe), made(), props.recipe.authors?.join(", ")]
      .filter(Boolean)
      .join(` ${m.common_dot_separator()} `)
  const labels = createMemo(() =>
    rankLabels(
      [
        ...(props.recipe.tags ?? []).map((tag) => ({
          ...tag,
          kind: "tag",
          recipeCount: props.labelRecipeCounts?.get(tag.id) ?? 0,
        })),
        ...(props.recipe.facets ?? []).map((facet) => ({
          id: facet.id,
          name: facet.value,
          kind: facet.kind,
          color: undefined,
          recipeCount: props.labelRecipeCounts?.get(facet.id) ?? 0,
        })),
      ],
      props.libraryTotal,
    ),
  )
  const shownLabels = createMemo(() => labels().slice(0, 2))

  const body = () => (
    <>
      <div class={styles.frame}>
        <Show
          when={props.recipe.primary_image?.url}
          fallback={
            <div class={styles.placeholder}>
              <UtensilsCrossed aria-hidden="true" size={30} />
            </div>
          }
        >
          <img
            class={styles.image}
            src={`${props.recipe.primary_image!.url}/derivatives/card`}
            onError={(event) => fallbackToOriginalImage(event.currentTarget)}
            alt=""
            loading={props.priority ? "eager" : "lazy"}
            fetchpriority={props.priority ? "high" : "auto"}
            decoding="async"
          />
        </Show>
        <Show when={props.recipe.favorite}>
          <span class={styles.favorite} role="img" aria-label={m.recipe_card_favorite()}>
            <Heart aria-hidden="true" fill="currentColor" size={15} />
          </span>
        </Show>
        <Show when={props.selecting}>
          <span class={`${styles.mark} ${props.selected ? styles.marked : ""}`} aria-hidden="true">
            <Show when={props.selected}>
              <Check size={14} />
            </Show>
          </span>
        </Show>
      </div>
      <div>
        <h2
          class={`${styles.title} recipe-card-title`}
          {...contentAttributes(props.recipe.language)}
        >
          {props.recipe.title}
        </h2>
        <Show when={meta()}>
          <p class={styles.meta}>{meta()}</p>
        </Show>
        <Show when={language()}>
          <p class={styles.meta}>
            <span lang={props.recipe.language}>{language()}</span>
            <Show when={alternatives()}>
              {" "}
              {m.common_dot_separator()} {alternatives()}
            </Show>
          </p>
        </Show>
        <Show when={shownLabels().length}>
          <p class={styles.labels}>
            <For each={shownLabels()}>
              {(label) => (
                <span class={styles.label}>
                  <LabelSwatch color={label.color} size="sm" />
                  {label.name}
                </span>
              )}
            </For>
            <Show when={labels().length > shownLabels().length}>
              <span class={styles.moreLabels}>
                {" "}
                {m.common_plus_symbol()}
                {formatNumber(labels().length - shownLabels().length)}
              </span>
            </Show>
          </p>
        </Show>
      </div>
    </>
  )

  return (
    <Show
      when={props.selecting}
      fallback={
        <Link
          class={styles.card}
          to="/recipes/$recipeId"
          params={{ recipeId: props.recipe.id }}
          state={{ libraryReturn: libraryReturn() }}
          onClick={() => setLibraryScroll({ x: window.scrollX, y: window.scrollY })}
        >
          {body()}
        </Link>
      }
    >
      <button
        type="button"
        class={`${styles.selectable} ${props.selected ? "" : styles.dimmed}`}
        aria-pressed={props.selected}
        onClick={() => props.onSelect?.(props.recipe.id)}
      >
        {body()}
      </button>
    </Show>
  )
}
