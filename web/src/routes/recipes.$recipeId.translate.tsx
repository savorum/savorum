import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { NewTranslationPage } from "@/features/recipe-translations/new-translation-page"

import { m } from "../lib/messages.ts"

interface TranslateSearch {
  language: string
  copy: boolean
}

export const Route = createFileRoute("/recipes/$recipeId/translate")({
  component: TranslateRoute,
  validateSearch: (search: Record<string, unknown>): TranslateSearch => ({
    language: typeof search.language === "string" ? search.language : "",
    copy: search.copy === true || search.copy === "true",
  }),
  head: () => ({ meta: [{ title: m.translations_new_page_title() }] }),
})

function TranslateRoute() {
  const params = Route.useParams()
  const search = Route.useSearch()
  const navigate = useNavigate()
  return (
    <NewTranslationPage
      recipeID={() => params().recipeId}
      language={() => search().language}
      copy={() => search().copy}
      onCreated={(recipeId) => void navigate({ to: "/recipes/$recipeId", params: { recipeId } })}
      onCancel={() =>
        void navigate({ to: "/recipes/$recipeId", params: { recipeId: params().recipeId } })
      }
    />
  )
}
