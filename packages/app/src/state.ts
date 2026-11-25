import { create } from "zustand";
import { persist } from "zustand/middleware";
import {
  loadPropertiesPref,
  type PropertiesPref,
  savePropertiesPref,
  toggledPropertiesPref,
  watchPropertiesPref,
} from "./lib/propertiesPref.ts";
import {
  loadNoteMode,
  type NoteMode,
  noteModeFor,
  saveNoteMode,
  toggledNoteMode,
  watchNoteMode,
} from "./lib/noteMode.ts";
import { settingsSync } from "./settings/index.ts";

// UI state: what is on screen. Data lives in TanStack Query.

export type ListSpec =
  | { type: "recent" }
  | { type: "tag"; tag: string }
  | { type: "view"; id: string; name: string }
  | { type: "folder"; path: string };

export type Route =
  | { kind: "home" }
  /**
   * `via: "open"`: opened from the file manager, shown in Reading for this
   * open. `anchor`: a heading or "^block" to scroll to, from a link.
   */
  | { kind: "note"; path: string; via?: "open"; anchor?: string }
  | { kind: "inbox" }
  /** A Markdown file from outside the vault (desktop), by its opener id. */
  | { kind: "external"; id: string }
  | { kind: "list"; list: ListSpec }
  | { kind: "settings" };

/**
 * "move" files the note at `path` in a folder; "noteIn" picks a folder for a
 * new note; "newFolder" asks for a folder name, relative to the folder `path`;
 * "saveExternal" and "moveExternal" pick the folder for the outside file
 * whose preview id is `path`.
 */
export type PaletteMode = "all" | "move" | "noteIn" | "newFolder" | "saveExternal" | "moveExternal";

interface UIState {
  route: Route;
  sidebarOpen: boolean;
  /** Narrow screens show one pane at a time: the sidebar ("nav") or the page. */
  pane: "nav" | "main";
  palette: { open: boolean; mode: PaletteMode; path?: string };
  /** Expanded folders, views and sections, by key. */
  expanded: Record<string, boolean>;
  /**
   * The vault wizard over the app (Switch vault…, New vault…), or null.
   * Not kept between runs.
   */
  vaultFlow: { mode: "create" | "open" } | null;
  /** How notes are shown, per device (localStorage nota:note-mode), not per note. */
  noteMode: NoteMode;
  /** Whether properties show above a note, per device (localStorage nota:properties). */
  properties: PropertiesPref;
  /**
   * The files the OS opened with the app have been looked at once (or
   * there is no such thing here), so a fresh window may open the most
   * recent note instead. Not kept.
   */
  openChecked: boolean;

  openVaultFlow: (mode: "create" | "open") => void;
  closeVaultFlow: () => void;
  /** Sets the stored mode; it also ends a note's open-in-Reading override. */
  setNoteMode: (mode: NoteMode) => void;
  /** Live ↔ Reading (⌘E), from the mode on screen. */
  toggleNoteMode: () => void;
  setProperties: (pref: PropertiesPref) => void;
  toggleProperties: () => void;
  navigate: (route: Route) => void;
  back: () => void;
  toggleSidebar: () => void;
  openPalette: (mode?: PaletteMode, path?: string) => void;
  closePalette: () => void;
  toggle: (key: string, open?: boolean) => void;
  /** Expands every folder above a note so it shows in the tree. */
  reveal: (path: string) => void;
  /** A note moved: follow it if it is on screen. */
  moved: (from: string, to: string) => void;
}

function deviceStorage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

export const useUI = create<UIState>()(
  persist(
    (set, get) => ({
      route: { kind: "home" },
      sidebarOpen: true,
      pane: "nav",
      palette: { open: false, mode: "all" },
      expanded: { "section:folders": true, "section:views": true },
      noteMode: loadNoteMode(deviceStorage()),
      properties: loadPropertiesPref(deviceStorage()),
      openChecked: false,
      vaultFlow: null,

      openVaultFlow: (mode) => set({ vaultFlow: { mode } }),
      closeVaultFlow: () => set({ vaultFlow: null }),
      setNoteMode: (mode) => {
        // settings.toml sets it: the change lasts this window.
        const keep = !settingsSync.locked("note_mode");
        if (keep) saveNoteMode(deviceStorage(), mode);
        if (keep && mode !== get().noteMode) settingsSync.changed("note_mode", mode);
        const r = get().route;
        set(
          r.kind === "note" && r.via
            ? { noteMode: mode, route: { kind: "note", path: r.path } }
            : { noteMode: mode },
        );
      },
      toggleNoteMode: () =>
        get().setNoteMode(toggledNoteMode(noteModeFor(get().route, get().noteMode))),
      setProperties: (pref) => {
        if (!settingsSync.locked("properties")) {
          savePropertiesPref(deviceStorage(), pref);
          if (pref !== get().properties) settingsSync.changed("properties", pref);
        }
        set({ properties: pref });
      },
      toggleProperties: () => get().setProperties(toggledPropertiesPref(get().properties)),

      navigate: (route) => {
        set({ route, pane: "main" });
        if (route.kind === "note") get().reveal(route.path);
      },
      back: () => set({ pane: "nav" }),
      toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
      openPalette: (mode = "all", path) => set({ palette: { open: true, mode, path } }),
      closePalette: () => set((s) => ({ palette: { ...s.palette, open: false } })),
      toggle: (key, open) =>
        set((s) => ({ expanded: { ...s.expanded, [key]: open ?? !s.expanded[key] } })),
      reveal: (path) =>
        set((s) => {
          const parts = path.split("/").slice(0, -1);
          const next = { ...s.expanded };
          let changed = false;
          for (let i = 1; i <= parts.length; i++) {
            const key = `folder:${parts.slice(0, i).join("/")}`;
            if (!next[key]) {
              next[key] = true;
              changed = true;
            }
          }
          return changed ? { expanded: next } : {};
        }),
      moved: (from, to) => {
        const r = get().route;
        if (r.kind === "note" && r.path === from) get().navigate({ kind: "note", path: to });
      },
    }),
    {
      name: "nota:ui",
      version: 1,
      partialize: (s) => ({
        route: persistedRoute(s.route),
        sidebarOpen: s.sidebarOpen,
        expanded: s.expanded,
      }),
    },
  ),
);

/**
 * The route as kept between runs: an open-in-Reading override is for
 * that one open, and previews of outside files do not come back.
 */
export function persistedRoute(r: Route): Route {
  if (r.kind === "note" && (r.via || r.anchor)) return { kind: "note", path: r.path };
  // Preview ids die with the process.
  if (r.kind === "external") return { kind: "home" };
  return r;
}

/** The mode the note on screen shows in (see noteModeFor). */
export const useNoteMode = () => useUI((s) => noteModeFor(s.route, s.noteMode));

/** The path of the note on screen, if any. */
export const currentPath = (r: Route) => (r.kind === "note" ? r.path : undefined);

// Another window changed the mode: follow it.
watchNoteMode(typeof window === "undefined" ? null : window, deviceStorage(), (noteMode) =>
  useUI.setState({ noteMode }),
);
watchPropertiesPref(typeof window === "undefined" ? null : window, deviceStorage(), (properties) =>
  useUI.setState({ properties }),
);
// The settings layers (settings.toml, the account) changed them.
settingsSync.onReload(() =>
  useUI.setState({
    noteMode: loadNoteMode(deviceStorage()),
    properties: loadPropertiesPref(deviceStorage()),
  }),
);
