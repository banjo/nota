import { type AuthApi, createHttpAuth, createHttpClient, type NotesClient } from "@nota/client";
import {
  AuthGate,
  ConnectPage,
  type ConnectRequest,
  DevicesPage,
  NotaApp,
  useSession,
} from "@nota/app";
import { Button } from "@nota/ui/button";
import { toast, Toaster } from "@nota/ui/sonner";
import {
  useMutation,
  useQuery,
  useQueryClient,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { CloudOff, LoaderCircle, Plus, RefreshCw } from "lucide-react";
import { type ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import { connection } from "./connection.ts";
import { href, navigate, usePath } from "./router.ts";

/** The API is its own origin; override with VITE_NOTA_API for a local server. */
export const API =
  (import.meta.env.VITE_NOTA_API as string | undefined) ?? "https://nota.app.banjoanton.com";

/** One account API for the whole session; the gate re-checks it on 401s. */
export const auth: AuthApi = createHttpAuth({
  baseUrl: API,
  onExpired: () => auth.invalidate(),
  onStatus: (up) => connection.set(up),
});

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { staleTime: 30_000, refetchOnWindowFocus: false, retry: 1 },
    },
  });
}

/** Top-and-bottom for the pages that only need an account, not a vault. */
function AuthPage({ children, title }: { children: ReactNode; title: string }) {
  return (
    <>
      <Toaster />
      <AuthGate auth={auth} title={title}>
        {children}
      </AuthGate>
    </>
  );
}

/** The connect URL: desktop sends a PKCE challenge, the CLI a user code. */
function parseConnect(search: string): ConnectRequest | null {
  const q = new URLSearchParams(search);
  const challenge = q.get("challenge");
  if (challenge) {
    return {
      kind: "desktop",
      challenge,
      device: q.get("device") ?? "Nota",
      redirect: q.get("redirect") || "nota://auth",
      state: q.get("state") ?? undefined,
    };
  }
  const code = q.get("code") ?? q.get("user_code");
  if (code) return { kind: "cli", code: code.trim().toUpperCase() };
  return null;
}

function ConnectRoute() {
  const request = useMemo(() => parseConnect(window.location.search), []);
  return <ConnectPage auth={auth} request={request} />;
}

function DevicesRoute() {
  return <DevicesPage auth={auth} onBack={() => navigate("/")} />;
}

/** A banner for a server that went away mid-session. */
function ConnectionBanner() {
  const [up, setUp] = useState(connection.get());
  useEffect(() => connection.subscribe(setUp), []);
  if (up) return null;
  return (
    <div
      role="status"
      className="fixed bottom-4 left-1/2 z-50 flex -translate-x-1/2 items-center gap-2 rounded-lg bg-surface px-3 py-2 text-ui text-foreground shadow-pop"
    >
      <CloudOff className="size-4 text-danger" />
      <span>Can't reach the server — changes aren't being saved.</span>
      <Button size="sm" variant="secondary" onClick={() => window.location.reload()}>
        <RefreshCw /> Retry
      </Button>
    </div>
  );
}

/** Picks (or creates) the server vault this browser opens. */
function VaultGate() {
  const session = useSession();
  const qc = useQueryClient();
  const key = `nota:web:vault:${session?.user.email ?? ""}`;
  const [vault, setVault] = useState<string | null>(() => localStorage.getItem(key));
  const [name, setName] = useState("Notes");

  const vaults = useQuery({
    queryKey: ["vaults", session?.user.email],
    queryFn: () => auth.vaults(),
  });
  const use = useCallback(
    (v: string) => {
      localStorage.setItem(key, v);
      qc.clear();
      setVault(v);
    },
    [key, qc],
  );
  const create = useMutation({
    mutationFn: (n: string) => auth.createVault(n),
    onSuccess: (v) => {
      toast.success(`Created ${v.name}`);
      use(v.name);
    },
    onError: (e) => toast.error("Couldn't create the vault", { description: String(e) }),
  });

  // A stored vault wins; with a single vault, open it. Several: ask once.
  useEffect(() => {
    if (vault || !vaults.data) return;
    if (vaults.data.length === 1) use(vaults.data[0]!.name);
  }, [vault, vaults.data, use]);

  // A vault that is gone from the account (deleted elsewhere) asks again.
  useEffect(() => {
    if (!vault || !vaults.data) return;
    if (vaults.data.some((v) => v.name === vault)) return;
    localStorage.removeItem(key);
    setVault(null);
  }, [vault, vaults.data, key]);

  if (vault) return <Notes vault={vault} />;

  return (
    <div className="flex min-h-full flex-col items-center justify-center bg-background px-5 py-12">
      <div className="w-full max-w-[380px] rounded-xl bg-surface p-7 shadow-card">
        <h1 className="text-[20px] font-semibold tracking-[-0.015em] text-foreground">
          {vaults.isPending ? "Opening your vault…" : "Choose a vault"}
        </h1>
        {vaults.isPending ? (
          <p className="mt-4 flex items-center gap-2 text-ui text-muted-foreground">
            <LoaderCircle className="size-4 animate-spin motion-reduce:animate-none" /> Asking the
            server which vaults this account has…
          </p>
        ) : vaults.isError ? (
          <>
            <p className="mt-1.5 text-ui text-muted-foreground">
              Couldn't list your vaults:{" "}
              {vaults.error instanceof Error ? vaults.error.message : String(vaults.error)}
            </p>
            <Button className="mt-4 w-full" onClick={() => void vaults.refetch()}>
              <RefreshCw /> Try again
            </Button>
          </>
        ) : vaults.data.length === 0 ? (
          <>
            <p className="mt-1.5 text-ui leading-[1.5] text-muted-foreground">
              Your account has no vault yet. A vault is a folder of Markdown files on the server;
              the desktop app and the CLI can sync the same one.
            </p>
            <form
              className="mt-5 flex gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                if (name.trim()) create.mutate(name.trim());
              }}
            >
              <input
                aria-label="Vault name"
                className="h-9 min-w-0 flex-1 rounded-md bg-surface px-2.5 text-ui text-foreground shadow-card outline-none focus-visible:shadow-[0_0_0_1px_var(--ink),0_0_0_4px_var(--ink-soft)]"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Notes"
              />
              <Button type="submit" disabled={!name.trim() || create.isPending}>
                Create
              </Button>
            </form>
          </>
        ) : (
          <>
            <p className="mt-1.5 text-ui text-muted-foreground">Open one of your vaults.</p>
            <ul className="mt-4 overflow-hidden rounded-lg border border-hairline">
              {vaults.data.map((v) => (
                <li
                  key={v.name}
                  className="flex items-center gap-3 border-t border-divider px-3.5 py-2.5 text-ui first:border-t-0"
                >
                  <span className="min-w-0 flex-1 truncate text-foreground">{v.name}</span>
                  <Button size="sm" variant="secondary" onClick={() => use(v.name)}>
                    Open
                  </Button>
                </li>
              ))}
            </ul>
            <form
              className="mt-4 flex gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                if (name.trim()) create.mutate(name.trim());
              }}
            >
              <input
                aria-label="New vault name"
                className="h-9 min-w-0 flex-1 rounded-md bg-surface px-2.5 text-ui text-foreground shadow-card outline-none focus-visible:shadow-[0_0_0_1px_var(--ink),0_0_0_4px_var(--ink-soft)]"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="New vault name"
              />
              <Button type="submit" variant="outline" disabled={!name.trim() || create.isPending}>
                <Plus /> New vault
              </Button>
            </form>
          </>
        )}
      </div>
      <a
        href={href("/devices")}
        className="mt-5 text-ui text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
      >
        Devices and security
      </a>
    </div>
  );
}

/** The notes UI over the HTTP client, for one vault. */
function Notes({ vault }: { vault: string }) {
  const client: NotesClient = useMemo(
    () =>
      createHttpClient({
        baseUrl: API,
        vault,
        auth,
        onUnauthorized: () => auth.invalidate(),
        onStatus: (up) => connection.set(up),
      }),
    [vault],
  );
  return (
    <>
      <NotaApp client={client} />
      <ConnectionBanner />
    </>
  );
}

function AppRoute({ signup }: { signup: boolean }) {
  const invite = useMemo(
    () => new URLSearchParams(window.location.search).get("invite") ?? undefined,
    [],
  );
  return (
    <AuthGate
      auth={auth}
      initialMode={signup ? "signup" : "signin"}
      invite={invite}
      title={signup ? "Create your account · Nota" : "Nota"}
    >
      <VaultGate />
    </AuthGate>
  );
}

export function Root() {
  const path = usePath();
  const [qc] = useState(makeQueryClient);
  const accountPage = path === "/connect" || path === "/devices";

  return (
    <QueryClientProvider client={qc}>
      {!accountPage ? <Toaster /> : null}
      {path === "/connect" ? (
        <AuthPage title="Approve a sign-in · Nota">
          <ConnectRoute />
        </AuthPage>
      ) : path === "/devices" ? (
        <AuthPage title="Devices · Nota">
          <DevicesRoute />
        </AuthPage>
      ) : (
        <AppRoute signup={path === "/signup"} />
      )}
    </QueryClientProvider>
  );
}
