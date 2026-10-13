import { m } from "../../lib/messages.ts"
import { accountRequest } from "./account-state"

type DescriptorJSON = Omit<PublicKeyCredentialDescriptor, "id"> & { id: string }
type CreationJSON = Omit<
  PublicKeyCredentialCreationOptions,
  "challenge" | "user" | "excludeCredentials"
> & {
  challenge: string
  user: Omit<PublicKeyCredentialUserEntity, "id"> & { id: string }
  excludeCredentials?: DescriptorJSON[]
}
type RequestJSON = Omit<PublicKeyCredentialRequestOptions, "challenge" | "allowCredentials"> & {
  challenge: string
  allowCredentials?: DescriptorJSON[]
}
function decode(value: string): ArrayBuffer {
  const bytes = Uint8Array.from(atob(value.replace(/-/g, "+").replace(/_/g, "/")), (c) =>
    c.charCodeAt(0),
  )
  return bytes.buffer
}
function encode(value: ArrayBuffer): string {
  return btoa(String.fromCharCode(...new Uint8Array(value)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "")
}
function serialize(credential: PublicKeyCredential) {
  const response = credential.response
  return {
    id: credential.id,
    rawId: encode(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
    response:
      response instanceof AuthenticatorAttestationResponse
        ? {
            clientDataJSON: encode(response.clientDataJSON),
            attestationObject: encode(response.attestationObject),
            transports: response.getTransports(),
          }
        : {
            clientDataJSON: encode(response.clientDataJSON),
            authenticatorData: encode(
              (response as AuthenticatorAssertionResponse).authenticatorData,
            ),
            signature: encode((response as AuthenticatorAssertionResponse).signature),
            userHandle: (response as AuthenticatorAssertionResponse).userHandle
              ? encode((response as AuthenticatorAssertionResponse).userHandle!)
              : null,
          },
  }
}
export function passkeysSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    window.isSecureContext &&
    typeof PublicKeyCredential !== "undefined"
  )
}
export async function registerPasskey(name: string): Promise<void> {
  const { publicKey } = await accountRequest<{ publicKey: CreationJSON }>(
    "/account/passkeys/register/begin",
    "POST",
    {},
  )
  const credential = await navigator.credentials.create({
    publicKey: {
      ...publicKey,
      challenge: decode(publicKey.challenge),
      user: { ...publicKey.user, id: decode(publicKey.user.id) },
      excludeCredentials: publicKey.excludeCredentials?.map((c) => ({ ...c, id: decode(c.id) })),
    },
  })
  if (!credential) throw new Error(m.passkeys_registration_cancelled())
  await accountRequest(
    `/account/passkeys/register/finish?name=${encodeURIComponent(name.trim())}`,
    "POST",
    serialize(credential as PublicKeyCredential),
  )
}
// Serializing only the begin requests prevents a cancelled conditional request's
// late Set-Cookie response from replacing the manually requested challenge.
let loginBeginQueue: Promise<unknown> = Promise.resolve()
export async function signInWithPasskey(conditional = false, signal?: AbortSignal): Promise<void> {
  const begin = loginBeginQueue
    .catch(() => {})
    .then(async () => {
      if (signal?.aborted) return null
      return accountRequest<{ publicKey: RequestJSON }>("/account/passkeys/login/begin", "POST", {})
    })
  loginBeginQueue = begin
  const options = await begin
  if (!options || signal?.aborted) return
  const { publicKey } = options
  const credential = await navigator.credentials.get({
    signal,
    mediation: conditional ? "conditional" : "optional",
    publicKey: {
      ...publicKey,
      challenge: decode(publicKey.challenge),
      allowCredentials: publicKey.allowCredentials?.map((c) => ({ ...c, id: decode(c.id) })),
    },
  })
  if (!credential) throw new Error(m.passkeys_sign_in_cancelled())
  await accountRequest(
    "/account/passkeys/login/finish",
    "POST",
    serialize(credential as PublicKeyCredential),
  )
}
