import { sha256 } from "@noble/hashes/sha2.js"

/** Lowercase hex SHA-256 of the bytes. */
export async function sha256Hex(bytes: ArrayBuffer): Promise<string> {
  const hash =
    typeof crypto !== "undefined" && crypto.subtle
      ? new Uint8Array(await crypto.subtle.digest("SHA-256", bytes))
      : sha256(new Uint8Array(bytes))
  return Array.from(hash, (value) => value.toString(16).padStart(2, "0")).join("")
}
