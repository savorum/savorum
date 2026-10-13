import { Link } from "@tanstack/solid-router"
import { Plus } from "lucide-solid"
import { For, Show } from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import type { SavedView } from "@/api/client"
import { ChangeError, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { describeQuery } from "@/lib/library-query"

import { m } from "../../lib/messages.ts"
import { createSavedSearches } from "./create-saved-searches"
import { SavedSearchEditor } from "./saved-search-editor"

const layout = pageShell({ width: "narrow" })

const styles = {
  header: css({
    alignItems: "center",
    display: "flex",
    gap: "4",
    justifyContent: "space-between",
    marginBottom: "7",
  }),
  list: css({ listStyle: "none", margin: "0", padding: "0" }),
  row: css({
    alignItems: "center",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "flex",
    gap: "4",
    justifyContent: "space-between",
    paddingBlock: "4",
  }),
  name: css({ fontSize: "md", minWidth: "0" }),
  query: css({ color: "fg.muted", fontSize: "xs", marginTop: "1" }),
  actions: css({ display: "flex", gap: "2" }),
  empty: css({ color: "fg.muted", fontSize: "sm", paddingBlock: "8" }),
}

export function SavedSearchesPage() {
  const { views, refetch, vocabulary, editing, setEditing, busy, error, run } =
    createSavedSearches()

  return (
    <main class={layout.page}>
      <div class={styles.header}>
        <h1 class={layout.title}>{m.saved_searches_page_saved_searches()}</h1>
        <Button
          colorPalette="accent"
          onClick={() => setEditing({ revision: 0, title: "", query: "", sort: "updated-desc" })}
        >
          <Plus aria-hidden="true" size={16} />
          {m.saved_searches_page_new_search()}
        </Button>
      </div>

      <ChangeError error={error()} onReload={() => void refetch()} />

      <ResourceState
        resource={views}
        onRetry={() => void refetch()}
        isEmpty={(items) => items.length === 0}
        empty={
          <p class={styles.empty}>
            {" "}
            {m.saved_searches_page_save_a_search_to_keep_a_shortcut_to_the_recipes_you_loo()}{" "}
          </p>
        }
      >
        {(saved) => (
          <ul class={styles.list}>
            <For each={saved()}>
              {(view: SavedView) => (
                <li class={styles.row}>
                  <div class={styles.name}>
                    {view.title}
                    <Show when={describeQuery(view.query, vocabulary.label)}>
                      <p class={styles.query}>{describeQuery(view.query, vocabulary.label)}</p>
                    </Show>
                  </div>
                  <div class={styles.actions}>
                    <Link
                      class={buttonLink({ size: "sm", variant: "outline" })}
                      to="/"
                      search={{ view: view.id }}
                    >
                      {" "}
                      {m.saved_searches_page_open()}{" "}
                    </Link>
                    <Button
                      size="sm"
                      variant="plain"
                      onClick={() =>
                        setEditing({
                          id: view.id,
                          revision: view.revision,
                          title: view.title,
                          query: view.query,
                          sort: view.sort ?? "updated-desc",
                        })
                      }
                    >
                      {m.saved_searches_page_edit()}
                    </Button>
                  </div>
                </li>
              )}
            </For>
          </ul>
        )}
      </ResourceState>

      <Show when={editing()} keyed>
        {(draft) => (
          <SavedSearchEditor
            draft={draft}
            vocabulary={vocabulary}
            busy={busy()}
            onRun={run}
            onClose={() => setEditing(undefined)}
          />
        )}
      </Show>
    </main>
  )
}
