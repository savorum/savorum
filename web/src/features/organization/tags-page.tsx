import { For } from "solid-js"
import { css, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { ChangeError, ResourceState } from "@/components/status"

import { m } from "../../lib/messages.ts"
import { createLabels } from "./create-labels"
import { LabelGroup } from "./label-group"

const layout = pageShell({ width: "medium" })

const styles = {
  title: cx(layout.title, css({ marginBottom: "8" })),
}

export function TagsPage() {
  const { tags, refetchTags, facets, refetchFacets, error, busy, run, groups } = createLabels()

  return (
    <main class={layout.page}>
      <h1 class={styles.title}>{m.tags_page_tags_and_categories()}</h1>

      <ChangeError error={error()} onReload={() => void refetchTags()} />

      <ResourceState
        resource={tags}
        onRetry={() => {
          void refetchTags()
          void refetchFacets()
        }}
      >
        {() => (
          <ResourceState
            resource={facets}
            onRetry={() => {
              void refetchTags()
              void refetchFacets()
            }}
          >
            {() => (
              <For each={groups()}>
                {(group) => <LabelGroup group={group} busy={busy()} onRun={run} />}
              </For>
            )}
          </ResourceState>
        )}
      </ResourceState>
    </main>
  )
}
