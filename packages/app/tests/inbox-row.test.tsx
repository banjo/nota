import type { NoteInfo, NotesClient } from "@nota/client";
import { TooltipProvider } from "@nota/ui/tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, test, vi } from "vite-plus/test";
import { ClientContext } from "../src/context.ts";
import { InboxItem } from "../src/components/Inbox.tsx";

// useContributions subscribes without a server snapshot, which static markup needs.
vi.mock("../src/registry.ts", async (orig) => ({
  ...(await orig<typeof import("../src/registry.ts")>()),
  useContributions: () => [],
}));

const note = {
  path: "Inbox/Hej.md",
  title: "Hej",
  folder: "Inbox",
  tags: [],
  created: "2026-11-23T22:39:00Z",
} as unknown as NoteInfo;

function render() {
  return renderToStaticMarkup(
    <ClientContext.Provider value={{ kind: "web" } as unknown as NotesClient}>
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <InboxItem note={note} tree={undefined} />
        </TooltipProvider>
      </QueryClientProvider>
    </ClientContext.Provider>,
  );
}

describe("an inbox row", () => {
  const out = render();

  test("is a bounded card: surface, hairline ring, rounded", () => {
    expect(out).toMatch(/class="[^"]*rounded-lg[^"]*bg-surface[^"]*shadow-card/);
  });

  test("is clickable: pointer cursor, hover state, a visible keyboard focus", () => {
    expect(out).toMatch(/class="group\/card cursor-pointer[^"]*hover:bg-hover/);
    expect(out).toContain("has-[[data-open]:focus-visible]");
    // The title is a real button, the keyboard stop.
    expect(out).toMatch(/<button[^>]*data-open[^>]*>/);
  });

  test("the triage buttons are buttons of their own with a pointer cursor", () => {
    expect(out).toContain("Other folder…");
    expect(out).toContain("Keep in inbox");
    for (const m of out.matchAll(
      /<button[^>]*class="([^"]*)"[^>]*>(?:(?!<\/button>).)*(Other folder|Keep in inbox)/gs,
    ))
      expect(m[1]).toContain("cursor-pointer");
  });
});
