import type { NotesClient } from "@nota/client";
import { createContext, useContext } from "react";

export const ClientContext = createContext<NotesClient | null>(null);

/** The NotesClient the app runs on (desktop bindings or the web API). */
export function useClient(): NotesClient {
  const c = useContext(ClientContext);
  if (!c) throw new Error("useClient outside <NotaApp>");
  return c;
}
