import { describe, expect, test, vi } from "vite-plus/test";
import { createHttpClient, escapePath, NotaError, type EventSourceLike } from "../src/index.ts";
import type { Note } from "../src/index.ts";

const BASE = "https://nota.app.banjoanton.com";
const ROOT = `${BASE}/v1/api/vaults/Anteckningar`;

function note(over: Partial<Note> = {}): Note {
  return {
    path: "Projekt/Nota/Sync-protokoll.md",
    folder: "Projekt/Nota",
    title: "Sync-protokoll",
    tags: ["nota", "sync"],
    properties: { status: "draft" },
    modified: "2026-11-22T09:42:00Z",
    size: 120,
    version: 7,
    hash: "abc",
    body: "# Sync-protokoll\n",
    content: "---\nstatus: draft\n---\n# Sync-protokoll\n",
    backlinks: [],
    links: [],
    ...over,
  };
}

function json(body: unknown, status = 200): Response {
  return new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** A fetch that answers from a queue and records the requests. */
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

function client(f: ReturnType<typeof mockFetch>, eventSource?: (url: string) => EventSourceLike) {
  return createHttpClient({
    baseUrl: `${BASE}/`,
    vault: "Anteckningar",
    fetch: f.fn as unknown as typeof fetch,
    eventSource,
  });
}

describe("createHttpClient", () => {
  test("gives vault files a URL an <img> can load", () => {
    const c = client(mockFetch());
    expect(c.fileUrl!("Attachments/Möte 1.png")).toBe(
      `${ROOT}/files/Attachments/M%C3%B6te%201.png?protocol=1`,
    );
    expect(c.fileUrl!("skiss.png", "Projekt/Plan.md")).toBe(
      `${ROOT}/files/skiss.png?protocol=1&from=Projekt%2FPlan.md`,
    );
  });

  test("uploads a pasted image as is", async () => {
    const f = mockFetch(json({ path: "Attachments/Pasted image 1.png" }, 201));
    const blob = new Blob(["PNG"], { type: "image/png" });
    expect(await client(f).attach!("Pasted image 1.png", blob)).toBe(
      "Attachments/Pasted image 1.png",
    );
    const c = f.call(0);
    expect(c.method).toBe("POST");
    expect(c.url.toString()).toBe(`${ROOT}/attachments?name=Pasted+image+1.png`);
    expect(c.headers.get("Content-Type")).toBe("image/png");
    expect(c.headers.get("Nota-Client")).toBe("web");
    expect(f.fn.mock.calls[0]![1]!.body).toBe(blob);
  });

  test("sends the protocol, CSRF and credentials on every request", async () => {
    const f = mockFetch(json({ inbox: "Inbox", folders: [], notes: [], cursor: 3 }));
    const tree = await client(f).tree();
    expect(tree.cursor).toBe(3);
    const c = f.call(0);
    expect(c.url.toString()).toBe(`${ROOT}/tree`);
    expect(c.method).toBe("GET");
    expect(c.credentials).toBe("include");
    expect(c.headers.get("Nota-Protocol")).toBe("1");
    expect(c.headers.get("Nota-Client")).toBe("web");
  });

  test("escapes note paths segment by segment", async () => {
    const f = mockFetch(json(note({ path: "Hem/Möte om jul & nyår.md" })));
    await client(f).get("Hem/Möte om jul & nyår.md");
    expect(f.call(0).url.pathname).toBe(
      "/v1/api/vaults/Anteckningar/notes/Hem/M%C3%B6te%20om%20jul%20%26%20ny%C3%A5r.md",
    );
    expect(escapePath("a/b c/d?.md")).toBe("a/b%20c/d%3F.md");
  });

  test("builds list and search queries", async () => {
    const f = mockFetch(json({ notes: [note()], count: 1 }), json({ notes: [], count: 0 }));
    const c = client(f);
    await c.list({
      folder: "Projekt",
      where: ["status=draft", "prio!=1"],
      sort: "updated desc",
      limit: 20,
    });
    const q = f.call(0).url.searchParams;
    expect(q.get("folder")).toBe("Projekt");
    expect(q.getAll("where")).toEqual(["status=draft", "prio!=1"]);
    expect(q.get("sort")).toBe("updated desc");
    expect(q.get("limit")).toBe("20");
    expect(q.has("tag")).toBe(false);
    await c.search("kimchi", { tag: "recept" });
    const s = f.call(1);
    expect(s.url.pathname).toBe("/v1/api/vaults/Anteckningar/search");
    expect(s.url.searchParams.get("q")).toBe("kimchi");
    expect(s.url.searchParams.get("tag")).toBe("recept");
    // An empty query never reaches the server (it would be a 400).
    expect(await c.search("   ")).toEqual([]);
    expect(f.fn).toHaveBeenCalledTimes(2);
  });

  test("saves the body with Base-Version", async () => {
    const saved = note({ version: 8, body: "# Sync-protokoll\n\nNy text.\n" });
    const f = mockFetch(json(saved));
    const r = await client(f).save(saved.path, saved.body, { version: 7, hash: "abc" });
    expect(r).toEqual({ conflict: false, note: saved });
    const c = f.call(0);
    expect(c.method).toBe("PUT");
    expect(c.headers.get("Base-Version")).toBe("7");
    expect(c.headers.get("Content-Type")).toBe("application/json");
    expect(c.body).toEqual({ body: saved.body });
  });

  test("turns a version mismatch into a conflict with the current note", async () => {
    const current = note({ version: 9, body: "# Sync-protokoll\n\nFrån telefonen.\n" });
    const f = mockFetch(
      json({ code: "version_mismatch", message: "base 7 is not the current version 9" }, 409),
      json(current),
    );
    const r = await client(f).save(current.path, "mine", { version: 7, hash: "abc" });
    expect(r).toEqual({ conflict: true, current });
    expect(f.call(1).method).toBe("GET");
  });

  test("a save without a base writes over the current version", async () => {
    const f = mockFetch(json(note({ version: 9 })), json(note({ version: 10 })));
    const r = await client(f).save("Projekt/Nota/Sync-protokoll.md", "mine");
    expect(r.conflict).toBe(false);
    expect(f.call(1).headers.get("Base-Version")).toBe("9");
  });

  test("patches properties and tags", async () => {
    const f = mockFetch(json(note({ properties: { status: "done" } })));
    const r = await client(f).patch("Projekt/Plan.md", {
      set: { status: "done" },
      tags: { add: ["viktigt"] },
    });
    expect(r.conflict).toBe(false);
    const c = f.call(0);
    expect(c.method).toBe("PATCH");
    expect(c.headers.has("Base-Version")).toBe(false);
    expect(c.body).toEqual({ set: { status: "done" }, tags: { add: ["viktigt"] } });
  });

  test("creates, moves, renames and deletes", async () => {
    const f = mockFetch(
      json(note({ path: "Inbox/Ringa elbolaget.md" }), 201),
      json({
        from: "Inbox/Ringa elbolaget.md",
        path: "Hem/Ringa elbolaget.md",
        version: 12,
        links: { count: 1, notes: [{ path: "Att göra.md", links: 1 }] },
      }),
      json({ from: "Hem/Ringa elbolaget.md", path: "Hem/Elbolaget.md", version: 13 }),
      json({}, 204),
    );
    const c = client(f);
    await c.create({ body: "Ringa elbolaget om fakturan", tags: ["hem"] });
    expect(f.call(0).body).toEqual({ body: "Ringa elbolaget om fakturan", tags: ["hem"] });
    expect(await c.move("Inbox/Ringa elbolaget.md", "Hem")).toEqual({
      from: "Inbox/Ringa elbolaget.md",
      path: "Hem/Ringa elbolaget.md",
      links: { count: 1, notes: [{ path: "Att göra.md", links: 1 }] },
    });
    expect(f.call(1).body).toEqual({ path: "Inbox/Ringa elbolaget.md", folder: "Hem" });
    await c.rename("Hem/Ringa elbolaget.md", "Hem/Elbolaget.md");
    expect(f.call(2).body).toEqual({ path: "Hem/Ringa elbolaget.md", to: "Hem/Elbolaget.md" });
    await c.remove("Hem/Elbolaget.md");
    expect(f.call(3).method).toBe("DELETE");
  });

  test("renames a folder through its own route", async () => {
    const f = mockFetch(json({ from: "Hem", path: "Hus" }));
    const c = client(f);
    // A server from before link updates sends no links.
    expect(await c.renameFolder("Hem", "Hus")).toEqual({
      from: "Hem",
      path: "Hus",
      links: { count: 0, notes: [] },
    });
    expect(f.call(0).url.pathname).toBe("/v1/api/vaults/Anteckningar/folders/rename");
    expect(f.call(0).body).toEqual({ path: "Hem", to: "Hus" });
  });

  test("maps history entries and restores", async () => {
    const f = mockFetch(
      json({
        path: "Plan.md",
        versions: [
          {
            path: "Plan.md",
            version: 4,
            hash: "h",
            size: 3,
            deleted: false,
            kind: "note",
            device_id: "web",
            modified_at: "2026-11-22T10:00:00Z",
          },
        ],
      }),
      json({ path: "Plan.md", version: 5 }),
    );
    const c = client(f);
    expect(await c.history("Plan.md")).toEqual([
      {
        path: "Plan.md",
        version: 4,
        hash: "h",
        size: 3,
        deleted: false,
        device: "web",
        modifiedAt: "2026-11-22T10:00:00Z",
        movedFrom: undefined,
      },
    ]);
    await c.restore("Plan.md", 4);
    expect(f.call(1).url.pathname).toBe("/v1/api/vaults/Anteckningar/history/Plan.md/restore");
    expect(f.call(1).body).toEqual({ version: 4 });
  });

  test("throws NotaError with the API's code", async () => {
    const f = mockFetch(json({ code: "not_found", message: "no note Saknas.md" }, 404));
    const err = await client(f)
      .get("Saknas.md")
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(NotaError);
    expect(err).toMatchObject({ code: "not_found", status: 404, message: "no note Saknas.md" });
  });

  test("subscribes to the SSE change stream", async () => {
    const listeners = new Map<string, (e: { data: string }) => void>();
    const close = vi.fn();
    const urls: string[] = [];
    const es = (url: string): EventSourceLike => {
      urls.push(url);
      return { addEventListener: (t, l) => listeners.set(t, l), close };
    };
    const onChange = vi.fn();
    const off = client(mockFetch(), es).subscribe(onChange);
    expect(urls).toEqual([`${ROOT}/events?protocol=1`]);
    listeners.get("change")!({ data: '{"cursor":13}' });
    expect(onChange).toHaveBeenCalledWith({ paths: [] });
    off();
    expect(close).toHaveBeenCalled();
  });

  test("exposes the open vault only when the web app can switch", () => {
    const openVault = vi.fn();
    const c = createHttpClient({ baseUrl: BASE, vault: "Work", openVault });
    expect(c.serverVault?.name).toBe("Work");
    c.serverVault?.open("Notes");
    expect(openVault).toHaveBeenCalledWith("Notes");
    expect(createHttpClient({ baseUrl: BASE, vault: "Work" }).serverVault).toBeUndefined();
  });
});
