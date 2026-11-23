import type { NotesClient, VaultSummary } from "@nota/client";
import { create } from "zustand";
import { useUI } from "../state.ts";

// The web app's vaults on the server: which one is open, and managing
// the account's others (create, rename, switch, delete). The desktop
// app's vault is a folder and has its own setup wizard (onboarding/).

/** The rule of internal/protocol ValidVaultName. */
const VAULT_NAME = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;

/**
 * Why `name` can't name a vault, or null when it can. `taken` is the
 * account's vault names; `self` is the vault being renamed, which may
 * keep (or change the case of) its own name. The server also refuses
 * the earlier names of renamed vaults; that answer comes back as a 409.
 */
export function vaultNameProblem(name: string, taken: string[], self?: string): string | null {
  if (!name) return "Give the vault a name.";
  if (name.length > 64) return "Use at most 64 characters.";
  if (!VAULT_NAME.test(name)) {
    return /\s/.test(name)
      ? "No spaces: use letters, digits, '.', '_' or '-'."
      : "Use letters, digits, '.', '_' or '-', starting with a letter or digit.";
  }
  if (name !== self && taken.includes(name)) return `There is already a vault called ${name}.`;
  return null;
}

/** The typed confirmation of a delete matches, exactly as the server checks it. */
export function deleteConfirmed(typed: string, name: string): boolean {
  return typed === name;
}

/**
 * Where the web app goes after deleting `deleted`: another vault when
 * the open one went (the first that is left, else null = ask), or
 * undefined when the open vault stays.
 */
export function afterDelete(
  open: string,
  deleted: string,
  vaults: VaultSummary[],
): string | null | undefined {
  if (deleted !== open) return undefined;
  return vaults.find((v) => v.name !== deleted)?.name ?? null;
}

/** The web client's open vault, when it manages server vaults. */
export function onlineVaults(client: NotesClient) {
  return client.kind === "web" && client.auth && client.serverVault
    ? { auth: client.auth, current: client.serverVault }
    : null;
}

/**
 * Opens another vault: the page reloads into it. A note path of this
 * vault means nothing in the next one, so the route goes home first
 * (the UI state is persisted).
 */
export function switchVault(client: NotesClient, name: string | null) {
  const sv = client.serverVault;
  if (!sv || name === sv.name) return;
  useUI.setState({ route: { kind: "home" } });
  sv.open(name);
}

export type VaultDialog =
  | { kind: "switch" }
  | { kind: "create" }
  | { kind: "rename"; name: string }
  | { kind: "delete"; name: string };

/** The vault dialogs over the app (⌘K and the sidebar switcher open them). */
export const useVaultDialog = create<{
  dialog: VaultDialog | null;
  open: (d: VaultDialog) => void;
  close: () => void;
}>()((set) => ({
  dialog: null,
  open: (dialog) => set({ dialog }),
  close: () => set({ dialog: null }),
}));

export const vaultsKey = ["server-vaults"] as const;
