import type { AuthSession } from "@nota/client";
import { createContext, useContext } from "react";

/** The signed-in browser session, for the web app. Null on the desktop. */
export const SessionContext = createContext<AuthSession | null>(null);

export function useSession(): AuthSession | null {
  return useContext(SessionContext);
}
