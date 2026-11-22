export { NotaError, type NotesClient } from "./client.ts";
export { createHttpAuth, type HttpAuth, type HttpAuthOptions } from "./auth.ts";
export {
  createPasskey,
  creationOptionsFromJSON,
  credentialToJSON,
  fromBase64Url,
  getPasskey,
  passkeysSupported,
  requestOptionsFromJSON,
  toBase64Url,
} from "./webauthn.ts";
export {
  createHttpClient,
  escapePath,
  PROTOCOL,
  type EventSourceLike,
  type HttpClientOptions,
} from "./http.ts";
export { createMygoClient, type MygoBindings, type MygoEvent } from "./mygo.ts";
export type * from "./types.ts";
