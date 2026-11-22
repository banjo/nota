import { type AuthApi, type AuthConfig, type AuthSession, NotaError } from "@nota/client";
import { Button } from "@nota/ui/button";
import { CloudOff, LoaderCircle, RefreshCw } from "lucide-react";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { AuthShell } from "./chrome.tsx";
import { SessionContext } from "./session.ts";
import { type AuthMode, SignInFlow, suggestPasskeyName } from "./SignIn.tsx";

type Status = "checking" | "offline" | "signed-out" | "needs-passkey" | "ready";

/** The account screen for a recovery-code session: a new passkey first. */
function AddPasskey({ auth, onAdded }: { auth: AuthApi; onAdded: () => void }) {
  const [name, setName] = useState(suggestPasskeyName());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const add = async () => {
    setBusy(true);
    setError(null);
    try {
      await auth.addPasskey(name.trim() || "This device");
      onAdded();
    } catch (e) {
      setError(
        e instanceof NotaError
          ? e.message
          : e instanceof DOMException && e.name === "NotAllowedError"
            ? "The passkey prompt was closed. Try again."
            : String(e),
      );
    } finally {
      setBusy(false);
    }
  };
  return (
    <AuthShell
      title="Add a passkey"
      description="You signed in with a recovery code. Add a passkey so you can sign in with your fingerprint, face or device PIN next time."
    >
      <div className="flex flex-col gap-3">
        <label className="block">
          <span className="mb-1.5 block text-small font-medium text-muted-foreground">
            Name this passkey
          </span>
          <input
            className="h-9 w-full rounded-md bg-surface px-2.5 text-ui text-foreground shadow-card outline-none focus-visible:shadow-[0_0_0_1px_var(--ink),0_0_0_4px_var(--ink-soft)]"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="MacBook Air"
          />
        </label>
        {error ? (
          <p role="alert" className="text-ui text-danger">
            {error}
          </p>
        ) : null}
        <Button size="lg" className="w-full" disabled={busy} onClick={() => void add()}>
          {busy ? <LoaderCircle className="animate-spin motion-reduce:animate-none" /> : null}
          Create passkey
        </Button>
        <button
          type="button"
          className="text-ui text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
          onClick={() => void auth.logout()}
        >
          Sign out instead
        </button>
      </div>
    </AuthShell>
  );
}

function Checking() {
  return (
    <div className="grid min-h-full place-items-center bg-background">
      <p className="flex items-center gap-2 text-ui text-faint">
        <LoaderCircle className="size-4 animate-spin motion-reduce:animate-none" />
        Connecting to Nota…
      </p>
    </div>
  );
}

function Offline({ error, onRetry }: { error: string; onRetry: () => void }) {
  return (
    <AuthShell
      title="Can't reach the server"
      description={
        <>
          The web app reads and writes your notes on <b>nota.app.banjoanton.com</b>, and it did not
          answer. Your notes are safe: the desktop app and the CLI keep working offline.
        </>
      }
    >
      <p className="rounded-md bg-code px-2.5 py-2 font-mono text-[12px] break-words text-text-soft">
        {error}
      </p>
      <Button className="mt-4 w-full" onClick={onRetry}>
        <RefreshCw /> Try again
      </Button>
      <p className="mt-3 flex items-start gap-1.5 text-small text-faint">
        <CloudOff className="mt-0.5 size-3.5 shrink-0" />
        Check your connection, or whether the server is up.
      </p>
    </AuthShell>
  );
}

/**
 * The web app's front door: checks the session cookie and keeps the app
 * behind sign-in. A 401 anywhere brings the gate back, and every sign-in
 * path (passkey, recovery code, new account) lives here.
 */
export function AuthGate({
  auth,
  children,
  initialMode = "signin",
  invite,
  title,
}: {
  /** The web app's account API (createHttpAuth). */
  auth: AuthApi;
  children: ReactNode;
  initialMode?: AuthMode;
  invite?: string;
  title?: string;
}) {
  const [status, setStatus] = useState<Status>("checking");
  const [session, setSession] = useState<AuthSession | null>(null);
  const [error, setError] = useState("");
  const [config, setConfig] = useState<AuthConfig>();
  const alive = useRef(true);

  const load = useCallback(async () => {
    try {
      const s = await auth.session();
      if (!alive.current) return;
      setSession(s);
      setError("");
      setStatus(s ? (s.needsPasskey ? "needs-passkey" : "ready") : "signed-out");
    } catch (e) {
      if (!alive.current) return;
      if (e instanceof NotaError && e.code === "unreachable") {
        setError(e.message);
        setStatus("offline");
      } else {
        setError(e instanceof Error ? e.message : String(e));
        setStatus("offline");
      }
    }
  }, [auth]);

  useEffect(() => {
    alive.current = true;
    void load();
    const off = auth.subscribe(() => void load());
    return () => {
      alive.current = false;
      off();
    };
  }, [auth, load]);

  useEffect(() => {
    void auth
      .config()
      .then(setConfig)
      .catch(() => setConfig(undefined));
  }, [auth]);

  useEffect(() => {
    if (title) document.title = title;
  }, [title]);

  if (status === "checking") return <Checking />;
  if (status === "offline") return <Offline error={error} onRetry={() => void load()} />;
  if (status === "needs-passkey") return <AddPasskey auth={auth} onAdded={() => void load()} />;
  if (status === "signed-out") {
    return (
      <SignInFlow
        auth={auth}
        config={config}
        initialMode={initialMode}
        invite={invite}
        onSignedIn={(s) => {
          setSession(s);
          setStatus(s.needsPasskey ? "needs-passkey" : "ready");
        }}
        onRegistered={() => void load()}
      />
    );
  }
  return <SessionContext.Provider value={session}>{children}</SessionContext.Provider>;
}
