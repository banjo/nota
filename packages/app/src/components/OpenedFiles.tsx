import type { NotesClient, Opened } from "@nota/client";
import { toast } from "@nota/ui/sonner";
import { useEffect } from "react";
import { useClient } from "../context.ts";
import { useUI } from "../state.ts";

/**
 * Shows what the OS opened with the app: the last file opened gets the
 * page, notes of the vault in Reading mode (for this open only), files
 * that can't open a toast each.
 */
export function showOpened(files: Opened[]) {
  const ui = useUI.getState();
  let last: Opened | undefined;
  for (const f of files) {
    if (f.kind === "error") {
      toast.error(`Couldn't open “${f.name}”`, { description: f.error });
      continue;
    }
    last = f;
  }
  if (last?.kind === "note" && last.notePath) {
    ui.navigate({ kind: "note", path: last.notePath, via: "open" });
  }
}

/** Takes the files opened with the app, on load and whenever more arrive. */
export async function takeOpened(client: NotesClient) {
  const o = client.opener;
  try {
    if (o) showOpened(await o.take());
  } catch (e) {
    toast.error("Couldn't open the file", { description: String(e) });
  } finally {
    useUI.setState({ openChecked: true });
  }
}

/** Mounted once in the shell. */
export function OpenedFiles() {
  const client = useClient();
  useEffect(() => {
    // Results are applied even after an unmount (StrictMode's second
    // mount, a reload): take() drained them, so nobody else will.
    void takeOpened(client);
    return client.opener?.onOpened(() => void takeOpened(client));
  }, [client]);
  return null;
}
