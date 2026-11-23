import type { NotesClient } from "@nota/client";
import { beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { registerBuiltins } from "../src/builtins/index.tsx";
import { createRegistry, type CommandContext } from "../src/registry.ts";
import { useUI } from "../src/state.ts";
import {
  afterDelete,
  deleteConfirmed,
  onlineVaults,
  switchVault,
  useVaultDialog,
  vaultNameProblem,
} from "../src/vaults/online.ts";

vi.mock("@nota/ui/sonner", () => ({
  toast: Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }),
}));

const summary = (name: string) => ({ name, cursor: 0, createdAt: "2026-11-23T08:00:00Z" });

function webClient(name = "Notes") {
  const open = vi.fn();
  const client = {
    kind: "web",
    auth: { vaults: vi.fn() },
    serverVault: { name, open },
  } as unknown as NotesClient;
  return { client, open };
}

describe("vault names", () => {
  test("follow the server's rule", () => {
    expect(vaultNameProblem("Work", [])).toBeNull();
    expect(vaultNameProblem("work.2026_a-b", [])).toBeNull();
    expect(vaultNameProblem("a".repeat(64), [])).toBeNull();
    expect(vaultNameProblem("", [])).toMatch(/name/);
    expect(vaultNameProblem("a".repeat(65), [])).toMatch(/64/);
    expect(vaultNameProblem("Work notes", [])).toMatch(/No spaces/);
    expect(vaultNameProblem("-work", [])).toMatch(/starting with a letter/);
    expect(vaultNameProblem("Anteckningar/ö", [])).toMatch(/letters, digits/);
    expect(vaultNameProblem("ö", [])).toMatch(/letters, digits/);
  });

  test("must not clash with another vault, but may keep their own", () => {
    expect(vaultNameProblem("Work", ["Notes", "Work"])).toMatch(/already a vault called Work/);
    expect(vaultNameProblem("Work", ["Notes", "Work"], "Work")).toBeNull();
    // Case differs: a different name on the server.
    expect(vaultNameProblem("work", ["Work"])).toBeNull();
  });

  test("a delete needs the exact name", () => {
    expect(deleteConfirmed("Work", "Work")).toBe(true);
    expect(deleteConfirmed("work", "Work")).toBe(false);
    expect(deleteConfirmed("Work ", "Work")).toBe(false);
    expect(deleteConfirmed("", "Work")).toBe(false);
  });
});

describe("after a delete", () => {
  const vs = [summary("A"), summary("B"), summary("C")];
  test("another vault stays open", () => {
    expect(afterDelete("A", "B", vs)).toBeUndefined();
  });
  test("deleting the open vault opens the first one left", () => {
    expect(afterDelete("A", "A", vs)).toBe("B");
    expect(afterDelete("B", "B", vs)).toBe("A");
  });
  test("deleting the last vault asks", () => {
    expect(afterDelete("A", "A", [summary("A")])).toBeNull();
  });
});

describe("switching", () => {
  beforeEach(() => useUI.setState({ route: { kind: "note", path: "Inbox/Plan.md" } }));

  test("goes home first, then opens the other vault", () => {
    const { client, open } = webClient("Notes");
    switchVault(client, "Work");
    expect(useUI.getState().route).toEqual({ kind: "home" });
    expect(open).toHaveBeenCalledWith("Work");
  });

  test("to the open vault does nothing", () => {
    const { client, open } = webClient("Notes");
    switchVault(client, "Notes");
    expect(open).not.toHaveBeenCalled();
    expect(useUI.getState().route).toEqual({ kind: "note", path: "Inbox/Plan.md" });
  });

  test("null forgets the choice", () => {
    const { client, open } = webClient("Notes");
    switchVault(client, null);
    expect(open).toHaveBeenCalledWith(null);
  });

  test("only the web client with a session and an open vault manages vaults", () => {
    expect(onlineVaults(webClient().client)).not.toBeNull();
    expect(onlineVaults({ kind: "web" } as unknown as NotesClient)).toBeNull();
    expect(onlineVaults({ kind: "web", auth: {} } as unknown as NotesClient)).toBeNull();
    expect(
      onlineVaults({
        kind: "desktop",
        auth: {},
        serverVault: { name: "x", open: vi.fn() },
      } as unknown as NotesClient),
    ).toBeNull();
  });
});

describe("vault commands on the web", () => {
  beforeEach(() => useVaultDialog.setState({ dialog: null }));
  const ctx = (c: NotesClient) => ({ client: c }) as unknown as CommandContext;

  test("are offered with the online meaning", async () => {
    const r = createRegistry();
    registerBuiltins(r);
    const { client } = webClient("Notes");
    const web = ctx(client);
    for (const id of ["vault.switch", "vault.new", "vault.rename", "vault.delete"]) {
      expect(r.command(id)?.when?.(web)).toBe(true);
    }
    await r.command("vault.switch")!.run(web);
    expect(useVaultDialog.getState().dialog).toEqual({ kind: "switch" });
    await r.command("vault.new")!.run(web);
    expect(useVaultDialog.getState().dialog).toEqual({ kind: "create" });
    await r.command("vault.rename")!.run(web);
    expect(useVaultDialog.getState().dialog).toEqual({ kind: "rename", name: "Notes" });
    await r.command("vault.delete")!.run(web);
    expect(useVaultDialog.getState().dialog).toEqual({ kind: "delete", name: "Notes" });
    // The wizard of the desktop never opens on the web.
    expect(useUI.getState().vaultFlow).toBeNull();
  });

  test("rename and delete are web only; the desktop keeps its wizard", async () => {
    const r = createRegistry();
    registerBuiltins(r);
    const desktop = ctx({
      kind: "desktop",
      shell: { setup: { vaultSource: async () => "config" } },
    } as unknown as NotesClient);
    expect(r.command("vault.rename")?.when?.(desktop)).toBe(false);
    expect(r.command("vault.delete")?.when?.(desktop)).toBe(false);
    await r.command("vault.new")!.run(desktop);
    expect(useUI.getState().vaultFlow).toEqual({ mode: "create" });
    expect(useVaultDialog.getState().dialog).toBeNull();
    useUI.getState().closeVaultFlow();
  });
});
