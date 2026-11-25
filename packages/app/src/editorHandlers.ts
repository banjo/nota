import type { NotesClient } from "@nota/client";
import type { PreviewHandlers } from "@nota/editor";
import { toast } from "@nota/ui/sonner";
import type { QueryClient } from "@tanstack/react-query";
import { fail, openLink, openWikilink } from "./actions.ts";
import { attachmentName, relativeHref, resolveLink, splitTitle, vaultPath } from "./lib/notes.ts";
import { keys } from "./queries.ts";
import { useUI } from "./state.ts";

/** Above this, a pasted image saves with a warning (it syncs to every device). */
const LARGE_IMAGE = 10 << 20;

/**
 * What the editor of the note at `path` does with links, images, embeds
 * and pasted images. An embedded note gets its own (its links and images
 * resolve from its folder), without embeds or pasting.
 */
export function noteHandlers(
  client: NotesClient,
  qc: QueryClient,
  path: string,
  opts: { embedded?: boolean } = {},
): PreviewHandlers {
  const fileUrl = client.fileUrl?.bind(client);
  const attach = client.attach?.bind(client);
  return {
    onWikilink: (target, heading) => void openWikilink(client, qc, target, path, heading),
    onTag: (tag) => useUI.getState().navigate({ kind: "list", list: { type: "tag", tag } }),
    onLink: (href) => void openLink(client, qc, href, path),
    ...(fileUrl && {
      resolveAsset: (src, wiki) => {
        if (wiki) return fileUrl(src, path);
        const p = vaultPath(src, path);
        return p ? fileUrl(p.path) : null;
      },
    }),
    ...(!opts.embedded && {
      embedNote: async (target, heading) => {
        const tree = await qc.ensureQueryData({
          queryKey: keys.tree,
          queryFn: () => client.tree(),
        });
        const hit = resolveLink(target, tree.notes, path);
        if (!hit || hit.path === path) return null;
        const note = await qc.ensureQueryData({
          queryKey: keys.note(hit.path),
          queryFn: () => client.get(hit.path),
        });
        return {
          path: note.path,
          title: note.title,
          // A whole note without its title line: the embed's header shows it.
          body: heading ? note.body : splitTitle(note.body).rest,
          handlers: noteHandlers(client, qc, note.path, { embedded: true }),
        };
      },
    }),
    ...(attach &&
      !opts.embedded && {
        onAttach: async (file) => {
          const name = attachmentName(file);
          if (file.size > LARGE_IMAGE)
            toast.warning(`${name} is ${Math.round(file.size / (1 << 20))} MB`, {
              description: "Large images take long to sync to your other devices.",
            });
          try {
            const saved = await attach(name, file);
            void qc.invalidateQueries({ queryKey: keys.tree });
            return `![](${relativeHref(path, saved)})`;
          } catch (e) {
            fail(`Couldn't save ${name}`, e);
            return null;
          }
        },
      }),
  };
}
