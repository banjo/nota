import type { HistoryEntry, NotesClient } from "@nota/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, test, vi } from "vite-plus/test";
import { ClientContext } from "../src/context.ts";

const toast = vi.hoisted(() => Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }));
vi.mock("@nota/ui/sonner", () => ({ toast }));

const { initialVersion, VersionPane, visibleVersions } =
  await import("../src/components/History.tsx");

const entry = (over: Partial<HistoryEntry>): HistoryEntry => ({
  path: "Plan.md",
  version: 1,
  hash: "h1",
  size: 10,
  deleted: false,
  device: "laptop",
  modifiedAt: "2026-11-20T10:00:00Z",
  ...over,
});

describe("the version list", () => {
  test("drops the delete a move leaves at the old path", () => {
    const vs = [
      entry({ version: 7, path: "Projekt/Plan.md", movedFrom: "Plan.md", hash: "h3" }),
      entry({ version: 6, deleted: true }),
      entry({ version: 3, deleted: true }),
      entry({ version: 2 }),
    ];
    expect(visibleVersions(vs).map((e) => e.version)).toEqual([7, 3, 2]);
  });

  test("starts on the newest version that differs from the note now", () => {
    const vs = [
      entry({ version: 3, hash: "now" }),
      entry({ version: 2, deleted: true }),
      entry({}),
    ];
    expect(initialVersion(vs, "now")).toBe(1);
    expect(initialVersion([entry({ hash: "now" })], "now")).toBe(1);
    expect(initialVersion([], "now")).toBeNull();
  });
});

function pane(e: HistoryEntry, content: string | null, current: string) {
  const qc = new QueryClient();
  qc.setQueryData(["historyVersion", "Plan.md", e.version], content);
  const client = { kind: "web" } as unknown as NotesClient;
  return renderToStaticMarkup(
    <ClientContext.Provider value={client}>
      <QueryClientProvider client={qc}>
        <VersionPane
          path="Plan.md"
          entry={e}
          current={current}
          isCurrent={false}
          onRestored={() => {}}
        />
      </QueryClientProvider>
    </ClientContext.Provider>,
  );
}

describe("a version", () => {
  test("shows what changed since, line by line, and offers a restore", () => {
    const html = pane(entry({}), "# Plan\nA\n", "# Plan\nB\n");
    expect(html).toContain("since then +1 −1 lines");
    expect(html).toContain("Only in this version: </span>A");
    expect(html).toContain("Added since: </span>B");
    expect(html).toContain("Restore this version");
  });

  test("a delete says so and can't be restored itself", () => {
    const html = pane(entry({ deleted: true }), null, "# Plan\n");
    expect(html).toContain("The note was deleted in this version");
    expect(html).not.toContain("Restore this version");
  });
});
