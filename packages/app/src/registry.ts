import type { Note, NoteInfo, NotesClient, Tree } from "@nota/client";
import type { QueryClient } from "@tanstack/react-query";
import type { LucideIcon } from "lucide-react";
import { type ComponentType, createContext, useContext, useSyncExternalStore } from "react";
import type { Route } from "./state.ts";
import type { ColorTheme } from "./theme/themes.ts";

// The frontend contribution registry (decision 13): commands, sidebar
// sections, tree decorations, note panels, inbox suggestions and colour
// themes. The
// built-in features register through it exactly as UI plugins will, so
// the API is dogfooded before anyone else depends on it.

/** What a command gets when it runs. */
export interface CommandContext {
  client: NotesClient;
  queryClient: QueryClient;
  route: Route;
  /** The note on screen, if any. */
  notePath?: string;
  navigate(route: Route): void;
}

export interface Command {
  id: string;
  title: string;
  /** Palette group: "Notes", "Go to", "View", … */
  group?: string;
  keywords?: string[];
  icon?: LucideIcon;
  /** e.g. "mod+n", "mod+shift+i", "mod+\\" — mod is ⌘ on macOS, Ctrl elsewhere. */
  shortcut?: string;
  /** Hidden from the palette when false. */
  when?: (ctx: CommandContext) => boolean;
  run: (ctx: CommandContext) => void | Promise<void>;
}

export interface SidebarSection {
  id: string;
  /** Lower first. */
  order: number;
  component: ComponentType;
}

/** A row of the tree that decorations may annotate. */
export type TreeItem =
  | { kind: "folder"; path: string; name: string; notes: number }
  | { kind: "note"; path: string; note: NoteInfo };

export interface TreeDecoration {
  /** A small badge (count, label) after the name. */
  badge?: string | number;
  /** A coloured dot before the badge. */
  dot?: string;
  /** Accessible description of the decoration. */
  label?: string;
}

export interface TreeDecorator {
  id: string;
  decorate: (item: TreeItem) => TreeDecoration | null;
}

export interface Panel {
  id: string;
  order: number;
  component: ComponentType<{ note: Note }>;
  when?: (note: Note) => boolean;
}

export interface Suggestion {
  /** Folder to move to ("" is the root). */
  folder: string;
  /** Why, for a tooltip: "Tagged #fotboll". */
  reason: string;
  /** Higher wins. */
  score: number;
}

export interface InboxSuggester {
  id: string;
  suggest: (note: NoteInfo, ctx: { tree: Tree }) => Suggestion[];
}

interface Kinds {
  command: Command;
  sidebar: SidebarSection;
  treeDecorator: TreeDecorator;
  panel: Panel;
  inboxSuggester: InboxSuggester;
  /** A colour theme for Settings › Appearance (theme/themes.ts). */
  theme: ColorTheme;
}
export type ContributionKind = keyof Kinds;

export class Registry {
  private items: { [K in ContributionKind]: Map<string, Kinds[K]> } = {
    command: new Map(),
    sidebar: new Map(),
    treeDecorator: new Map(),
    panel: new Map(),
    inboxSuggester: new Map(),
    theme: new Map(),
  };
  private snapshots = new Map<ContributionKind, unknown[]>();
  private listeners = new Set<() => void>();

  /** Adds a contribution (replacing one with the same id); returns its disposer. */
  register<K extends ContributionKind>(kind: K, item: Kinds[K]): () => void {
    const map = this.items[kind] as Map<string, Kinds[K]>;
    map.set(item.id, item);
    this.changed(kind);
    return () => {
      if (map.get(item.id) === item) {
        map.delete(item.id);
        this.changed(kind);
      }
    };
  }

  /** The contributions of a kind, sorted by order where they have one. Stable between changes. */
  get<K extends ContributionKind>(kind: K): Kinds[K][] {
    let snap = this.snapshots.get(kind) as Kinds[K][] | undefined;
    if (!snap) {
      snap = [...(this.items[kind] as Map<string, Kinds[K]>).values()];
      snap.sort((a, b) => order(a) - order(b));
      this.snapshots.set(kind, snap);
    }
    return snap;
  }

  command(id: string): Command | undefined {
    return this.items.command.get(id);
  }

  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  private changed(kind: ContributionKind) {
    this.snapshots.delete(kind);
    for (const fn of this.listeners) fn();
  }
}

function order(x: unknown): number {
  return typeof x === "object" && x !== null && "order" in x && typeof x.order === "number"
    ? x.order
    : 0;
}

export function createRegistry() {
  return new Registry();
}

export const RegistryContext = createContext<Registry | null>(null);

export function useRegistry(): Registry {
  const r = useContext(RegistryContext);
  if (!r) throw new Error("useRegistry outside <NotaApp>");
  return r;
}

/** The live list of contributions of a kind. */
export function useContributions<K extends ContributionKind>(kind: K): Kinds[K][] {
  const r = useRegistry();
  return useSyncExternalStore(r.subscribe, () => r.get(kind));
}
