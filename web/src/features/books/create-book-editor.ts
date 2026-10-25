import { batch, createMemo, createResource, createSignal } from "solid-js"
import { createStore, reconcile, unwrap } from "solid-js/store"

import { bookInput, deleteBook, replaceBook } from "@/api/books"
import { type BookInput, type RecipeSummary } from "@/api/client"
import { lookupRecipes } from "@/api/selections"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import type { BookViewProps } from "./book-types"
export function createBookEditor(props: BookViewProps, onFocus: (id?: string) => void) {
  const [base, setBase] = createSignal({ value: props.book, revision: props.revision })
  const original = () => JSON.stringify(bookInput(base().value))
  const [draft, setDraft] = createStore<BookInput>(structuredClone(bookInput(props.book)))
  const [editing, setEditing] = createSignal(false)
  const [confirmingTrash, setConfirmingTrash] = createSignal(false)
  const [pending, setPending] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  const [announcement, setAnnouncement] = createSignal("")
  // Candidate search is intentionally a narrow page. Existing members still
  // need their names and pictures even when they are outside that page.
  const [members] = createResource(
    () => base().value.entries ?? [],
    async (entries) => {
      const items = (await lookupRecipes(entries.map((entry) => entry.library_entry_id))).items
      return new Map(items.map((item) => [item.id, item]))
    },
  )

  const published = () => draft.kind === "publication"
  // Serializing the store itself reads every field through the proxy, so this
  // stays true to the draft as it changes. Reading `unwrap` here would take a
  // snapshot outside Solid's tracking and the save bar would never appear.
  const dirty = createMemo(() => JSON.stringify(draft) !== original())
  const entryState = (entryID: string) =>
    (base().value.entries ?? []).find((entry) => entry.library_entry_id === entryID)
      ?.recipe_state ?? "active"
  const membersRead = () => members.state !== "pending" && members.state !== "unresolved"
  const recipeSummary = (entryID: string): RecipeSummary | undefined =>
    settled(members)?.get(entryID) ??
    props.candidatePage?.items.find((candidate) => candidate.id === entryID)
  const recipeTitle = (entryID: string) =>
    props.candidatePage?.items.find((candidate) => candidate.id === entryID)?.title ??
    settled(members)?.get(entryID)?.title ??
    m.common_untitled_recipe()

  const credits = createMemo(() =>
    [draft.author, draft.publisher, draft.edition, draft.publication_date]
      .filter(Boolean)
      .join(" · "),
  )

  const grouped = createMemo(() => {
    const entries = draft.entries ?? []
    const sections = draft.sections ?? []
    const groups = sections.map((section) => ({
      id: section.id,
      title: section.title,
      entries: entries.filter((entry) => entry.section_id === section.id),
    }))
    const loose = entries.filter(
      (entry) => !entry.section_id || !sections.some((section) => section.id === entry.section_id),
    )
    return { groups, loose }
  })

  const discard = () => {
    setDraft(reconcile(structuredClone(bookInput(base().value))))
    setError(undefined)
  }

  const save = async () => {
    if (pending()) return
    const submitted = JSON.stringify(draft)
    const expected = base()
    setPending(true)
    setError(undefined)
    // Order is kept per chapter, so each chapter counts from its own first
    // recipe rather than sharing one running number with the whole book.
    const nextPosition = new Map<string, number>()
    const positioned: BookInput = {
      ...unwrap(draft),
      sections: (draft.sections ?? []).map((section, index) => ({ ...section, position: index })),
      entries: (draft.entries ?? []).map((entry) => {
        const chapter = entry.section_id ?? ""
        const position = nextPosition.get(chapter) ?? 0
        nextPosition.set(chapter, position + 1)
        return { ...entry, position }
      }),
    }
    try {
      const acknowledged = await replaceBook(
        expected.value.id,
        expected.revision,
        structuredClone(positioned),
      )
      batch(() => {
        // Preserve edits made during the request. Only an unchanged draft may
        // be replaced with the server's normalized acknowledged representation.
        if (JSON.stringify(draft) === submitted) {
          setDraft(reconcile(structuredClone(bookInput(acknowledged.value))))
        }
        setBase(acknowledged)
      })
    } catch (failure) {
      setError(failure)
    } finally {
      setPending(false)
    }
  }

  const moveToTrash = async () => {
    setPending(true)
    try {
      await deleteBook(base().value.id, base().revision)
      await props.onTrashed()
    } catch (failure) {
      setError(failure)
    } finally {
      setPending(false)
    }
  }

  /**
   * Moves a recipe within the chapter it is shown in. The two recipes exchange
   * their places in the saved order, so an arrow never reaches past the chapter
   * the reader is looking at.
   */
  const moveWithin = (
    group: NonNullable<BookInput["entries"]>,
    entryID: string,
    offset: number,
  ) => {
    const within = group.findIndex((entry) => entry.library_entry_id === entryID)
    const neighbor = group[within + offset]
    if (!neighbor) return
    const entries = draft.entries ?? []
    const from = entries.findIndex((entry) => entry.library_entry_id === entryID)
    const to = entries.findIndex((entry) => entry.library_entry_id === neighbor.library_entry_id)
    if (from === -1 || to === -1) return
    const next = [...entries]
    next[from] = entries[to]
    next[to] = entries[from]
    setDraft("entries", next)
    setAnnouncement(`Moved ${recipeTitle(entryID)} to position ${within + offset + 1}.`)
    onFocus(entryID)
  }

  /** Puts recipes in the book and saves it immediately. */
  const addEntries = async (ids: string[]) => {
    const existing = new Set((draft.entries ?? []).map((entry) => entry.library_entry_id))
    const wanted = ids.filter((id) => !existing.has(id))
    if (wanted.length === 0) return
    setDraft("entries", (entries) => [
      ...(entries ?? []),
      ...wanted.map((id, index) => ({
        library_entry_id: id,
        position: (entries ?? []).length + index,
      })),
    ])
    await save()
    setAnnouncement(
      wanted.length === 1
        ? `Added ${recipeTitle(wanted[0]!)} to the book.`
        : `Added ${wanted.length} recipes to the book.`,
    )
  }

  const removeEntry = (group: NonNullable<BookInput["entries"]>, entryID: string) => {
    const within = group.findIndex((entry) => entry.library_entry_id === entryID)
    const survivor = group[within + 1] ?? group[within - 1]
    setDraft("entries", (entries) =>
      (entries ?? []).filter((candidate) => candidate.library_entry_id !== entryID),
    )
    setAnnouncement(`Removed ${recipeTitle(entryID)} from the book.`)
    onFocus(survivor?.library_entry_id)
  }

  return {
    draft,
    setDraft,
    editing,
    setEditing,
    confirmingTrash,
    setConfirmingTrash,
    pending,
    error,
    addEntries,
    announcement,
    published,
    dirty,
    entryState,
    membersRead,
    recipeSummary,
    recipeTitle,
    credits,
    grouped,
    save,
    discard,
    moveToTrash,
    moveWithin,
    removeEntry,
  }
}
export type BookEditor = ReturnType<typeof createBookEditor>
