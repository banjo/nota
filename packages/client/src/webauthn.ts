// WebAuthn between the browser and the server's JSON options
// (docs/api.md, "Accounts"). go-webauthn sends binary fields as
// base64url strings, while navigator.credentials wants ArrayBuffers, and
// the credential that comes back has to be JSON again for `finish`.

/** base64url (no padding) of a byte array. */
export function toBase64Url(bytes: ArrayBuffer | Uint8Array): string {
  const b = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  let s = "";
  for (const byte of b) s += String.fromCharCode(byte);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/** Bytes of a base64url (or base64) string. */
export function fromBase64Url(value: string): Uint8Array {
  const b64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const bin = atob(b64.padEnd(Math.ceil(b64.length / 4) * 4, "="));
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function buffer(value: string): ArrayBuffer {
  const u = fromBase64Url(value);
  return u.buffer.slice(u.byteOffset, u.byteOffset + u.byteLength) as ArrayBuffer;
}

interface CredentialDescriptorJSON {
  type?: string;
  id: string;
  transports?: string[];
}

interface CreationOptionsJSON {
  challenge: string;
  rp: { id?: string; name: string };
  user: { id: string; name: string; displayName: string };
  pubKeyCredParams: PublicKeyCredentialParameters[];
  timeout?: number;
  excludeCredentials?: CredentialDescriptorJSON[];
  authenticatorSelection?: AuthenticatorSelectionCriteria;
  attestation?: AttestationConveyancePreference;
  hints?: string[];
  extensions?: AuthenticationExtensionsClientInputs;
}

interface RequestOptionsJSON {
  challenge: string;
  rpId?: string;
  timeout?: number;
  allowCredentials?: CredentialDescriptorJSON[];
  userVerification?: UserVerificationRequirement;
  hints?: string[];
  extensions?: AuthenticationExtensionsClientInputs;
}

function descriptor(d: CredentialDescriptorJSON): PublicKeyCredentialDescriptor {
  return {
    type: "public-key",
    id: buffer(d.id),
    ...(d.transports ? { transports: d.transports as AuthenticatorTransport[] } : {}),
  };
}

/** The inner publicKey of `{publicKey: …}`, as the server sends either form. */
function publicKey(json: unknown): unknown {
  const o = json as { publicKey?: unknown } | null;
  return o && typeof o === "object" && "publicKey" in o && o.publicKey ? o.publicKey : json;
}

/** The server's creation options as PublicKeyCredentialCreationOptions. */
export function creationOptionsFromJSON(json: unknown): PublicKeyCredentialCreationOptions {
  const o = publicKey(json) as CreationOptionsJSON;
  return {
    challenge: buffer(o.challenge),
    rp: o.rp,
    user: {
      id: buffer(o.user.id),
      name: o.user.name,
      displayName: o.user.displayName,
    },
    pubKeyCredParams: o.pubKeyCredParams,
    ...(o.timeout !== undefined ? { timeout: o.timeout } : {}),
    ...(o.excludeCredentials?.length
      ? { excludeCredentials: o.excludeCredentials.map(descriptor) }
      : {}),
    ...(o.authenticatorSelection ? { authenticatorSelection: o.authenticatorSelection } : {}),
    ...(o.attestation ? { attestation: o.attestation } : {}),
    ...(o.extensions ? { extensions: o.extensions } : {}),
  };
}

/** The server's request options as PublicKeyCredentialRequestOptions. */
export function requestOptionsFromJSON(json: unknown): PublicKeyCredentialRequestOptions {
  const o = publicKey(json) as RequestOptionsJSON;
  return {
    challenge: buffer(o.challenge),
    ...(o.rpId ? { rpId: o.rpId } : {}),
    ...(o.timeout !== undefined ? { timeout: o.timeout } : {}),
    ...(o.allowCredentials?.length ? { allowCredentials: o.allowCredentials.map(descriptor) } : {}),
    ...(o.userVerification ? { userVerification: o.userVerification } : {}),
    ...(o.extensions ? { extensions: o.extensions } : {}),
  };
}

/** Whether the page can use passkeys at all. */
export function passkeysSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof PublicKeyCredential !== "undefined" &&
    typeof navigator.credentials?.create === "function"
  );
}

/**
 * The credential as the server's `finish` wants it. Browsers with
 * `toJSON()` (Chrome 130+, Safari 18+) do this themselves; the manual
 * path keeps older WebKit working.
 */
export function credentialToJSON(credential: PublicKeyCredential): Record<string, unknown> {
  const toJSON = (credential as unknown as { toJSON?: () => unknown }).toJSON;
  if (typeof toJSON === "function") return toJSON.call(credential) as Record<string, unknown>;
  const response = credential.response;
  const base = {
    id: credential.id,
    rawId: toBase64Url(credential.rawId),
    type: credential.type,
    clientExtensionResults: credential.getClientExtensionResults(),
  };
  if (response instanceof AuthenticatorAttestationResponse) {
    return {
      ...base,
      response: {
        clientDataJSON: toBase64Url(response.clientDataJSON),
        attestationObject: toBase64Url(response.attestationObject),
        transports: response.getTransports?.() ?? [],
      },
    };
  }
  const assertion = response as AuthenticatorAssertionResponse;
  return {
    ...base,
    response: {
      clientDataJSON: toBase64Url(assertion.clientDataJSON),
      authenticatorData: toBase64Url(assertion.authenticatorData),
      signature: toBase64Url(assertion.signature),
      userHandle: assertion.userHandle ? toBase64Url(assertion.userHandle) : null,
    },
  };
}

/** A passkey for a new account. */
export async function createPasskey(
  options: unknown,
  signal?: AbortSignal,
): Promise<Record<string, unknown>> {
  const publicKey = creationOptionsFromJSON(options);
  const cred = (await navigator.credentials.create({
    publicKey,
    ...(signal ? { signal } : {}),
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error("the passkey was not created");
  return credentialToJSON(cred);
}

/** Answers a passkey challenge. */
export async function getPasskey(
  options: unknown,
  signal?: AbortSignal,
): Promise<Record<string, unknown>> {
  const publicKey = requestOptionsFromJSON(options);
  const cred = (await navigator.credentials.get({
    publicKey,
    ...(signal ? { signal } : {}),
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error("no passkey was chosen");
  return credentialToJSON(cred);
}
