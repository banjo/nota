import { describe, expect, test, vi } from "vite-plus/test";
import { createHttpClient } from "../src/http.ts";
import { createMygoClient, type MygoBindings } from "../src/mygo.ts";

const fn = () => vi.fn(async () => undefined as never);

function bindings() {
  const Desktop = {
    hideCapture: fn(),
    showMain: fn(),
    revealVault: fn(),
    setAppearance: fn(),
    needsOnboarding: vi.fn(async () => true),
    defaultLocation: vi.fn(async () => "/home/me/Documents"),
    vaultSource: vi.fn(async () => "default"),
    pickFolder: vi.fn(async () => "/home/me/Notes"),
    inspectFolder: vi.fn(async (path: string) => ({
      path,
      exists: false,
      isDir: false,
      empty: false,
      notes: 0,
      files: 0,
      truncated: false,
      writable: true,
    })),
    checkLocation: vi.fn(async () => ""),
    createVault: vi.fn(async (location: string, name: string) => ({
      path: `${location}/${name}`,
      name,
    })),
    useVault: vi.fn(async (path: string) => ({ path, name: "Notes" })),
  };
  const b = {
    Vault: {} as MygoBindings["Vault"],
    Account: {} as MygoBindings["Account"],
    Desktop,
    events: {
      changed: { on: () => () => {} },
      syncStatus: { on: () => () => {} },
      account: { on: () => () => {} },
      command: { on: () => () => {} },
    },
  } satisfies MygoBindings;
  return { b, Desktop };
}

describe("the desktop client's setup calls", () => {
  test("go to the Desktop service with their arguments", async () => {
    const { b, Desktop } = bindings();
    const setup = createMygoClient(b).shell!.setup!;
    expect(await setup.needsOnboarding()).toBe(true);
    expect(await setup.defaultLocation()).toBe("/home/me/Documents");
    expect(await setup.vaultSource()).toBe("default");
    expect(await setup.pickFolder("Choose")).toBe("/home/me/Notes");
    expect(Desktop.pickFolder).toHaveBeenCalledWith("Choose", ""); // no start folder
    await setup.pickFolder("Choose", "/home/me");
    expect(Desktop.pickFolder).toHaveBeenLastCalledWith("Choose", "/home/me");
    expect((await setup.inspectFolder("/x")).path).toBe("/x");
    expect(await setup.checkLocation("/usr")).toBe("");
    expect(await setup.createVault("/home/me", "Notes", "Inbox", true)).toEqual({
      path: "/home/me/Notes",
      name: "Notes",
    });
    expect(Desktop.createVault).toHaveBeenCalledWith("/home/me", "Notes", "Inbox", true);
    await setup.useVault("/home/me/Writing", "Capture", false);
    expect(Desktop.useVault).toHaveBeenCalledWith("/home/me/Writing", "Capture", false);
  });

  test("the web client has no setup: the server is the vault", () => {
    const c = createHttpClient({
      baseUrl: "https://example.com",
      vault: "v",
      fetch: vi.fn() as unknown as typeof fetch,
    });
    expect(c.shell).toBeUndefined();
  });
});

describe("the desktop client's files", () => {
  test("load from the notafile:// scheme", () => {
    const c = createMygoClient(bindings().b);
    expect(c.fileUrl!("Attachments/Möte 1.png")).toBe(
      "notafile://vault/Attachments/M%C3%B6te%201.png",
    );
    expect(c.fileUrl!("skiss.png", "Projekt/Plan.md")).toBe(
      "notafile://vault/skiss.png?from=Projekt%2FPlan.md",
    );
  });

  test("upload by POST to it, and fail with the server's message", async () => {
    const fetch = vi.fn(async (url: string) =>
      url.endsWith("x.exe")
        ? new Response("x.exe: not an image Nota can show\n", { status: 400 })
        : new Response(JSON.stringify({ path: "Attachments/a b.png" }), { status: 201 }),
    );
    vi.stubGlobal("fetch", fetch);
    try {
      const c = createMygoClient(bindings().b);
      const blob = new Blob(["PNG"]);
      expect(await c.attach!("a b.png", blob)).toBe("Attachments/a b.png");
      expect(fetch).toHaveBeenCalledWith("notafile://attach/a%20b.png", {
        method: "POST",
        body: blob,
      });
      await expect(c.attach!("x.exe", blob)).rejects.toMatchObject({
        message: "x.exe: not an image Nota can show",
        code: "invalid_path",
      });
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
