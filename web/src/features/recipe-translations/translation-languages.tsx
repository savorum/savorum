import { Link } from "@tanstack/solid-router"
import { For, Show } from "solid-js"
import { css } from "styled-system/css"

import type { TranslationRendition } from "@/api/client"
import { nativeLanguageName } from "@/lib/direction"
import { interfaceAttributes } from "@/lib/i18n"

import { m } from "../../lib/messages.ts"

const styles = {
  nav: css({
    alignItems: "baseline",
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    rowGap: "1",
    columnGap: "3",
    marginTop: "2",
  }),
  list: css({
    display: "flex",
    flexWrap: "wrap",
    rowGap: "1",
    columnGap: "3",
    listStyle: "none",
    margin: "0",
    padding: "0",
  }),
  current: css({ color: "fg.default", fontWeight: "semibold" }),
  link: css({
    color: "fg.muted",
    textDecoration: "underline",
    textDecorationColor: "border.default",
    textUnderlineOffset: "3px",
    _hover: { color: "accent.default" },
  }),
}

/**
 * The languages a recipe is available in. Each is its own recipe with its own
 * address, so choosing one navigates rather than swapping text in place.
 */
export function TranslationLanguages(props: {
  language?: string
  renditions: TranslationRendition[]
}) {
  const others = () =>
    [...props.renditions].sort((left, right) =>
      nativeLanguageName(left.language).localeCompare(nativeLanguageName(right.language)),
    )
  return (
    <Show when={props.language && props.renditions.length > 0}>
      <nav class={styles.nav} aria-label={m.translations_languages()} {...interfaceAttributes()}>
        <span aria-hidden="true">{m.translations_languages()}</span>
        <ul class={styles.list} role="list">
          <li>
            <span class={styles.current} lang={props.language} aria-current="page">
              {nativeLanguageName(props.language!)}
            </span>
          </li>
          <For each={others()}>
            {(rendition) => (
              <li>
                <Link
                  class={styles.link}
                  to="/recipes/$recipeId"
                  params={{ recipeId: rendition.id }}
                  hreflang={rendition.language}
                  lang={rendition.language}
                  title={rendition.title}
                >
                  {nativeLanguageName(rendition.language)}
                </Link>
              </li>
            )}
          </For>
        </ul>
      </nav>
    </Show>
  )
}
