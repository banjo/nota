import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import {
  createHttpAuth,
  creationOptionsFromJSON,
  fromBase64Url,
  requestOptionsFromJSON,
  toBase64Url,
} from "../src/index.ts";
import type { NotaError } from "../src/index.ts";

const BASE = "https://nota.app.banjoanton.com";

function json(body: unknown, status = 200): Response {
  return new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function mockFetch(...responses: Response[]) {
  const fn = vi.fn(async (_url: string | URL | Request, _init?: RequestInit) => {
    const r = responses.shift();
    if (!r) throw new Error("unexpected request");
    return r;
  });
  const call = (i: number) => {
    const [url, init] = fn.mock.calls[i]!;
    const headers = new Headers(init?.headers);
    return {
      url: new URL(url instanceof Request ? url.url : url.toString()),
      method: init?.method,
      headers,
      credentials: init?.credentials,
      body: typeof init?.body === "string" ? (JSON.parse(init.body) as unknown) : undefined,
    };
  };
  return { fn, call };
}

const options = {
  challenge: toBase64Url(new Uint8Array([1, 2, 3])),
  rp: { id: "nota.banjoanton.com", name: "Nota" },
  user: { id: toBase64Url(new Uint8Array([9, 9])), name: "a@b.c", displayName: "a@b.c" },
  pubKeyCredParams: [{ type: "public-key", alg: -7 }],
  excludeCredentials: [{ type: "public-key", id: toBase64Url(new Uint8Array([5])) }],
};

/** A fake authenticator: records the publicKey it was given. */
function fakeAuthenticator() {
  const created: PublicKeyCredentialCreationOptions[] = [];
  const credential = {
    id: "cred-1",
    toJSON: () => ({ id: "cred-1", type: "public-key", response: { clientDataJSON: "x" } }),
  } as unknown as PublicKeyCredential;
  const create = vi.fn(async (opts: CredentialCreationOptions) => {
    created.push(opts.publicKey!);
    return credential;
  });
  vi.stubGlobal("navigator", { credentials: { create, get: vi.fn() } });
  return { created, create };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("webauthn options", () => {
  test("accepts the {publicKey} wrapper the server sends", () => {
    const inner = creationOptionsFromJSON({ publicKey: options });
    expect(new Uint8Array(inner.challenge as ArrayBuffer)).toEqual(new Uint8Array([1, 2, 3]));
    expect(inner.rp.id).toBe("nota.banjoanton.com");
    const request = requestOptionsFromJSON({
      publicKey: { challenge: options.challenge, rpId: "nota.banjoanton.com", allowCredentials: [] },
    });
    expect(new Uint8Array(request.challenge as ArrayBuffer)).toEqual(new Uint8Array([1, 2, 3]));
    expect(request.rpId).toBe("nota.banjoanton.com");
  });
});

describe("base64url", () => {
  test("round-trips bytes and strips padding", () => {
    const bytes = new Uint8Array([0, 1, 250, 251, 252, 253, 254, 255]);
    const s = toBase64Url(bytes);
    expect(s).not.toContain("=");
    expect(s).not.toContain("+");
    expect(s).not.toContain("/");
    expect([...fromBase64Url(s)]).toEqual([...bytes]);
    expect(toBase64Url(new Uint8Array([1]))).toBe("AQ");
  });
});

describe("createHttpAuth", () => {
  test("sends the protocol, CSRF header and credentials", async () => {
    const f = mockFetch(
      json({
        user: { id: 1, email: "a@b.c", created_at: "2026-11-22T10:00:00Z" },
        via: "session",
        needs_passkey: false,
        passkeys: 2,
        recovery_codes_left: 9,
      }),
    );
    const auth = createHttpAuth({ baseUrl: `${BASE}/`, fetch: f.fn as unknown as typeof fetch });
    const s = await auth.session();
    expect(s).toMatchObject({ user: { email: "a@b.c" }, passkeys: 2, needsPasskey: false });
    const c = f.call(0);
    expect(c.url.toString()).toBe(`${BASE}/v1/auth/me`);
    expect(c.credentials).toBe("include");
    expect(c.headers.get("Nota-Client")).toBe("web");
    expect(c.headers.get("Nota-Protocol")).toBe("1");
  });

  test("a 401 on the session is signed out, other 401s report expiry", async () => {
    const expired = vi.fn();
    const f = mockFetch(json({ code: "unauthorized", message: "sign in first" }, 401));
    const auth = createHttpAuth({
      baseUrl: BASE,
      fetch: f.fn as unknown as typeof fetch,
      onExpired: expired,
    });
    expect(await auth.session()).toBeNull();
    expect(expired).not.toHaveBeenCalled();
    const f2 = mockFetch(json({ code: "unauthorized", message: "expired" }, 401));
    const auth2 = createHttpAuth({
      baseUrl: BASE,
      fetch: f2.fn as unknown as typeof fetch,
      onExpired: expired,
    });
    await expect(auth2.devices()).rejects.toMatchObject({ status: 401 });
    expect(expired).toHaveBeenCalledTimes(1);
  });

  test("a network failure becomes an unreachable error", async () => {
    const auth = createHttpAuth({
      baseUrl: BASE,
      fetch: (async () => {
        throw new TypeError("Failed to fetch");
      }) as unknown as typeof fetch,
    });
    await expect(auth.session()).rejects.toMatchObject({ code: "unreachable" });
  });

  test("signs up: begin, create a passkey, finish with its name", async () => {
    const { created } = fakeAuthenticator();
    const f = mockFetch(
      json({ ceremony: "c1", options }),
      json({
        user: { id: 7, email: "a@b.c", created_at: "2026-11-22T10:00:00Z" },
        recovery_codes: ["aaaa-bbbb", "cccc-dddd"],
      }),
    );
    const events: number[] = [];
    const auth = createHttpAuth({ baseUrl: BASE, fetch: f.fn as unknown as typeof fetch });
    auth.subscribe(() => events.push(1));
    const out = await auth.register({
      email: "a@b.c",
      invite: "INV-1",
      name: "MacBook Air",
    });
    expect(out.recoveryCodes).toEqual(["aaaa-bbbb", "cccc-dddd"]);
    // The recovery codes are shown once: the gate must not move on yet.
    expect(events).toEqual([]);
    expect(f.call(0).body).toEqual({ email: "a@b.c", invite: "INV-1" });
    expect(f.call(1).body).toEqual({
      ceremony: "c1",
      credential: { id: "cred-1", type: "public-key", response: { clientDataJSON: "x" } },
      name: "MacBook Air",
    });
    // The challenge and the excluded credential arrive as bytes.
    expect(new Uint8Array(created[0]!.challenge as ArrayBuffer)).toEqual(new Uint8Array([1, 2, 3]));
    expect(new Uint8Array(created[0]!.excludeCredentials![0]!.id as ArrayBuffer)).toEqual(
      new Uint8Array([5]),
    );
  });

  test("passes an invite error through with its code", async () => {
    const f = mockFetch(json({ code: "invalid_invite", message: "the invite is not valid" }, 403));
    const auth = createHttpAuth({ baseUrl: BASE, fetch: f.fn as unknown as typeof fetch });
    const err = (await auth
      .register({ email: "a@b.c", invite: "nope", name: "Laptop" })
      .catch((e: unknown) => e)) as NotaError;
    expect(err.code).toBe("invalid_invite");
    expect(err.status).toBe(403);
  });

  test("approves a desktop sign-in with the PKCE challenge", async () => {
    const f = mockFetch(
      json({ code: "one-time", redirect: "nota://auth?code=one-time&state=s", expires_in: 120 }),
    );
    const auth = createHttpAuth({ baseUrl: BASE, fetch: f.fn as unknown as typeof fetch });
    const out = await auth.approveDesktop({
      challenge: "abc",
      device: "laptop",
      redirect: "nota://auth",
      state: "s",
    });
    expect(out).toEqual({
      code: "one-time",
      redirect: "nota://auth?code=one-time&state=s",
      expiresIn: 120,
    });
    expect(f.call(0).url.pathname).toBe("/v1/auth/connect");
    expect(f.call(0).body).toEqual({
      code_challenge: "abc",
      code_challenge_method: "S256",
      device: "laptop",
      redirect_uri: "nota://auth",
      state: "s",
    });
  });

  test("lists and revokes devices", async () => {
    const f = mockFetch(
      json({
        devices: [
          {
            id: 4,
            device: "laptop",
            created_at: "2026-11-20T08:00:00Z",
            last_used_at: "2026-11-22T09:00:00Z",
            current: false,
          },
        ],
      }),
      json({}, 204),
    );
    const auth = createHttpAuth({ baseUrl: BASE, fetch: f.fn as unknown as typeof fetch });
    expect(await auth.devices()).toEqual([
      {
        id: 4,
        device: "laptop",
        createdAt: "2026-11-20T08:00:00Z",
        lastUsedAt: "2026-11-22T09:00:00Z",
        current: false,
      },
    ]);
    await auth.revokeDevice(4);
    expect(f.call(1).method).toBe("DELETE");
    expect(f.call(1).url.pathname).toBe("/v1/devices/4");
  });
});
