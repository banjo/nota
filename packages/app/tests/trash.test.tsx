import { NotaError, type NotesClient, type TrashItem } from "@nota/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { ClientContext } from "../src/context.ts";
import { keys } from "../src/queries.ts";
import { useUI } from "../src/state.ts";

const toast = vi.hoisted(() => Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }));
vi.mock("@nota/ui/sonner", () => ({ toast }));

const { deleteNote, restoreFromTrash } = await import("../src/actions.ts");
const { TrashView } = await import("../src/components/Trash.tsx");

beforeEach(() => {
  vi.clearAllMocks();
  useUI.setState({ route: { kind: "home" } });
});

describe("deleting a note", () => {
  test("offers Undo, which restores it and opens it again", async () => {
    const restoreTrash = vi.fn(async (_id: string) => "Projekt/Plan.md");
    const client = {
      kind: "desktop",
      remove: vi.fn(async () => ".trash/Projekt/Plan.md"),
      restoreTrash,
    } as unknown as NotesClient;
    useUI.setState({ route: { kind: "note", path: "Projekt/Plan.md" } });
    await deleteNote(client, new QueryClient(), "Projekt/Plan.md");
    expect(useUI.getState().route).toEqual({ kind: "home" });
    const [msg, opts] = toast.mock.calls[0]! as [
      string,
      { action: { label: string; onClick: () => void } },
    ];
    expect(msg).toBe("Moved to the trash");
    expect(opts.action.label).toBe("Undo");
    opts.action.onClick();
    await vi.waitFor(() =>
      expect(useUI.getState().route).toEqual({ kind: "note", path: "Projekt/Plan.md" }),
    );
    expect(restoreTrash).toHaveBeenCalledWith(".trash/Projekt/Plan.md");
  });
});

describe("restoring", () => {
  test("says why when a note has taken the path", async () => {
    const client = {
      restoreTrash: vi.fn(async () => {
        throw new NotaError("restore Plan.md: already exists", "exists", 409);
      }),
    } as unknown as NotesClient;
    expect(await restoreFromTrash(client, new QueryClient(), ".trash/Plan.md")).toBeUndefined();
    expect(toast.error).toHaveBeenCalledWith("Couldn't restore the note", {
      description: "A note has taken its place since. Rename or move that note, then try again.",
    });
  });
});

function view(client: Partial<NotesClient>, items: TrashItem[]) {
  const qc = new QueryClient();
  qc.setQueryData(keys.trash, items);
  if (items[0]) qc.setQueryData([...keys.trash, items[0].id], "# Plan\n\nThe text\n");
  return renderToStaticMarkup(
    <ClientContext.Provider value={client as NotesClient}>
      <QueryClientProvider client={qc}>
        <TrashView />
      </QueryClientProvider>
    </ClientContext.Provider>,
  );
}

describe("the trash", () => {
  const item: TrashItem = {
    id: ".trash/Projekt/Plan.md",
    path: "Projekt/Plan.md",
    deletedAt: "2026-11-20T10:00:00Z",
    size: 20,
  };

  test("lists deleted notes with Restore, and on the desktop deleting for good", () => {
    const html = view({ kind: "desktop", purgeTrash: vi.fn(), emptyTrash: vi.fn() }, [item]);
    expect(html).toContain("Plan");
    expect(html).toContain("Projekt");
    expect(html).toContain("from Projekt");
    expect(html).toContain("Restore");
    expect(html).toContain("Delete for good");
    expect(html).toContain("Empty trash");
  });

  test("on the web there is nothing to delete for good", () => {
    const html = view({ kind: "web" }, [{ ...item, id: "Projekt/Plan.md" }]);
    expect(html).toContain("Restore");
    expect(html).not.toContain("Delete for good");
    expect(html).not.toContain("Empty trash");
  });

  test("says when it is empty", () => {
    expect(view({ kind: "desktop", purgeTrash: vi.fn() }, [])).toContain("The trash is empty");
  });
});
