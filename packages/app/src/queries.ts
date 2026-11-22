import type { Base, ListQuery, Note, NotesClient } from "@nota/client";
import {
  keepPreviousData,
  type QueryClient,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useEffect } from "react";
import { useClient } from "./context.ts";

// Every read goes through TanStack Query; change events from the client
// (file watcher, sync, SSE) invalidate what they touch.

export const keys = {
  info: ["info"] as const,
  tree: ["tree"] as const,
  inbox: ["inbox"] as const,
  tags: ["tags"] as const,
  views: ["views"] as const,
  note: (path: string) => ["note", path] as const,
  list: (q: ListQuery) => ["list", q] as const,
  search: (q: string) => ["search", q] as const,
  sync: ["sync"] as const,
  account: ["account"] as const,
};

export const useVaultInfo = () => {
  const c = useClient();
  return useQuery({ queryKey: keys.info, queryFn: () => c.info(), staleTime: Infinity });
};
export const useTree = () => {
  const c = useClient();
  return useQuery({ queryKey: keys.tree, queryFn: () => c.tree() });
};
export const useInbox = () => {
  const c = useClient();
  return useQuery({ queryKey: keys.inbox, queryFn: () => c.inbox() });
};
export const useTags = () => {
  const c = useClient();
  return useQuery({ queryKey: keys.tags, queryFn: () => c.tags() });
};
export const useViews = () => {
  const c = useClient();
  return useQuery({ queryKey: keys.views, queryFn: () => c.views() });
};
export const useList = (q: ListQuery, enabled = true) => {
  const c = useClient();
  return useQuery({ queryKey: keys.list(q), queryFn: () => c.list(q), enabled });
};
export const useNote = (path: string | undefined) => {
  const c = useClient();
  return useQuery({
    queryKey: keys.note(path ?? ""),
    queryFn: () => c.get(path!),
    enabled: !!path,
    retry: false,
  });
};
export const useSearch = (q: string) => {
  const c = useClient();
  return useQuery({
    queryKey: keys.search(q),
    queryFn: () => c.search(q, { limit: 20 }),
    enabled: q.trim().length > 0,
    placeholderData: keepPreviousData,
  });
};

/** The base each note's next write is checked against, kept current by every write. */
const bases = new Map<string, Base>();
export const noteBase = (path: string): Base | undefined => bases.get(path);
export const setNoteBase = (note: Pick<Note, "path" | "version" | "hash">) =>
  bases.set(note.path, { version: note.version, hash: note.hash });

/** Puts a written note into the cache and records its base. */
export function noteWritten(qc: QueryClient, note: Note) {
  setNoteBase(note);
  qc.setQueryData(keys.note(note.path), note);
}

/** Refetches what changes touch. Paths unknown (web SSE): everything. */
export function useChangeSubscription(client: NotesClient) {
  const qc = useQueryClient();
  useEffect(
    () =>
      client.subscribe(({ paths }) => {
        for (const k of [keys.tree, keys.inbox, keys.tags, keys.views, ["list"], ["search"]])
          void qc.invalidateQueries({ queryKey: k });
        if (paths.length === 0) void qc.invalidateQueries({ queryKey: ["note"] });
        else for (const p of paths) void qc.invalidateQueries({ queryKey: keys.note(p) });
        // Backlinks of other notes may have changed too.
        void qc.invalidateQueries({ queryKey: ["note"], refetchType: "none" });
      }),
    [client, qc],
  );
}
