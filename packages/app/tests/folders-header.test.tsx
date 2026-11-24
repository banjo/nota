import { ClientContext } from "../src/context.ts";
import type { NotesClient } from "@nota/client";
import { TooltipProvider } from "@nota/ui/tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, test } from "vite-plus/test";
import { registerBuiltins } from "../src/builtins/index.tsx";
import { FoldersHeaderActions } from "../src/components/FoldersHeader.tsx";
import { createRegistry, RegistryContext } from "../src/registry.ts";

describe("Folders header buttons", () => {
  const r = createRegistry();
  registerBuiltins(r);

  test("two labelled icon buttons, always shown (no hover-only class)", () => {
    const out = renderToStaticMarkup(
      <RegistryContext.Provider value={r}>
        <ClientContext.Provider value={{} as NotesClient}>
          <QueryClientProvider client={new QueryClient()}>
            <TooltipProvider>
              <FoldersHeaderActions />
            </TooltipProvider>
          </QueryClientProvider>
        </ClientContext.Provider>
      </RegistryContext.Provider>,
    );
    expect(out).toContain('aria-label="New note"');
    expect(out).toContain('aria-label="New folder"');
    expect(out).not.toMatch(/opacity-0|group-hover/);
  });

  test("they run commands that exist in ⌘K", () => {
    expect(r.command("note.new")).toBeDefined();
    expect(r.command("folder.add")).toBeDefined();
  });
});
