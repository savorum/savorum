import { Link } from "@tanstack/solid-router"
import { History, ImagePlus, Pencil, Plus, Printer } from "lucide-solid"
import { Show, createSignal } from "solid-js"
import { untrack } from "solid-js"
import { css } from "styled-system/css"

import { uploadAsset } from "@/api/assets"
import { assetURL, fallbackToOriginalImage } from "@/api/client"
import { downloadBookBundle } from "@/api/exports"
import { ExportMenu } from "@/components/export-menu"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { newID } from "@/lib/recipe-draft"

import { m } from "../../lib/messages.ts"
import type { BookViewProps } from "./book-types"
import type { BookEditor } from "./create-book-editor"
export function BookMetadata(
  props: BookViewProps & {
    editor: BookEditor
    arranging: boolean
    onArrange: () => void
    onFinishArranging: () => void
  },
) {
  const { draft, setDraft, editing, setEditing, setConfirmingTrash, published, credits } = untrack(
    () => props.editor,
  )
  /** What the published-cookbook fields currently say, or nothing yet. */
  const publicationSummary = () =>
    [credits(), draft.isbn].filter(Boolean).join(" · ") || m.book_metadata_nothing_recorded_yet()
  return (
    <>
      {" "}
      <div class={styles.masthead}>
        <div class={`${styles.cover} ${editing() ? styles.coverEditing : ""}`}>
          <Show
            when={draft.cover_asset_id}
            fallback={
              <span class={styles.coverTitle} aria-hidden="true">
                {draft.title}
              </span>
            }
          >
            <img
              class={styles.coverImage}
              src={assetURL(draft.cover_asset_id!, { bookID: props.book.id }, "reader")}
              onError={(event) => fallbackToOriginalImage(event.currentTarget)}
              alt=""
            />
          </Show>
          <CoverButton onUploaded={(id) => setDraft("cover_asset_id", id)} />
        </div>
        <Show
          when={!editing() || !props.arranging}
          fallback={
            <section class={styles.detailsEditor} aria-labelledby="book-details-title">
              <h1 id="book-details-title" class={styles.detailsTitle}>
                {" "}
                {m.book_metadata_book_details()}{" "}
              </h1>
              <div class={styles.fields}>
                <FormField label={m.book_metadata_title()}>
                  <Input
                    autofocus
                    value={draft.title}
                    maxLength={1000}
                    onInput={(event) => setDraft("title", event.currentTarget.value)}
                  />
                </FormField>
                <FormField label={m.book_metadata_subtitle()}>
                  <Input
                    value={draft.subtitle ?? ""}
                    onInput={(event) => setDraft("subtitle", event.currentTarget.value)}
                  />
                </FormField>
                <FormField label={m.book_metadata_about_this_book()}>
                  <Textarea
                    size="sm"
                    value={draft.description ?? ""}
                    onInput={(event) => setDraft("description", event.currentTarget.value)}
                  />
                </FormField>
                <Show when={published()}>
                  <Disclosure
                    label={m.book_metadata_published_cookbook_details()}
                    hint={publicationSummary()}
                  >
                    <div class={styles.fields}>
                      <div class={styles.pair}>
                        <FormField label={m.book_metadata_author()}>
                          <Input
                            size="sm"
                            value={draft.author ?? ""}
                            onInput={(event) => setDraft("author", event.currentTarget.value)}
                          />
                        </FormField>
                        <FormField label={m.book_metadata_editor()}>
                          <Input
                            size="sm"
                            value={draft.editor ?? ""}
                            onInput={(event) => setDraft("editor", event.currentTarget.value)}
                          />
                        </FormField>
                        <FormField label={m.book_metadata_publisher()}>
                          <Input
                            size="sm"
                            value={draft.publisher ?? ""}
                            onInput={(event) => setDraft("publisher", event.currentTarget.value)}
                          />
                        </FormField>
                        <FormField label={m.book_metadata_edition()}>
                          <Input
                            size="sm"
                            value={draft.edition ?? ""}
                            onInput={(event) => setDraft("edition", event.currentTarget.value)}
                          />
                        </FormField>
                        <FormField label={m.book_metadata_year()}>
                          <Input
                            size="sm"
                            value={draft.publication_date ?? ""}
                            placeholder="2019"
                            onInput={(event) =>
                              setDraft("publication_date", event.currentTarget.value)
                            }
                          />
                        </FormField>
                        <FormField label={m.book_metadata_isbn()}>
                          <Input
                            size="sm"
                            value={draft.isbn ?? ""}
                            onInput={(event) => setDraft("isbn", event.currentTarget.value)}
                          />
                        </FormField>
                      </div>
                    </div>
                  </Disclosure>
                </Show>
              </div>
              <div class={styles.detailsActions}>
                <Button size="sm" colorPalette="accent" onClick={() => setEditing(false)}>
                  {m.book_metadata_done_editing()}
                </Button>
                <Button
                  size="sm"
                  variant="plain"
                  colorPalette="red"
                  onClick={() => setConfirmingTrash(true)}
                >
                  {m.book_metadata_move_to_trash()}
                </Button>
              </div>
            </section>
          }
        >
          <div>
            <h1 class={styles.title}>{draft.title}</h1>
            <Show when={draft.subtitle}>
              <p class={styles.subtitle}>{draft.subtitle}</p>
            </Show>
            <Show when={credits()}>
              <p class={styles.credits}>{credits()}</p>
            </Show>
            <Show when={draft.description}>
              <p class={styles.description}>{draft.description}</p>
            </Show>
            <div class={styles.headActions}>
              <Show
                when={props.arranging}
                fallback={
                  <Button size="sm" variant="outline" onClick={props.onArrange}>
                    <Pencil aria-hidden="true" size={14} />
                    {m.book_metadata_organize_book()}
                  </Button>
                }
              >
                <Button size="sm" variant="outline" onClick={() => setEditing(true)}>
                  <Pencil aria-hidden="true" size={14} /> {m.book_metadata_book_details()}
                </Button>
              </Show>
              {/* A book prints through the same rendering the library and a
                  single recipe use, so it is the same action here. A book is
                  also a way this library is organised, and SREF has no portable
                  collection, so what leaves is the recipes in the book's order
                  rather than the book itself; the wording says exactly that. */}
              <Link
                class={buttonLink({ size: "sm", variant: "outline" })}
                to="/books/$bookId/print"
                params={{ bookId: props.book.id }}
              >
                <Printer aria-hidden="true" size={14} /> {m.book_metadata_print_book()}{" "}
              </Link>
              <ExportMenu
                choices={[
                  {
                    value: "sref-bundle",
                    get label() {
                      return m.book_metadata_recipes_as_sref_bundle()
                    },
                    start: () => downloadBookBundle(props.book.id),
                  },
                ]}
              />
              <Link
                class={buttonLink({ size: "sm", variant: "plain" })}
                to="/books/$bookId/history"
                params={{ bookId: props.book.id }}
              >
                <History aria-hidden="true" size={14} /> {m.common_earlier_versions()}{" "}
              </Link>
              <Show when={props.arranging}>
                <Button
                  size="sm"
                  variant="plain"
                  onClick={() =>
                    setDraft("sections", (sections) => [
                      ...(sections ?? []),
                      // The chapter is given its identity here so recipes can be
                      // put into it before the book is saved.
                      {
                        id: newID("k"),
                        title: m.book_metadata_new_chapter(),
                        position: (sections ?? []).length,
                      },
                    ])
                  }
                >
                  <Plus aria-hidden="true" size={14} />
                  {m.book_metadata_chapter()}
                </Button>
                <Button size="sm" variant="plain" onClick={props.onFinishArranging}>
                  {props.editor.dirty() ? m.book_metadata_save_and_close() : m.dialog_done()}
                </Button>
              </Show>
            </div>
          </div>
        </Show>
      </div>
    </>
  )
}
function CoverButton(props: { onUploaded: (assetID: string) => void }) {
  const [uploading, setUploading] = createSignal(false)
  const [uploadError, setUploadError] = createSignal<unknown>()
  let input: HTMLInputElement | undefined
  const upload = async (file: File) => {
    setUploadError(undefined)
    setUploading(true)
    try {
      const blob = await uploadAsset(file)
      props.onUploaded(blob.id)
    } catch (error) {
      setUploadError(error)
    } finally {
      setUploading(false)
      if (input) input.value = ""
    }
  }
  return (
    <>
      <input
        class={styles.fileInput}
        ref={(node) => {
          input = node
        }}
        type="file"
        accept="image/*"
        onChange={(event) => {
          const file = event.currentTarget.files?.[0]
          if (file) void upload(file)
        }}
      />
      <ChangeError error={uploadError()} />
      <IconButton
        class={styles.coverButton}
        aria-label={m.book_metadata_change_cover()}
        size="xs"
        variant="surface"
        loading={uploading()}
        onClick={() => input?.click()}
      >
        <ImagePlus aria-hidden="true" size={14} />
      </IconButton>
    </>
  )
}

const styles = {
  masthead: css({
    display: "grid",
    gap: { base: "5", md: "8" },
    gridTemplateColumns: { base: "5rem minmax(0, 1fr)", sm: "9rem minmax(0, 1fr)" },
    marginBottom: "8",
  }),

  cover: css({
    alignItems: "center",
    aspectRatio: "3 / 4",
    alignSelf: "start",
    background: "bg.muted",
    borderRadius: "l2",
    color: "border.default",
    display: "flex",
    justifyContent: "center",
    overflow: "hidden",
    position: "relative",
  }),

  coverTitle: css({
    alignSelf: "stretch",
    borderInlineStartWidth: "4px",
    borderColor: "accent.default",
    color: "fg.default",
    display: "flex",
    alignItems: "center",
    fontFamily: "editorial",
    fontSize: { base: "xs", sm: "xl" },
    lineHeight: "1.2",
    overflowWrap: "anywhere",
    padding: { base: "2", sm: "4" },
    width: "100%",
  }),
  coverImage: css({ height: "100%", objectFit: "cover", width: "100%" }),

  coverEditing: css({
    justifySelf: "start",
    maxWidth: { base: "11rem", sm: "none" },
    width: "100%",
  }),

  coverButton: css({ insetBlockEnd: "2", insetInlineEnd: "2", position: "absolute" }),

  title: css({
    fontFamily: "editorial",
    fontSize: { base: "2xl", md: "4xl" },
    fontWeight: "normal",
    letterSpacing: "-0.02em",
    lineHeight: "1.1",
    margin: "0",
    textWrap: "balance",
    overflowWrap: "anywhere",
  }),

  subtitle: css({ color: "fg.muted", fontSize: "md", marginTop: "2" }),

  credits: css({ color: "fg.muted", fontSize: "sm", marginTop: "3" }),

  description: css({
    color: "fg.default",
    fontSize: "sm",
    lineHeight: "1.6",
    marginTop: "4",
    maxWidth: "44rem",
  }),

  headActions: css({ display: "flex", flexWrap: "wrap", gap: "2", marginTop: "5" }),

  detailsEditor: css({
    maxWidth: "44rem",
    minWidth: "0",
    gridColumn: { base: "1 / -1", sm: "auto" },
  }),

  detailsTitle: css({ fontFamily: "editorial", fontSize: "xl", fontWeight: "normal", margin: "0" }),

  fields: css({ display: "grid", gap: "4", marginTop: "5" }),

  pair: css({
    display: "grid",
    gap: "3",
    gridTemplateColumns: { base: "1fr", sm: "repeat(2, minmax(0, 1fr))" },
  }),

  detailsActions: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    marginTop: "5",
  }),

  fileInput: css({ display: "none" }),
}
