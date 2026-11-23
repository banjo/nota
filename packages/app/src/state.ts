import { create } from "zustand";
import { persist } from "zustand/middleware";
import {
  loadNoteMode,
  type NoteMode,
  saveNoteMode,
  toggledNoteMode,
  watchNoteMode,
} from "./lib/noteMode.ts";

// UI state: what is on screen. Data lives in TanStack Query.

export type ListSpec =
  | { type: "recent" }
  | { type: "tag"; tag: string }
  | { type: "view"; id: string; name: string }
  | { type: "folder"; path: string };

export type Route =
  | { kind: "home" }
  | { kind: "note"; path: string }
  | { kind: "inbox" }
  | { kind: "list"; list: ListSpec }
  | { kind: "settings" };

export type PaletteMode = "all" | "move";

interface UIState {
  route: Route;
  sidebarOpen: boolean;
  /** Narrow screens show one pane at a time: the sidebar ("nav") or the page. */
  pane: "nav" | "main";
  palette: { open: boolean; mode: PaletteMode; path?: string };
  /** Expanded folders, views and sections, by key. */
  expanded: Record<string, boolean>;
  /** How notes are shown, per device (localStorage nota:note-mode), not per note. */
  noteMode: NoteMode;

  setNoteMode: (mode: NoteMode) => void;
  /** Live ↔ Reading (⌘E). */
  toggleNoteMode: () => void;
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

      setNoteMode: (mode) => {
        saveNoteMode(deviceStorage(), mode);
        set({ noteMode: mode });
      },
      toggleNoteMode: () => get().setNoteMode(toggledNoteMode(get().noteMode)),

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
      partialize: (s) => ({ route: s.route, sidebarOpen: s.sidebarOpen, expanded: s.expanded }),
    },
  ),
);

/** The path of the note on screen, if any. */
export const currentPath = (r: Route) => (r.kind === "note" ? r.path : undefined);

// Another window changed the mode: follow it.
watchNoteMode(typeof window === "undefined" ? null : window, deviceStorage(), (noteMode) =>
  useUI.setState({ noteMode }),
);
