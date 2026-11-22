import { NotaError } from "./client.ts";
import type {
  AuthApi,
  AuthConfig,
  AuthSession,
  Device,
  PasskeyInfo,
  PendingCode,
  VaultSummary,
} from "./types.ts";
import { createPasskey, getPasskey } from "./webauthn.ts";

export interface HttpAuthOptions {
  /** The API server, e.g. https://nota.app.banjoanton.com. */
  baseUrl: string;
  /** Defaults to globalThis.fetch. */
  fetch?: typeof fetch;
  /** Called when a request answers 401, so the UI can show sign-in again. */
  onExpired?: () => void;
  /** Called when the server answers, or when it cannot be reached. */
  onStatus?: (up: boolean) => void;
}

/** The web app's auth API, with a way to report an expired session. */
export interface HttpAuth extends AuthApi {
  /** Treats the session as gone (a 401 anywhere). */
  invalidate(): void;
}

/** The protocol version the client speaks (docs/api.md). */
const PROTOCOL = "1";

interface Me {
  user: { id: number; email: string; created_at: string };
  via: string;
  device?: string;
  needs_passkey: boolean;
  passkeys: number;
  recovery_codes_left: number;
}

interface BeginResponse {
  ceremony: string;
  options: unknown;
}

function session(me: Me): AuthSession {
  return {
    user: { id: me.user.id, email: me.user.email, createdAt: me.user.created_at },
    needsPasskey: me.needs_passkey,
    passkeys: me.passkeys,
    recoveryCodesLeft: me.recovery_codes_left,
  };
}

/**
 * The web app's account calls (docs/api.md): passkey sign-up and
 * sign-in, recovery codes, passkeys, devices, and the approvals behind
 * `nota.banjoanton.com/app/connect`. The session is an HttpOnly cookie;
 * every call sends `Nota-Client: web` (CSRF) and credentials.
 */
export function createHttpAuth(options: HttpAuthOptions): HttpAuth {
  const doFetch = options.fetch ?? globalThis.fetch.bind(globalThis);
  const root = options.baseUrl.replace(/\/+$/, "");
  const listeners = new Set<() => void>();

  async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = {
      "Nota-Protocol": PROTOCOL,
      "Nota-Client": "web",
      Accept: "application/json",
    };
    if (body !== undefined) headers["Content-Type"] = "application/json";
    let res: Response;
    try {
      res = await doFetch(root + path, {
        method,
        headers,
        credentials: "include",
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch (e) {
      options.onStatus?.(false);
      throw new NotaError(
        e instanceof Error && e.message ? e.message : "the server did not answer",
        "unreachable",
      );
    }
    options.onStatus?.(true);
    if (!res.ok) {
      let err: { code?: string; message?: string } | undefined;
      try {
        err = (await res.json()) as typeof err;
      } catch {
        err = undefined;
      }
      if (res.status === 401 && path !== "/v1/auth/me") options.onExpired?.();
      throw new NotaError(
        err?.message ?? `${res.status} ${res.statusText}`,
        err?.code ?? `http_${res.status}`,
        res.status,
        err,
      );
    }
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  }

  return {
    devicesUrl: () => "/app/devices",

    invalidate() {
      for (const l of listeners) l();
    },

    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },

    config: () => request<AuthConfig>("GET", "/v1/auth/config"),

    async session(): Promise<AuthSession | null> {
      try {
        return session(await request<Me>("GET", "/v1/auth/me"));
      } catch (e) {
        if (e instanceof NotaError && e.status === 401) return null;
        throw e;
      }
    },

    async register(input) {
      const begun = await request<BeginResponse>("POST", "/v1/auth/register/begin", {
        email: input.email,
        ...(input.invite ? { invite: input.invite } : {}),
      });
      const credential = await createPasskey(begun.options);
      const out = await request<{
        user: Me["user"];
        recovery_codes: string[];
      }>("POST", "/v1/auth/register/finish", {
        ceremony: begun.ceremony,
        credential,
        name: input.name,
      });
      // No invalidate() here: the gate must stay on the recovery-codes
      // screen, which is the only time the codes exist. The UI moves on
      // itself once they are saved.
      return {
        user: { id: out.user.id, email: out.user.email, createdAt: out.user.created_at },
        recoveryCodes: out.recovery_codes,
      };
    },

    async login(email?: string) {
      const begun = await request<BeginResponse>(
        "POST",
        "/v1/auth/login/begin",
        email ? { email } : {},
      );
      const credential = await getPasskey(begun.options);
      const out = await request<{
        user: Me["user"];
        needs_passkey?: boolean;
        recovery_codes_left: number;
      }>("POST", "/v1/auth/login/finish", { ceremony: begun.ceremony, credential });
      this.invalidate();
      return {
        user: { id: out.user.id, email: out.user.email, createdAt: out.user.created_at },
        needsPasskey: out.needs_passkey ?? false,
        passkeys: 1,
        recoveryCodesLeft: out.recovery_codes_left,
      };
    },

    async recovery(email: string, code: string) {
      const out = await request<{
        user: Me["user"];
        needs_passkey: boolean;
        recovery_codes_left: number;
      }>("POST", "/v1/auth/recovery", { email, code });
      this.invalidate();
      return {
        user: { id: out.user.id, email: out.user.email, createdAt: out.user.created_at },
        needsPasskey: out.needs_passkey,
        passkeys: 0,
        recoveryCodesLeft: out.recovery_codes_left,
      };
    },

    async addPasskey(name: string) {
      const begun = await request<BeginResponse>("POST", "/v1/auth/passkeys/begin", {});
      const credential = await createPasskey(begun.options);
      const out = await request<{ passkeys: PasskeyInfo[] }>("POST", "/v1/auth/passkeys/finish", {
        ceremony: begun.ceremony,
        credential,
        name,
      });
      this.invalidate();
      return out.passkeys;
    },

    passkeys: async () =>
      (await request<{ passkeys: PasskeyInfo[] }>("GET", "/v1/auth/passkeys")).passkeys,

    async removePasskey(id: string) {
      await request("DELETE", `/v1/auth/passkeys/${encodeURIComponent(id)}`);
    },

    async logout() {
      await request("POST", "/v1/auth/logout");
      this.invalidate();
    },

    async devices() {
      const out = await request<{
        devices: {
          id: number;
          device: string;
          created_at: string;
          last_used_at?: string;
          current: boolean;
        }[];
      }>("GET", "/v1/devices");
      return out.devices.map((d): Device => ({
        id: d.id,
        device: d.device,
        createdAt: d.created_at,
        lastUsedAt: d.last_used_at,
        current: d.current,
      }));
    },

    async revokeDevice(id: number) {
      await request("DELETE", `/v1/devices/${id}`);
    },

    // The server speaks snake_case on the wire; the UI reads camelCase.
    async lookupCode(code: string) {
      const d = await request<PendingCode & { created_at?: string; expires_at?: string }>(
        "POST",
        "/v1/auth/device/lookup",
        { user_code: code },
      );
      return {
        id: d.id,
        device: d.device,
        createdAt: d.createdAt ?? d.created_at ?? "",
        expiresAt: d.expiresAt ?? d.expires_at ?? "",
      };
    },

    async approveCode(code: string, approve: boolean) {
      await request("POST", "/v1/auth/device/approve", { user_code: code, approve });
    },

    async approveDesktop(input) {
      const out = await request<{ code: string; redirect: string; expires_in: number }>(
        "POST",
        "/v1/auth/connect",
        {
          code_challenge: input.challenge,
          code_challenge_method: "S256",
          device: input.device,
          redirect_uri: input.redirect,
          ...(input.state ? { state: input.state } : {}),
        },
      );
      return { code: out.code, redirect: out.redirect, expiresIn: out.expires_in };
    },

    async vaults() {
      const out = await request<{ vaults: { name: string; cursor: number; created_at: string }[] }>(
        "GET",
        "/v1/api/vaults",
      );
      return out.vaults.map((v): VaultSummary => ({
        name: v.name,
        cursor: v.cursor,
        createdAt: v.created_at,
      }));
    },

    async createVault(name: string) {
      const out = await request<{ name: string; cursor: number; created_at: string }>(
        "POST",
        "/v1/api/vaults",
        { name },
      );
      return { name: out.name, cursor: out.cursor, createdAt: out.created_at };
    },
  };
}
