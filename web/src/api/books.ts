import { m } from "../lib/messages.ts"
import {
  type Book,
  type BookSummary,
  type BookInput,
  type BookRevision,
  type BookRevisionSummary,
  type BookSourceRecipe,
  type Versioned,
  client,
  expectOk,
  ifMatch,
  unwrap,
  unwrapItems,
  unwrapVersioned,
} from "./client"

export interface BookPage {
  items: BookSummary[]
  total: number
  limit: number
  offset: number
}

export interface ListBooksOptions {
  limit?: number
  offset?: number
  entryID?: string
  state?: "active" | "trash"
}

/** Reads one navigable page of books. */
export async function listBookPage(options: ListBooksOptions = {}): Promise<BookPage> {
  const { limit = 25, offset = 0, entryID, state = "active" } = options
  const data = unwrap(
    await client.GET("/api/v1/books", {
      params: { query: { limit, offset, entry_id: entryID, state } },
    }),
    m.books_books_could_not_be_loaded(),
  )
  return { ...data, items: data.items ?? [] }
}

/** Reads every page when a caller genuinely needs the whole collection. */
export async function listAllBooks(
  options: Omit<ListBooksOptions, "limit" | "offset"> = {},
): Promise<BookSummary[]> {
  const items: BookSummary[] = []
  const limit = 200
  for (let offset = 0; ; offset += limit) {
    const page = await listBookPage({ ...options, limit, offset })
    items.push(...page.items)
    if (items.length >= page.total || page.items.length < limit) return items
  }
}

/** Compatibility helper for small vocabularies: never silently stops at one page. */
export async function listBooks(
  entryID?: string,
  state: "active" | "trash" = "active",
): Promise<BookSummary[]> {
  return listAllBooks({ entryID, state })
}

export async function restoreBook(id: string, revision: number): Promise<Versioned<Book>> {
  return unwrapVersioned(
    await client.POST("/api/v1/books/{id}/restore", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
    }),
    m.books_the_book_could_not_be_restored(),
  )
}

export async function deleteBookPermanently(id: string, revision: number): Promise<void> {
  expectOk(
    await client.DELETE("/api/v1/trash/books/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
    }),
    m.books_the_book_could_not_be_deleted_permanently(),
  )
}

export async function getBook(id: string): Promise<Versioned<Book>> {
  return unwrapVersioned(
    await client.GET("/api/v1/books/{id}", { params: { path: { id } } }),
    m.books_the_book_could_not_be_loaded(),
  )
}

export async function createBook(book: BookInput): Promise<Versioned<Book>> {
  return unwrapVersioned(
    await client.POST("/api/v1/books", { body: book }),
    m.books_the_book_could_not_be_created(),
  )
}

export async function replaceBook(
  id: string,
  revision: number,
  book: BookInput,
): Promise<Versioned<Book>> {
  return unwrapVersioned(
    await client.PUT("/api/v1/books/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
      body: book,
    }),
    m.books_the_book_could_not_be_saved(),
  )
}

/** Moves a book to trash. Permanent deletion is a separate trash action. */
export async function deleteBook(id: string, revision: number): Promise<void> {
  expectOk(
    await client.DELETE("/api/v1/books/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
    }),
    m.books_the_book_could_not_be_moved_to_trash(),
  )
}

export async function listBookSourceRecipes(id: string): Promise<BookSourceRecipe[]> {
  return unwrapItems(
    await client.GET("/api/v1/books/{id}/sourced-recipes", {
      params: { path: { id } },
    }),
    m.books_recipes_sourced_from_this_book_could_not_be_loaded(),
  )
}

export async function listBookRevisions(
  id: string,
  options: { limit?: number; offset?: number } = {},
): Promise<BookRevisionSummary[]> {
  const { limit = 50, offset = 0 } = options
  return unwrapItems(
    await client.GET("/api/v1/books/{id}/revisions", {
      params: { path: { id }, query: { limit, offset } },
    }),
    m.books_the_book_history_could_not_be_loaded(),
  )
}

export async function getBookRevision(id: string, revision: number): Promise<BookRevision> {
  return unwrap(
    await client.GET("/api/v1/books/{id}/revisions/{revision}", {
      params: { path: { id, revision } },
    }),
    m.books_that_version_of_the_book_could_not_be_loaded(),
  )
}

export async function restoreBookRevision(
  id: string,
  revision: number,
  currentRevision: number,
): Promise<Versioned<Book>> {
  return unwrapVersioned(
    await client.POST("/api/v1/books/{id}/revisions/{revision}/restore", {
      params: { path: { id, revision }, header: { "If-Match": ifMatch(currentRevision) } },
    }),
    m.books_that_version_of_the_book_could_not_be_restored(),
  )
}

/** Rebuilds the replace payload from a read, dropping server-owned fields. */
export function bookInput(book: Book): BookInput {
  return {
    kind: book.kind,
    title: book.title,
    subtitle: book.subtitle,
    description: book.description,
    cover_asset_id: book.cover_asset_id,
    author: book.author,
    editor: book.editor,
    edition: book.edition,
    isbn: book.isbn,
    publisher: book.publisher,
    publication_date: book.publication_date,
    sections: (book.sections ?? []).map((section) => ({ ...section })),
    entries: (book.entries ?? []).map((entry) => ({
      id: entry.id,
      section_id: entry.section_id,
      library_entry_id: entry.library_entry_id,
      position: entry.position,
      note: entry.note,
    })),
  }
}
