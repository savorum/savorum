import { ChevronLeft, ChevronRight, Eye, EyeOff, ImagePlus, Star, X } from "lucide-solid"
import { For, Show, createSignal } from "solid-js"
import { produce, type SetStoreFunction } from "solid-js/store"
import { css } from "styled-system/css"

import { uploadAsset } from "@/api/assets"
import { type Recipe, assetURL, fallbackToOriginalImage } from "@/api/client"
import { ChangeError } from "@/components/status"
import { DetailDialog } from "@/components/ui/detail-dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { formatNumber } from "@/lib/locale-format"
import { moveItem, newID } from "@/lib/recipe-draft"
import { imageCreditLine } from "@/lib/recipe-facts"

import { m } from "../../lib/messages.ts"

const styles = {
  images: css({
    display: "grid",
    gap: "5",
    gridTemplateColumns: "repeat(auto-fill, minmax(14rem, 1fr))",
  }),
  imageCard: css({ display: "grid", gap: "2", alignContent: "start", minWidth: "0" }),
  frame: css({ position: "relative" }),
  thumbnail: css({
    aspectRatio: "4 / 3",
    borderRadius: "l2",
    display: "block",
    objectFit: "cover",
    transition: "opacity 150ms ease",
    width: "100%",
  }),
  excluded: css({ opacity: "0.45" }),
  thumbnailMissing: css({
    alignItems: "center",
    aspectRatio: "4 / 3",
    background: "bg.muted",
    borderRadius: "l2",
    color: "fg.muted",
    display: "flex",
    fontSize: "sm",
    justifyContent: "center",
    margin: "0",
    padding: "4",
    textAlign: "center",
    width: "100%",
  }),
  badge: css({
    alignItems: "center",
    background: "bg.surface/90",
    borderRadius: "full",
    color: "fg.default",
    display: "inline-flex",
    fontSize: "xs",
    fontWeight: "medium",
    gap: "1",
    insetBlockStart: "2",
    insetInlineStart: "2",
    paddingBlock: "0.5",
    paddingInline: "2",
    position: "absolute",
  }),
  badgeIcon: css({ color: "accent.default" }),
  credit: css({
    color: "fg.muted",
    fontSize: "xs",
    margin: "0",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  }),
  imageActions: css({
    alignItems: "center",
    display: "flex",
    gap: "0.5",
    marginInline: "-1.5",
  }),
  spacer: css({ flex: "1" }),
  directional: css({ _rtl: { transform: "scaleX(-1)" } }),
  cover: css({ color: "accent.default" }),
  addTile: css({
    alignItems: "center",
    aspectRatio: "4 / 3",
    borderColor: "border.default",
    borderRadius: "l2",
    borderStyle: "dashed",
    borderWidth: "1.5px",
    color: "fg.muted",
    cursor: "pointer",
    display: "flex",
    flexDirection: "column",
    fontSize: "sm",
    fontWeight: "medium",
    gap: "2",
    justifyContent: "center",
    maxWidth: "20rem",
    transition: "colors",
    width: "100%",
    _hover: { borderColor: "accent.default", color: "accent.default" },
    _focusVisible: {
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "2px",
    },
    "&[data-dragging]": { borderColor: "accent.default", color: "accent.default" },
    "&[aria-busy=true]": { cursor: "progress", opacity: "0.6" },
  }),
  fileInput: css({ display: "none" }),
  provenance: css({ display: "grid", gap: "3" }),
  provenanceHint: css({ color: "fg.muted", fontSize: "xs", lineHeight: "1.5", margin: "0" }),
}

export async function attachRecipeImages(
  setDraft: SetStoreFunction<Recipe>,
  files: FileList | null,
): Promise<string[]> {
  if (!files || files.length === 0) return []
  const attached: string[] = []
  for (const file of Array.from(files)) {
    const blob = await uploadAsset(file)
    const id = newID("a")
    attached.push(id)
    setDraft(
      produce((current) => {
        current.assets = [
          ...(current.assets ?? []),
          {
            id,
            path: `assets/${blob.sha256}`,
            media_type: blob.media_type,
            sha256: blob.sha256,
            size: blob.size,
            role: "image",
            alt: "",
          },
        ]
        current.image_refs = [...(current.image_refs ?? []), id]
        if (!current.primary_image) current.primary_image = id
      }),
    )
  }
  return attached
}

export function RecipePhotosEditor(props: {
  draft: Recipe
  setDraft: SetStoreFunction<Recipe>
  entryID?: string
  onAnnounce?: (message: string) => void
}) {
  const [uploadError, setUploadError] = createSignal<unknown>()
  const [uploading, setUploading] = createSignal(false)
  const [dragging, setDragging] = createSignal(false)
  const [unavailable, setUnavailable] = createSignal<string[]>([])
  let fileInput: HTMLInputElement | undefined
  const assets = () => props.draft.assets ?? []
  const recipeImages = () => props.draft.image_refs ?? []

  const toggleRecipeImage = (assetID: string) =>
    props.setDraft(
      produce((current) => {
        const references = current.image_refs ?? []
        if (references.includes(assetID)) {
          current.image_refs = references.filter((reference) => reference !== assetID)
          if (current.image_refs.length === 0) current.image_refs = undefined
          if (current.primary_image === assetID) {
            current.primary_image = current.image_refs?.[0] ?? ""
          }
          return
        }
        current.image_refs = [...references, assetID]
      }),
    )
  const useAsCover = (assetID: string) =>
    props.setDraft(
      produce((current) => {
        if (!(current.image_refs ?? []).includes(assetID)) {
          current.image_refs = [...(current.image_refs ?? []), assetID]
        }
        current.primary_image = assetID
      }),
    )
  const moveRecipeImage = (assetID: string, offset: number) => {
    const position = recipeImages().indexOf(assetID) + offset
    props.setDraft("image_refs", (references) => {
      const items = references ?? []
      const index = items.indexOf(assetID)
      return index === -1 ? items : moveItem(items, index, index + offset)
    })
    props.onAnnounce?.(m.editor_photo_moved({ position: formatNumber(position + 1) }))
    queueMicrotask(() => {
      document
        .querySelector<HTMLInputElement>(`[data-photo-id="${CSS.escape(assetID)}"] input`)
        ?.focus()
    })
  }

  const attachImages = async (files: FileList | null) => {
    if (!files || files.length === 0) return
    setUploadError(undefined)
    setUploading(true)
    try {
      await attachRecipeImages(props.setDraft, files)
    } catch (error) {
      setUploadError(error)
    } finally {
      setUploading(false)
      if (fileInput) fileInput.value = ""
    }
  }

  const remove = (assetID: string) =>
    props.setDraft(
      produce((current) => {
        current.assets = (current.assets ?? []).filter((entry) => entry.id !== assetID)
        current.image_refs = (current.image_refs ?? []).filter((reference) => reference !== assetID)
        if (current.image_refs.length === 0) current.image_refs = undefined
        if (current.primary_image === assetID) {
          current.primary_image = current.image_refs?.[0] ?? ""
        }
        for (const section of current.instruction_sections ?? [])
          for (const step of section.steps ?? [])
            step.image_refs = (step.image_refs ?? []).filter((reference) => reference !== assetID)
      }),
    )

  return (
    <Disclosure
      label={m.photos_photos()}
      hint={assets().length ? ` (${formatNumber(assets().length)})` : undefined}
    >
      <ChangeError error={uploadError()} />
      <input
        class={styles.fileInput}
        ref={(node) => {
          fileInput = node
        }}
        type="file"
        accept="image/*"
        multiple
        onChange={(event) => void attachImages(event.currentTarget.files)}
      />
      <div class={styles.images}>
        <For each={assets()}>
          {(asset, index) => {
            const position = () => recipeImages().indexOf(asset.id)
            const included = () => position() >= 0
            const cover = () => props.draft.primary_image === asset.id
            return (
              <div class={styles.imageCard} data-photo-id={asset.id}>
                <div class={styles.frame}>
                  {/* An attachment is readable only once its recipe is saved. */}
                  <Show
                    when={!unavailable().includes(asset.id)}
                    fallback={
                      <p class={styles.thumbnailMissing}>{m.photos_preview_unavailable()}</p>
                    }
                  >
                    <img
                      onError={(event) => {
                        const before = event.currentTarget.src
                        fallbackToOriginalImage(event.currentTarget)
                        if (event.currentTarget.src === before)
                          setUnavailable((ids) => [...ids, asset.id])
                      }}
                      class={`${styles.thumbnail} ${included() ? "" : styles.excluded}`}
                      src={assetURL(
                        `sha256:${asset.sha256}`,
                        {
                          recipeID: props.entryID ?? "new",
                        },
                        "thumb",
                      )}
                      alt={asset.alt ?? ""}
                    />
                  </Show>
                  <Show
                    when={cover()}
                    fallback={
                      <Show when={!included()}>
                        <span class={styles.badge}>{m.editor_photo_not_in_recipe()}</span>
                      </Show>
                    }
                  >
                    <span class={styles.badge}>
                      <Star
                        class={styles.badgeIcon}
                        aria-hidden="true"
                        size={12}
                        fill="currentColor"
                      />
                      {m.photos_cover_photo()}
                    </span>
                  </Show>
                </div>
                <Input
                  size="sm"
                  aria-label={m.photos_describe_this_photo()}
                  placeholder={m.photos_describe_this_photo()}
                  value={asset.alt ?? ""}
                  onInput={(event) =>
                    props.setDraft("assets", index(), "alt", event.currentTarget.value)
                  }
                />
                <p class={styles.credit}>
                  {imageCreditLine(asset) || m.media_details_license_unknown()}
                </p>
                <div class={styles.imageActions}>
                  <IconButton
                    aria-label={cover() ? m.photos_cover_photo() : m.photos_use_as_cover_photo()}
                    title={cover() ? m.photos_cover_photo() : m.photos_use_as_cover_photo()}
                    aria-pressed={cover()}
                    size="xs"
                    variant="plain"
                    class={cover() ? styles.cover : undefined}
                    onClick={() => useAsCover(asset.id)}
                  >
                    <Star aria-hidden="true" size={15} fill={cover() ? "currentColor" : "none"} />
                  </IconButton>
                  <IconButton
                    aria-label={
                      included()
                        ? m.photos_remove_from_recipe_photos()
                        : m.photos_include_in_recipe_photos()
                    }
                    title={
                      included()
                        ? m.photos_remove_from_recipe_photos()
                        : m.photos_include_in_recipe_photos()
                    }
                    aria-pressed={included()}
                    size="xs"
                    variant="plain"
                    onClick={() => toggleRecipeImage(asset.id)}
                  >
                    <Show when={included()} fallback={<EyeOff aria-hidden="true" size={15} />}>
                      <Eye aria-hidden="true" size={15} />
                    </Show>
                  </IconButton>
                  <IconButton
                    aria-label={m.photos_move_recipe_photo_earlier()}
                    title={m.photos_move_recipe_photo_earlier()}
                    size="xs"
                    variant="plain"
                    disabled={position() <= 0}
                    onClick={() => moveRecipeImage(asset.id, -1)}
                  >
                    <ChevronLeft class={styles.directional} aria-hidden="true" size={15} />
                  </IconButton>
                  <IconButton
                    aria-label={m.photos_move_recipe_photo_later()}
                    title={m.photos_move_recipe_photo_later()}
                    size="xs"
                    variant="plain"
                    disabled={position() < 0 || position() === recipeImages().length - 1}
                    onClick={() => moveRecipeImage(asset.id, 1)}
                  >
                    <ChevronRight class={styles.directional} aria-hidden="true" size={15} />
                  </IconButton>
                  <span class={styles.spacer} />
                  <DetailDialog compact label={m.media_details_title()}>
                    <div class={styles.provenance}>
                      <p class={styles.provenanceHint}>{m.media_details_editor_hint()}</p>
                      <FormField label={m.media_details_credit()}>
                        <Input
                          size="sm"
                          value={asset.credit ?? ""}
                          onInput={(event) =>
                            props.setDraft("assets", index(), "credit", event.currentTarget.value)
                          }
                        />
                      </FormField>
                      <FormField label={m.media_details_creator()}>
                        <Input
                          size="sm"
                          value={asset.creator ?? ""}
                          onInput={(event) =>
                            props.setDraft("assets", index(), "creator", event.currentTarget.value)
                          }
                        />
                      </FormField>
                      <FormField label={m.media_details_rights()}>
                        <Input
                          size="sm"
                          value={asset.rights ?? ""}
                          onInput={(event) =>
                            props.setDraft("assets", index(), "rights", event.currentTarget.value)
                          }
                        />
                      </FormField>
                      <FormField label={m.media_details_license_name()}>
                        <Input
                          size="sm"
                          value={asset.license?.name ?? ""}
                          onInput={(event) => {
                            const name = event.currentTarget.value
                            props.setDraft("assets", index(), "license", (license) => ({
                              ...license,
                              name,
                            }))
                          }}
                        />
                      </FormField>
                      <FormField label={m.media_details_license_url()}>
                        <Input
                          size="sm"
                          type="url"
                          value={asset.license?.url ?? ""}
                          onInput={(event) => {
                            const url = event.currentTarget.value
                            props.setDraft("assets", index(), "license", (license) => ({
                              ...license,
                              url,
                            }))
                          }}
                        />
                      </FormField>
                      <FormField label={m.media_details_source()}>
                        <Input
                          size="sm"
                          type="url"
                          value={asset.source_url ?? ""}
                          onInput={(event) =>
                            props.setDraft(
                              "assets",
                              index(),
                              "source_url",
                              event.currentTarget.value,
                            )
                          }
                        />
                      </FormField>
                    </div>
                  </DetailDialog>
                  <IconButton
                    aria-label={m.photos_remove_photo()}
                    title={m.photos_remove_photo()}
                    size="xs"
                    variant="plain"
                    onClick={() => remove(asset.id)}
                  >
                    <X aria-hidden="true" size={15} />
                  </IconButton>
                </div>
              </div>
            )
          }}
        </For>
        <button
          type="button"
          class={styles.addTile}
          aria-busy={uploading()}
          disabled={uploading()}
          data-dragging={dragging() ? "" : undefined}
          onClick={() => fileInput?.click()}
          onDragOver={(event) => {
            if (!event.dataTransfer?.types.includes("Files")) return
            event.preventDefault()
            setDragging(true)
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(event) => {
            event.preventDefault()
            setDragging(false)
            void attachImages(event.dataTransfer?.files ?? null)
          }}
        >
          <ImagePlus aria-hidden="true" size={22} />
          {m.photos_add_photos()}
        </button>
      </div>
    </Disclosure>
  )
}
