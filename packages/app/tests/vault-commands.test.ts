import type { NotesClient } from "@nota/client";
import { beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { openVaultFlow } from "../src/actions.ts";
import { registerBuiltins } from "../src/builtins/index.tsx";
import { createRegistry, type CommandContext } from "../src/registry.ts";
import { useUI } from "../src/state.ts";

const toast = vi.hoisted(() => vi.fn());
vi.mock("@nota/ui/sonner", () => ({
  toast: Object.assign(toast, { error: vi.fn(), success: vi.fn() }),
}));

describe("vault commands in ⌘K", () => {
  beforeEach(() => {
    toast.mockClear();
    useUI.setState({ vaultFlow: null });
  });

  const client = (setup: object | undefined) =>
    ({ kind: "desktop", shell: { setup } }) as unknown as NotesClient;
  const ctx = (c: NotesClient) => ({ client: c }) as unknown as CommandContext;

  test("are offered where there is a setup (desktop), hidden on the web", () => {
    const r = createRegistry();
    registerBuiltins(r);
    const sw = r.command("vault.switch")!;
    const nw = r.command("vault.new")!;
    expect(sw.title).toBe("Switch vault…");
    expect(nw.title).toBe("New vault…");
    const desktop = ctx(client({}));
    const web = ctx({ kind: "web" } as unknown as NotesClient);
    expect(sw.when?.(desktop)).toBe(true);
    expect(nw.when?.(desktop)).toBe(true);
    expect(sw.when?.(web)).toBe(false);
    expect(nw.when?.(web)).toBe(false);
  });

  test("open the wizard in the right mode", async () => {
    const r = createRegistry();
    registerBuiltins(r);
    const c = ctx(client({ vaultSource: async () => "config" }));
    await r.command("vault.switch")!.run(c);
    expect(useUI.getState().vaultFlow).toEqual({ mode: "open" });
    useUI.getState().closeVaultFlow();
    await r.command("vault.new")!.run(c);
    expect(useUI.getState().vaultFlow).toEqual({ mode: "create" });
  });

  test("say why instead when NOTA_VAULT decides", async () => {
    await openVaultFlow(client({ vaultSource: async () => "env" }), "open");
    expect(useUI.getState().vaultFlow).toBeNull();
    expect(toast).toHaveBeenCalledWith(
      "NOTA_VAULT is set, so it decides which vault opens",
      expect.objectContaining({ description: expect.stringContaining("Unset it") }),
    );
  });

  test("still open when the source can't be read", async () => {
    await openVaultFlow(
      client({
        vaultSource: async () => {
          throw new Error("no");
        },
      }),
      "create",
    );
    expect(useUI.getState().vaultFlow).toEqual({ mode: "create" });
  });
});
