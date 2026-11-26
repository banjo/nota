import {
  ArchiveRestore,
  BookOpen,
  Code2,
  FileDown,
  FileInput,
  FileOutput,
  FilePlus2,
  FileText,
  FileWarning,
  FolderInput,
  FolderOpen,
  FolderPlus,
  History,
  Inbox,
  Link2,
  ListTree,
  PanelLeft,
  PenLine,
  Pin,
  PinOff,
  Replace,
  Search,
  Settings,
  SunMoon,
  TextSearch,
  Trash2,
  Undo2,
  X,
} from "lucide-react";
import {
  closePreview,
  confirmOverwrite,
  currentPreview,
  discardDraft,
  draftOf,
  editPreview,
  isDirty,
  revertDraft,
} from "../external.ts";
import { createNote, deleteNote, newNoteFolder, openVaultFlow, setPinned } from "../actions.ts";
import { isPinned } from "../lib/pinned.ts";
import { requestFind } from "../lib/find.ts";
import { baseFolder } from "../lib/folders.ts";
import { focusNoteTitle } from "../components/RowMenu.tsx";
import { openHistory } from "../components/History.tsx";
import { startRename } from "../components/Rename.tsx";
import { keys } from "../queries.ts";
import type { Registry } from "../registry.ts";

import { suggestFolders } from "../lib/suggest.ts";
import { themeStore } from "../theme/store.ts";
import { CLASSIC_THEMES } from "../theme/classics.ts";
import { registerTheme } from "../theme/register.ts";
import { BUILTIN_THEMES } from "../theme/themes.ts";
import { type Route, useUI } from "../state.ts";
import type { Tree } from "@nota/client";
import type { QueryClient } from "@tanstack/react-query";
import { onlineVaults, useVaultDialog } from "../vaults/online.ts";
import { BacklinksPanel } from "./backlinks.tsx";
import { FoldersSection, NavSection, ViewsSection } from "./sections.tsx";
import { OpenedSection } from "../components/OpenedSection.tsx";
import { PinnedSection } from "../components/PinnedSection.tsx";

/** Whether the note on screen is pinned (from the cached tree). */
function notePinned(ctx: { notePath?: string; queryClient: QueryClient }): boolean {
  const n = ctx.queryClient
    .getQueryData<Tree>(keys.tree)
    ?.notes.find((x) => x.path === ctx.notePath);
  return !!n && isPinned(n);
}

/** The folder on screen (a note's, or the list's) that can be renamed, else "". */
function currentFolder(ctx: { route: Route; client: unknown; queryClient: QueryClient }): string {
  const tree = ctx.queryClient.getQueryData<Tree>(keys.tree);
  const f = baseFolder(ctx.route, tree?.inbox ?? "Inbox");
  return f === (tree?.inbox ?? "Inbox") ? "" : f;
}

/**
 * Registers Nota's own features through the contribution registry, the
 * same way a UI plugin would. Returns a disposer.
 */
export function registerBuiltins(r: Registry): () => void {
  const off = [
    r.register("sidebar", { id: "nota.nav", order: 0, component: NavSection }),
    r.register("sidebar", { id: "nota.pinned", order: 3, component: PinnedSection }),
    r.register("sidebar", { id: "nota.opened", order: 5, component: OpenedSection }),
    r.register("sidebar", { id: "nota.views", order: 10, component: ViewsSection }),
    r.register("sidebar", { id: "nota.folders", order: 20, component: FoldersSection }),

    r.register("panel", { id: "nota.backlinks", order: 100, component: BacklinksPanel }),

    r.register("inboxSuggester", {
      id: "nota.rules",
      suggest: (note, { tree }) => suggestFolders(note, tree),
    }),

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
      id: "note.addToFolder",
      title: "Add to folder…",
      group: "Notes",
      keywords: ["move", "folder", "file", "mapp", "flytta"],
      icon: FolderInput,
      shortcut: "mod+shift+m",
      when: (ctx) => !!ctx.notePath,
      run: (ctx) => useUI.getState().openPalette("move", ctx.notePath),
    }),
    r.register("command", {
      id: "note.rename",
      title: "Rename note",
      group: "Notes",
      keywords: ["title", "name", "rename"],
      icon: PenLine,
      when: (ctx) => !!ctx.notePath,
      // The title field is where a note is renamed, in place.
      run: () => focusNoteTitle(),
    }),
    r.register("command", {
      id: "folder.rename",
      title: "Rename folder",
      group: "Notes",
      keywords: ["name", "rename", "folder", "mapp", "byt namn"],
      icon: PenLine,
      // The folder of the note or list on screen, never the inbox itself.
      when: (ctx) => currentFolder(ctx) !== "",
      run: (ctx) => startRename({ kind: "folder", path: currentFolder(ctx) }),
    }),
    r.register("command", {
      id: "note.newInFolder",
      title: "New note in folder…",
      group: "Notes",
      keywords: ["folder", "create", "add", "file", "mapp", "ny"],
      icon: FilePlus2,
      run: () => useUI.getState().openPalette("noteIn"),
    }),
    r.register("command", {
      id: "folder.add",
      title: "Add folder…",
      group: "Notes",
      keywords: ["mkdir", "new", "create", "folder", "mapp", "skapa"],
      icon: FolderPlus,
      run: async (ctx) => {
        const tree = await ctx.queryClient.ensureQueryData({
          queryKey: keys.tree,
          queryFn: () => ctx.client.tree(),
        });
        useUI.getState().openPalette("newFolder", baseFolder(ctx.route, tree.inbox));
      },
    }),
    r.register("command", {
      id: "note.pin",
      title: "Pin note",
      group: "Notes",
      keywords: ["favourite", "favorite", "star", "bookmark", "fäst"],
      icon: Pin,
      shortcut: "mod+shift+p",
      when: (ctx) => !!ctx.notePath && !notePinned(ctx),
      run: (ctx) => setPinned(ctx.client, ctx.queryClient, ctx.notePath!, true),
    }),
    r.register("command", {
      id: "note.unpin",
      title: "Unpin note",
      group: "Notes",
      keywords: ["favourite", "favorite", "star", "bookmark", "unfavourite"],
      icon: PinOff,
      shortcut: "mod+shift+p",
      when: (ctx) => !!ctx.notePath && notePinned(ctx),
      run: (ctx) => setPinned(ctx.client, ctx.queryClient, ctx.notePath!, false),
    }),
    r.register("command", {
      id: "note.find",
      title: "Find in note",
      group: "Notes",
      keywords: ["search", "find", "sök", "hitta"],
      icon: TextSearch,
      shortcut: "mod+f",
      when: (ctx) => ctx.route.kind === "note" || ctx.route.kind === "external",
      run: () => requestFind(false),
    }),
    r.register("command", {
      id: "note.replace",
      title: "Find and replace in note",
      group: "Notes",
      keywords: ["replace", "substitute", "ersätt"],
      icon: Replace,
      shortcut: "mod+alt+f",
      when: (ctx) => ctx.route.kind === "note" || ctx.route.kind === "external",
      run: () => requestFind(true),
    }),
    r.register("command", {
      id: "note.history",
      title: "Show history",
      group: "Notes",
      keywords: ["versions", "restore", "undo", "older", "diff", "historik"],
      icon: History,
      shortcut: "mod+shift+h",
      when: (ctx) => !!ctx.notePath,
      run: (ctx) => openHistory(ctx.notePath!),
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
    // Markdown files from outside the vault (desktop): every banner
    // button is here too, and none has a shortcut.
    r.register("command", {
      id: "file.open",
      title: "Open Markdown file…",
      group: "Files",
      keywords: ["open", "import", "markdown", "file", "finder", "preview"],
      icon: FileText,
      when: (ctx) => !!ctx.client.opener,
      run: (ctx) => ctx.client.opener?.pick(),
    }),
    r.register("command", {
      id: "file.edit",
      title: "Edit file (a draft, nothing is saved)",
      group: "Files",
      keywords: ["edit", "draft", "write", "change"],
      icon: PenLine,
      when: (ctx) => {
        const id = currentPreview();
        return ctx.route.kind === "external" && !!id && draftOf(id) === undefined;
      },
      run: (ctx) => editPreview(ctx.queryClient, currentPreview()!),
    }),
    r.register("command", {
      id: "file.revert",
      title: "Revert draft to the file",
      group: "Files",
      keywords: ["undo", "reset", "draft"],
      icon: Undo2,
      when: (ctx) => {
        const id = currentPreview();
        return ctx.route.kind === "external" && !!id && isDirty(ctx.queryClient, id);
      },
      run: (ctx) => revertDraft(ctx.queryClient, currentPreview()!),
    }),
    r.register("command", {
      id: "file.discard",
      title: "Discard draft",
      group: "Files",
      keywords: ["cancel", "preview", "draft", "stop editing"],
      icon: ArchiveRestore,
      when: (ctx) => {
        const id = currentPreview();
        return ctx.route.kind === "external" && !!id && draftOf(id) !== undefined;
      },
      run: (ctx) => discardDraft(ctx.queryClient, currentPreview()!),
    }),
    r.register("command", {
      id: "file.saveToVault",
      title: "Save file to vault…",
      group: "Files",
      keywords: ["import", "copy", "save", "vault", "keep"],
      icon: FileDown,
      when: (ctx) => ctx.route.kind === "external",
      run: () => useUI.getState().openPalette("saveExternal", currentPreview()),
    }),
    r.register("command", {
      id: "file.moveToVault",
      title: "Move file into vault… (original to the Trash)",
      group: "Files",
      keywords: ["import", "move", "vault", "trash"],
      icon: FileInput,
      when: (ctx) => ctx.route.kind === "external",
      run: () => useUI.getState().openPalette("moveExternal", currentPreview()),
    }),
    r.register("command", {
      id: "file.overwriteOriginal",
      title: "Overwrite original file with the draft…",
      group: "Files",
      keywords: ["write", "save in place", "original", "overwrite"],
      icon: FileWarning,
      when: (ctx) => {
        const id = currentPreview();
        return ctx.route.kind === "external" && !!id && draftOf(id) !== undefined;
      },
      run: (ctx) => confirmOverwrite(ctx.client, ctx.queryClient, currentPreview()!),
    }),
    r.register("command", {
      id: "file.reveal",
      title: "Reveal original file",
      group: "Files",
      keywords: ["finder", "show", "folder", "explorer"],
      icon: FileOutput,
      when: (ctx) => ctx.route.kind === "external",
      run: (ctx) => ctx.client.opener?.reveal(currentPreview()!),
    }),
    r.register("command", {
      id: "file.dismiss",
      title: "Close preview",
      group: "Files",
      keywords: ["close", "dismiss", "preview"],
      icon: X,
      when: (ctx) => ctx.route.kind === "external",
      run: (ctx) => closePreview(ctx.client, ctx.queryClient, currentPreview()!),
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
      id: "trash.open",
      title: "Open trash",
      group: "Go to",
      keywords: ["deleted", "restore", "undelete", "bin", "papperskorg"],
      icon: Trash2,
      run: (ctx) => ctx.navigate({ kind: "trash" }),
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
    // Reading and source modes: ⌘E is the only shortcut, the rest is ⌘K.
    r.register("command", {
      id: "view.mode.toggle",
      title: "Toggle reading mode",
      group: "View",
      keywords: ["read", "edit", "preview", "live", "view"],
      icon: BookOpen,
      shortcut: "mod+e",
      when: (ctx) => !!ctx.notePath,
      run: () => useUI.getState().toggleNoteMode(),
    }),
    r.register("command", {
      id: "view.mode.live",
      title: "View: Live mode",
      group: "View",
      keywords: ["edit", "write", "preview"],
      icon: PenLine,
      when: (ctx) => !!ctx.notePath,
      run: () => useUI.getState().setNoteMode("live"),
    }),
    r.register("command", {
      id: "view.mode.reading",
      title: "View: Reading mode",
      group: "View",
      keywords: ["read", "read-only", "finished"],
      icon: BookOpen,
      when: (ctx) => !!ctx.notePath,
      run: () => useUI.getState().setNoteMode("reading"),
    }),
    r.register("command", {
      id: "view.mode.source",
      title: "View: Source mode",
      group: "View",
      keywords: ["markdown", "raw", "plain", "code"],
      icon: Code2,
      when: (ctx) => !!ctx.notePath,
      run: () => useUI.getState().setNoteMode("source"),
    }),
    r.register("command", {
      id: "view.properties.toggle",
      title: "Toggle properties",
      group: "View",
      keywords: ["frontmatter", "metadata", "tags", "status", "hide", "show"],
      icon: ListTree,
      run: () => useUI.getState().toggleProperties(),
    }),
    r.register("command", {
      id: "view.backlinks.toggle",
      title: "Toggle linked from",
      group: "View",
      keywords: ["backlinks", "links", "references", "linked", "hide", "show"],
      icon: Link2,
      run: () => useUI.getState().toggleBacklinks(),
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
    // Each theme, with its "Theme: …" command.
    ...[...BUILTIN_THEMES, ...CLASSIC_THEMES].map((t) => registerTheme(r, t)),
    // On the desktop a vault is a folder (the setup wizard, then a
    // relaunch); on the web it is one of the account's server vaults.
    r.register("command", {
      id: "vault.switch",
      title: "Switch vault…",
      group: "Vault",
      keywords: ["open", "folder", "change", "vault", "notes folder"],
      icon: FolderOpen,
      when: (ctx) => !!ctx.client.shell?.setup || !!onlineVaults(ctx.client),
      run: (ctx) =>
        onlineVaults(ctx.client)
          ? useVaultDialog.getState().open({ kind: "switch" })
          : openVaultFlow(ctx.client, "open"),
    }),
    r.register("command", {
      id: "vault.new",
      title: "New vault…",
      group: "Vault",
      keywords: ["create", "add", "folder", "vault", "notes folder"],
      icon: FolderPlus,
      when: (ctx) => !!ctx.client.shell?.setup || !!onlineVaults(ctx.client),
      run: (ctx) =>
        onlineVaults(ctx.client)
          ? useVaultDialog.getState().open({ kind: "create" })
          : openVaultFlow(ctx.client, "create"),
    }),
    r.register("command", {
      id: "vault.rename",
      title: "Rename vault…",
      group: "Vault",
      keywords: ["name", "vault", "title"],
      icon: PenLine,
      when: (ctx) => !!onlineVaults(ctx.client),
      run: (ctx) =>
        useVaultDialog
          .getState()
          .open({ kind: "rename", name: onlineVaults(ctx.client)!.current.name }),
    }),
    r.register("command", {
      id: "vault.delete",
      title: "Delete vault…",
      group: "Vault",
      keywords: ["remove", "vault", "trash"],
      icon: Trash2,
      when: (ctx) => !!onlineVaults(ctx.client),
      run: (ctx) =>
        useVaultDialog
          .getState()
          .open({ kind: "delete", name: onlineVaults(ctx.client)!.current.name }),
    }),
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
