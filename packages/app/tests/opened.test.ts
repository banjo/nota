import { beforeEach, describe, expect, test, vi } from "vite-plus/test";
import type { NotesClient, Opened } from "@nota/client";
import { noteModeFor } from "../src/lib/noteMode.ts";
import { persistedRoute, useUI } from "../src/state.ts";

const toast = vi.hoisted(() => Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }));
vi.mock("@nota/ui/sonner", () => ({ toast }));

const { showOpened, takeOpened } = await import("../src/components/OpenedFiles.tsx");

beforeEach(() => {
  toast.error.mockClear();
  useUI.setState({ route: { kind: "home" }, noteMode: "live", openChecked: false, expanded: {} });
});

describe("a vault note opened from the file manager", () => {
  test("reads first, for that open only", () => {
    expect(noteModeFor({ kind: "note", via: "open" }, "live")).toBe("reading");
    expect(noteModeFor({ kind: "note", via: "open" }, "source")).toBe("reading");
    expect(noteModeFor({ kind: "note" }, "live")).toBe("live");
    expect(noteModeFor({ kind: "home" }, "source")).toBe("source");
  });

  test("opens in Reading with its folders expanded, the stored mode untouched", () => {
    showOpened([
      { id: "", kind: "note", notePath: "Projects/Q4/Plan.md", name: "Plan.md", dir: "~" },
    ]);
    const s = useUI.getState();
    expect(s.route).toEqual({ kind: "note", path: "Projects/Q4/Plan.md", via: "open" });
    expect(s.expanded["folder:Projects"]).toBe(true);
    expect(s.expanded["folder:Projects/Q4"]).toBe(true);
    expect(s.noteMode).toBe("live");
    expect(noteModeFor(s.route, s.noteMode)).toBe("reading");
  });

  test("⌘E from the override goes to Live and ends it; the stored Live stays", () => {
    showOpened([{ id: "", kind: "note", notePath: "A.md", name: "A.md", dir: "~" }]);
    useUI.getState().toggleNoteMode();
    const s = useUI.getState();
    expect(s.route).toEqual({ kind: "note", path: "A.md" });
    expect(s.noteMode).toBe("live");
    expect(noteModeFor(s.route, s.noteMode)).toBe("live");
  });

  test("an explicit mode ends the override", () => {
    showOpened([{ id: "", kind: "note", notePath: "A.md", name: "A.md", dir: "~" }]);
    useUI.getState().setNoteMode("source");
    expect(useUI.getState().route).toEqual({ kind: "note", path: "A.md" });
    expect(useUI.getState().noteMode).toBe("source");
    useUI.getState().setNoteMode("live");
  });

  test("the override is not kept between runs", () => {
    expect(persistedRoute({ kind: "note", path: "A.md", via: "open" })).toEqual({
      kind: "note",
      path: "A.md",
    });
    expect(persistedRoute({ kind: "inbox" })).toEqual({ kind: "inbox" });
    // A link's anchor is for that one open.
    expect(persistedRoute({ kind: "note", path: "A.md", anchor: "Mål" })).toEqual({
      kind: "note",
      path: "A.md",
    });
  });

  test("the last file wins the page; failures are toasts", () => {
    const files: Opened[] = [
      { id: "", kind: "note", notePath: "A.md", name: "A.md", dir: "~" },
      { id: "", kind: "error", name: "x.txt", dir: "~", error: "Markdown only" },
      { id: "", kind: "note", notePath: "B.md", name: "B.md", dir: "~" },
    ];
    showOpened(files);
    expect(useUI.getState().route).toEqual({ kind: "note", path: "B.md", via: "open" });
    expect(toast.error).toHaveBeenCalledTimes(1);
    expect(toast.error.mock.calls[0]![0]).toContain("x.txt");
  });

  test("taking marks the check done, also without an opener (the web)", async () => {
    await takeOpened({ kind: "web" } as NotesClient);
    expect(useUI.getState().openChecked).toBe(true);
    useUI.setState({ openChecked: false });
    const take = vi.fn(async () => [] as Opened[]);
    await takeOpened({ kind: "desktop", opener: { take } } as unknown as NotesClient);
    expect(take).toHaveBeenCalled();
    expect(useUI.getState().openChecked).toBe(true);
    expect(useUI.getState().route).toEqual({ kind: "home" });
  });
});
