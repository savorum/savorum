import { m } from "../lib/messages.ts"
import { sha256Hex } from "../lib/sha256.ts"
import { type AssetBlob, ApiError, client, registerUploadedAssetPreview, unwrap } from "./client"

/**
 * Uploads bytes under their content hash. The claimed digest and size let the
 * server reject a transfer that changed on the way rather than storing it.
 */
export async function uploadAsset(file: File): Promise<AssetBlob> {
  const bytes = await file.arrayBuffer()
  if (!file.type.startsWith("image/")) {
    throw new ApiError(m.assets_only_image_files_can_be_attached_to_a_recipe(), 415)
  }
  const sha256 = await sha256Hex(bytes)
  const asset = unwrap(
    await client.POST("/api/v1/assets", {
      params: {
        header: {
          "X-Asset-Media-Type": file.type,
          "X-Asset-SHA256": sha256,
          "X-Asset-Size": String(bytes.byteLength),
        },
      },
      body: bytes as unknown as string,
      bodySerializer: (body: unknown) => body as BodyInit,
      headers: { "Content-Type": "application/octet-stream" },
    }),
    m.assets_the_image_could_not_be_uploaded(),
  )
  registerUploadedAssetPreview(asset.id, file)
  return asset
}
