import { describe, expect, test, vi } from "vite-plus/test";
import { createHttpClient } from "../src/http.ts";
import { createMygoClient, type MygoBindings } from "../src/mygo.ts";

type OpenerBindings = NonNullable<MygoBindings["Opener"]>;

function bindings(withOpener = true) {
  let fire = () => {};
  const Opener = {
    take: vi.fn(async (): Promise<Awaited<ReturnType<OpenerBindings["take"]>>> => [
      { id: "", kind: "note", notePath: "A.md", name: "A.md", dir: "~/Notes" },
      { id: "x1", kind: "external", name: "R.md", dir: "~/Downloads" },
      { id: "", kind: "weird", name: "b.txt", dir: "~", error: "no" },
    ]),
    previews: vi.fn(async (): Promise<Awaited<ReturnType<OpenerBindings["previews"]>>> => null),
    read: vi.fn(async (id: string) => ({ id }) as Awaited<ReturnType<OpenerBindings["read"]>>),
    save: vi.fn(async (_id: string, _req: Parameters<OpenerBindings["save"]>[1]) => ({
      conflict: false,
      path: "Inbox/R.md",
      trashError: "",
    })),
    overwriteOriginal: vi.fn(async (_id: string, _body: string, _hash: string) => ({
      conflict: true,
    })),
    reveal: vi.fn(async (_id: string) => {}),
    dismiss: vi.fn(async (_id: string) => {}),
    follow: vi.fn(async (_id: string, _href: string) => ({
      id: "x2",
      kind: "external",
      name: "n.md",
      dir: "~",
    })),
    pickFile: vi.fn(async () => {}),
  } satisfies OpenerBindings;
  const b: MygoBindings = {
    Vault: {} as MygoBindings["Vault"],
    Account: {} as MygoBindings["Account"],
    Desktop: {} as MygoBindings["Desktop"],
    ...(withOpener ? { Opener } : {}),
    events: {
      changed: { on: () => () => {} },
      syncStatus: { on: () => () => {} },
      account: { on: () => () => {} },
      command: { on: () => () => {} },
      opened: {
        on: (l: (p: { seq: number }) => void) => {
          fire = () => l({ seq: 1 });
          return () => {};
        },
      },
    },
  };
  return { b, Opener, fire: () => fire() };
}

describe("the desktop client's opener", () => {
  test("maps the Go calls", async () => {
    const { b, Opener, fire } = bindings();
    const o = createMygoClient(b).opener!;
    const q = await o.take();
    expect(q.map((x) => x.kind)).toEqual(["note", "external", "error"]);
    expect(await o.previews()).toEqual([]);

    expect(await o.save("x1", { folder: "", hash: "h" })).toEqual({
      conflict: false,
      path: "Inbox/R.md",
      trashError: undefined,
    });
    expect(Opener.save).toHaveBeenLastCalledWith("x1", {
      folder: "",
      edited: false,
      body: "",
      hash: "h",
      move: false,
    });
    // A draft: edited, with its body; an empty draft is still a draft.
    await o.save("x1", { folder: "/", body: "", hash: "h", move: true });
    expect(Opener.save).toHaveBeenLastCalledWith("x1", {
      folder: "/",
      edited: true,
      body: "",
      hash: "h",
      move: true,
    });
    expect((await o.overwriteOriginal("x1", "b", "h")).conflict).toBe(true);
    await o.pick();
    expect(Opener.pickFile).toHaveBeenCalled();

    const seen = vi.fn();
    o.onOpened(seen);
    fire();
    expect(seen).toHaveBeenCalledTimes(1);
  });

  test("is absent without the Opener service, and on the web", () => {
    expect(createMygoClient(bindings(false).b).opener).toBeUndefined();
    const web = createHttpClient({
      baseUrl: "https://example.com",
      vault: "v",
      fetch: vi.fn() as unknown as typeof fetch,
    });
    expect(web.opener).toBeUndefined();
  });
});
