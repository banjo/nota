import { describe, expect, test, vi } from "vite-plus/test";
import { createHttpClient, type EventSourceLike } from "../src/http.ts";
import { createMygoClient, type MygoBindings } from "../src/mygo.ts";

const BASE = "https://nota.app.banjoanton.com";

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });

describe("the web client's settings", () => {
  test("are the account layer, at the server's root", async () => {
    const responses = [
      json({ rev: 1, settings: { theme: "dark" } }),
      json({ rev: 2, settings: { theme: "dark", palette: "paper" } }),
      json({ rev: 3, settings: { theme: "light" } }),
    ];
    const fetch = vi.fn(async (_url: string, _init?: RequestInit) => responses.shift()!);
    const listeners = new Map<string, (e: { data: string }) => void>();
    const urls: string[] = [];
    const close = vi.fn();
    const es = (url: string): EventSourceLike => {
      urls.push(url);
      return { addEventListener: (t, l) => listeners.set(t, l), close };
    };
    const s = createHttpClient({
      baseUrl: BASE,
      vault: "Work",
      fetch: fetch as unknown as typeof globalThis.fetch,
      eventSource: es,
    }).settings!;

    expect(await s.get()).toEqual({
      file: {},
      account: { theme: "dark" },
      signedIn: true,
      path: "",
      warnings: [],
    });
    expect(fetch.mock.calls[0]![0]).toBe(`${BASE}/v1/account/settings`);

    const after = await s.update({ set: { palette: "paper" } });
    expect(after.account).toEqual({ theme: "dark", palette: "paper" });
    const [url, init] = fetch.mock.calls[1]!;
    expect(url).toBe(`${BASE}/v1/account/settings`);
    expect(init?.method).toBe("PATCH");
    expect(JSON.parse(init?.body as string)).toEqual({ set: { palette: "paper" } });
    expect(new Headers(init?.headers).get("Nota-Client")).toBe("web");

    const onChange = vi.fn();
    const off = s.subscribe(onChange);
    expect(urls).toEqual([`${BASE}/v1/account/events?protocol=1`]);
    // The revision it has is no news; a newer one is fetched.
    listeners.get("settings")!({ data: '{"rev":2}' });
    expect(fetch).toHaveBeenCalledTimes(2);
    listeners.get("settings")!({ data: '{"rev":3}' });
    await vi.waitFor(() => expect(onChange).toHaveBeenCalled());
    expect(onChange.mock.calls[0]![0].account).toEqual({ theme: "light" });
    off();
    expect(close).toHaveBeenCalled();
  });
});

describe("the desktop client's settings", () => {
  test("map the Go calls and fill in nulls", async () => {
    let fire: (p: unknown) => void = () => {};
    const Settings = {
      state: vi.fn(async () => ({
        file: null,
        account: null,
        signedIn: false,
        path: "/home/a/.config/nota/settings.toml",
        warnings: null,
      })),
      update: vi.fn(async (_p: { set: Record<string, string>; unset: string[] }) => ({
        file: { theme: "dark" },
        account: { palette: "paper" },
        signedIn: true,
        path: "p",
        warnings: ["x"],
      })),
    };
    const b = {
      Vault: {},
      Account: {},
      Desktop: {},
      Settings,
      events: {
        changed: { on: () => () => {} },
        syncStatus: { on: () => () => {} },
        account: { on: () => () => {} },
        command: { on: () => () => {} },
        settings: {
          on: (l: (p: unknown) => void) => {
            fire = l;
            return () => {};
          },
        },
      },
    } as unknown as MygoBindings;
    const s = createMygoClient(b).settings!;
    expect(await s.get()).toEqual({
      file: {},
      account: null,
      signedIn: false,
      path: "/home/a/.config/nota/settings.toml",
      warnings: [],
    });
    await s.update({ set: { palette: "paper" } });
    expect(Settings.update).toHaveBeenCalledWith({ set: { palette: "paper" }, unset: [] });
    const onChange = vi.fn();
    s.subscribe(onChange);
    fire({ file: { theme: "dark" }, account: null, signedIn: true, path: "p", warnings: null });
    expect(onChange).toHaveBeenCalledWith({
      file: { theme: "dark" },
      account: null,
      signedIn: true,
      path: "p",
      warnings: [],
    });
  });

  test("are absent in builds without them", () => {
    const b = {
      Vault: {},
      Account: {},
      Desktop: {},
      events: {
        changed: { on: () => () => {} },
        syncStatus: { on: () => () => {} },
        account: { on: () => () => {} },
        command: { on: () => () => {} },
      },
    } as unknown as MygoBindings;
    expect(createMygoClient(b).settings).toBeUndefined();
  });
});
