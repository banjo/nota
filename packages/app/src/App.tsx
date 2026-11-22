import type { NotesClient } from "@nota/client";
import { Toaster } from "@nota/ui/sonner";
import { TooltipProvider } from "@nota/ui/tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { registerBuiltins } from "./builtins/index.tsx";
import { Shell } from "./components/Shell.tsx";
import { ClientContext } from "./context.ts";
import { isMac } from "./lib/shortcuts.ts";
import { useChangeSubscription } from "./queries.ts";
import { createRegistry, type Registry, RegistryContext } from "./registry.ts";
import { useTheme, useThemeChrome } from "./theme/useTheme.ts";

// Apple platforms get their system font; others Inter (see styles.css).
if (typeof document !== "undefined")
  document.documentElement.dataset.platform = isMac() ? "mac" : "other";

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      // Local data: fresh until a change event says otherwise.
      queries: { staleTime: 30_000, refetchOnWindowFocus: false, retry: 1 },
    },
  });
}

function Changes({ client }: { client: NotesClient }) {
  useChangeSubscription(client);
  return null;
}

/** Toasts in the mode in effect, and the chrome around the page with it. */
function Themed() {
  const theme = useTheme();
  useThemeChrome(theme);
  return <Toaster theme={theme.resolved} />;
}

/** Providers shared by the main window and the quick-capture window. */
export function NotaProviders({
  client,
  registry,
  children,
}: {
  client: NotesClient;
  registry?: Registry;
  children: ReactNode;
}) {
  const [qc] = useState(makeQueryClient);
  const [reg] = useState(() => {
    if (registry) return registry;
    const r = createRegistry();
    registerBuiltins(r);
    return r;
  });
  return (
    <QueryClientProvider client={qc}>
      <ClientContext.Provider value={client}>
        <RegistryContext.Provider value={reg}>
          <TooltipProvider>
            <Changes client={client} />
            {children}
            <Themed />
          </TooltipProvider>
        </RegistryContext.Provider>
      </ClientContext.Provider>
    </QueryClientProvider>
  );
}

/**
 * The whole Nota UI over a NotesClient. The desktop app passes the MyGo
 * client, the web app the HTTP one; nothing in here knows which.
 */
export function NotaApp({ client, registry }: { client: NotesClient; registry?: Registry }) {
  return (
    <NotaProviders client={client} registry={registry}>
      <Shell />
    </NotaProviders>
  );
}
