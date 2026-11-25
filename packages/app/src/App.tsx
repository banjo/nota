import type { NotesClient } from "@nota/client";
import { Toaster } from "@nota/ui/sonner";
import { TooltipProvider } from "@nota/ui/tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useQuery } from "@tanstack/react-query";
import { type ReactNode, useEffect, useState } from "react";
import { registerBuiltins } from "./builtins/index.tsx";
import { Shell } from "./components/Shell.tsx";
import { Onboarding } from "./components/lazy.tsx";
import { ClientContext } from "./context.ts";
import { isMac } from "./lib/shortcuts.ts";
import { useChangeSubscription } from "./queries.ts";
import { useNarrow, useViewport } from "./lib/viewport.ts";
import { useUI } from "./state.ts";
import { createRegistry, type Registry, RegistryContext, useContributions } from "./registry.ts";
import { settingsSync } from "./settings/index.ts";
import { usePluginThemes } from "./theme/register.ts";
import { useTheme, useThemeChrome, useThemeTokens } from "./theme/useTheme.ts";

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

/** Brings in settings.toml and the account's settings, and follows them. */
function SettingsLayers({ client }: { client: NotesClient }) {
  useEffect(() => settingsSync.connect(client.settings), [client.settings]);
  return null;
}

/** Toasts in the mode in effect, and the chrome around the page with it. */
function Themed({ client }: { client: NotesClient }) {
  const theme = useTheme();
  const narrow = useNarrow();
  const navPane = useUI((s) => s.pane === "nav");
  useViewport();
  const plugins = usePluginThemes();
  const themes = useContributions("theme");
  useThemeTokens(theme, [...themes, ...plugins.themes], plugins.pending);
  // A phone's status bar sits over whichever pane is on screen.
  useThemeChrome(theme, client.shell, narrow && navPane ? "--sidebar" : "--background");
  // On a phone toasts drop in from the top, clear of the notch, where
  // the home indicator and the keyboard can't cover them.
  return narrow ? (
    <Toaster
      theme={theme.resolved}
      position="top-center"
      mobileOffset={{ top: "calc(var(--safe-top) + 8px)" }}
    />
  ) : (
    <Toaster theme={theme.resolved} />
  );
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
            <SettingsLayers client={client} />
            {children}
            <Themed client={client} />
          </TooltipProvider>
        </RegistryContext.Provider>
      </ClientContext.Provider>
    </QueryClientProvider>
  );
}

/**
 * The first-run setup comes before anything else: a desktop app that
 * started without a vault shows the wizard, and the vault's own screens
 * (which would ask Go for a vault that isn't there) are not mounted.
 * The web app has no setup, so it goes straight to the shell.
 */
function Gate({ client }: { client: NotesClient }) {
  const setup = client.shell?.setup;
  const needs = useQuery({
    queryKey: ["setup", "needsOnboarding"],
    queryFn: () => setup!.needsOnboarding(),
    enabled: !!setup,
    staleTime: Infinity,
    retry: false,
  });
  if (!setup) return <Shell />;
  if (needs.isPending) return <div className="h-full bg-background" />;
  return needs.data ? <Onboarding flow="setup" /> : <Shell />;
}

/**
 * The whole Nota UI over a NotesClient. The desktop app passes the MyGo
 * client, the web app the HTTP one; nothing in here knows which.
 */
export function NotaApp({ client, registry }: { client: NotesClient; registry?: Registry }) {
  return (
    <NotaProviders client={client} registry={registry}>
      <Gate client={client} />
    </NotaProviders>
  );
}
