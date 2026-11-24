import type { NotesClient, Opened } from "@nota/client";
import { toast } from "@nota/ui/sonner";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { useClient } from "../context.ts";
import { fileChanged, knowPreviews, showPreview } from "../external.ts";
import { useUI } from "../state.ts";

/**
 * Shows what the OS opened with the app: every outside file becomes a
 * preview in the sidebar, the last file opened gets the page (notes of
 * the vault in Reading mode, for this open only), files that can't open
 * get a toast each.
 */
export function showOpened(files: Opened[], client?: NotesClient) {
  const ui = useUI.getState();
  let last: Opened | undefined;
  for (const f of files) {
    if (f.kind === "error") {
      toast.error(`Couldn't open “${f.name}”`, { description: f.error });
      continue;
    }
    if (f.kind === "external" && client) knowPreviews(client, [f]);
    last = f;
  }
  if (last?.kind === "note" && last.notePath) {
    ui.navigate({ kind: "note", path: last.notePath, via: "open" });
  } else if (last?.kind === "external" && client) {
    showPreview(client, last);
  }
}

/** Takes the files opened with the app, on load and whenever more arrive. */
export async function takeOpened(client: NotesClient) {
  const o = client.opener;
  try {
    if (o) showOpened(await o.take(), client);
  } catch (e) {
    toast.error("Couldn't open the file", { description: String(e) });
  } finally {
    useUI.setState({ openChecked: true });
  }
}

/** Mounted once in the shell. */
export function OpenedFiles() {
  const client = useClient();
  const qc = useQueryClient();
  // A previewed file changed on disk: the preview follows it.
  useEffect(() => client.opener?.onChanged((id) => void fileChanged(client, qc, id)), [client, qc]);
  useEffect(() => {
    // Results are applied even after an unmount (StrictMode's second
    // mount, a reload): take() drained them, so nobody else will.
    // Previews already open in Go (the page reloaded, or the main
    // window was closed and made again) come back to the sidebar first.
    void client.opener
      ?.previews()
      .then((list) => knowPreviews(client, list))
      .catch(() => {})
      .finally(() => void takeOpened(client));
    if (!client.opener) void takeOpened(client);
    return client.opener?.onOpened(() => void takeOpened(client));
  }, [client]);
  return null;
}
