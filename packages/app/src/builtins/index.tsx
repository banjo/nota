import {
  FilePlus2,
  FolderInput,
  Inbox,
  PanelLeft,
  Palette,
  Search,
  Settings,
  SunMoon,
  Trash2,
} from "lucide-react";
import { createNote, deleteNote, newNoteFolder } from "../actions.ts";
import { keys } from "../queries.ts";
import type { Registry } from "../registry.ts";

import { suggestFolders } from "../lib/suggest.ts";
import { themeStore } from "../theme/store.ts";
import { BUILTIN_THEMES } from "../theme/themes.ts";
import { useUI } from "../state.ts";
import { BacklinksPanel } from "./backlinks.tsx";
import { FoldersSection, NavSection, ViewsSection } from "./sections.tsx";

/**
 * Registers Nota's own features through the contribution registry, the
 * same way a UI plugin would. Returns a disposer.
 */
export function registerBuiltins(r: Registry): () => void {
  const off = [
    r.register("sidebar", { id: "nota.nav", order: 0, component: NavSection }),
    r.register("sidebar", { id: "nota.views", order: 10, component: ViewsSection }),
    r.register("sidebar", { id: "nota.folders", order: 20, component: FoldersSection }),

    r.register("panel", { id: "nota.backlinks", order: 100, component: BacklinksPanel }),

    r.register("inboxSuggester", {
      id: "nota.rules",
      suggest: (note, { tree }) => suggestFolders(note, tree),
    }),

    ...BUILTIN_THEMES.map((t) => r.register("theme", t)),

    // No default tree decoration: a status is read in the page, and the
    // tree stays quiet. Plugins get the same hook through the registry.

    r.register("command", {
      id: "note.new",
      title: "New note",
      group: "Notes",
      icon: FilePlus2,
      shortcut: "mod+n",
      run: async (ctx) => {
        const tree = await ctx.queryClient.ensureQueryData({
          queryKey: keys.tree,
          queryFn: () => ctx.client.tree(),
        });
        await createNote(ctx.client, ctx.queryClient, newNoteFolder(ctx.route, tree.inbox));
      },
    }),
    r.register("command", {
      id: "note.move",
      title: "Move note to…",
      group: "Notes",
      keywords: ["folder", "file", "flytta"],
      icon: FolderInput,
      shortcut: "mod+shift+m",
      when: (ctx) => !!ctx.notePath,
      run: (ctx) => useUI.getState().openPalette("move", ctx.notePath),
    }),
    r.register("command", {
      id: "note.delete",
      title: "Move note to trash",
      group: "Notes",
      keywords: ["delete", "remove"],
      icon: Trash2,
      when: (ctx) => !!ctx.notePath,
      run: (ctx) => deleteNote(ctx.client, ctx.queryClient, ctx.notePath!),
    }),
    r.register("command", {
      id: "inbox.open",
      title: "Open inbox",
      group: "Go to",
      keywords: ["triage", "capture", "inkorg"],
      icon: Inbox,
      shortcut: "mod+shift+i",
      run: (ctx) => ctx.navigate({ kind: "inbox" }),
    }),
    r.register("command", {
      id: "palette.toggle",
      title: "Search notes",
      group: "Go to",
      icon: Search,
      shortcut: "mod+k",
      when: () => false, // the palette itself
      run: () => {
        const ui = useUI.getState();
        if (ui.palette.open) ui.closePalette();
        else ui.openPalette();
      },
    }),
    r.register("command", {
      id: "sidebar.toggle",
      title: "Toggle sidebar",
      group: "View",
      icon: PanelLeft,
      shortcut: "mod+\\",
      run: () => useUI.getState().toggleSidebar(),
    }),
    // No shortcut: ⌘⇧L is CodeMirror's select-matches and Safari's
    // sidebar, ⌘⇧D bookmarks every tab in browsers.
    r.register("command", {
      id: "theme.toggleDark",
      title: "Toggle dark mode",
      group: "View",
      keywords: ["light", "appearance", "night", "theme", "mörkt"],
      icon: SunMoon,
      run: () => themeStore().toggleDark(),
    }),
    ...BUILTIN_THEMES.map((t) =>
      r.register("command", {
        id: `theme.palette.${t.id}`,
        title: `Theme: ${t.name}`,
        group: "View",
        keywords: ["colour", "color", "palette", "appearance"],
        icon: Palette,
        run: () => themeStore().setPalette(t.id),
      }),
    ),
    r.register("command", {
      id: "settings.open",
      title: "Settings",
      group: "Go to",
      keywords: ["sync", "account", "sign in", "preferences", "appearance", "theme"],
      icon: Settings,
      shortcut: "mod+,",
      run: (ctx) => ctx.navigate({ kind: "settings" }),
    }),
  ];
  return () => off.forEach((f) => f());
}
