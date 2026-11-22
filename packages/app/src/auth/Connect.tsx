import { type AuthApi, NotaError } from "@nota/client";
import { Button } from "@nota/ui/button";
import { toast } from "@nota/ui/sonner";
import { useQuery } from "@tanstack/react-query";
import {
  Ban,
  Check,
  ExternalLink,
  Laptop,
  LoaderCircle,
  ShieldCheck,
  Terminal,
} from "lucide-react";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { fail } from "../actions.ts";
import { AuthShell } from "./chrome.tsx";

/** What /app/connect was asked to approve: a desktop app or a CLI. */
export type ConnectRequest =
  | { kind: "desktop"; challenge: string; device: string; redirect: string; state?: string }
  | { kind: "cli"; code: string };

function details(rows: { label: string; value: ReactNode }[]) {
  return (
    <dl className="flex flex-col gap-2 rounded-lg bg-code/60 px-3.5 py-3 text-ui">
      {rows.map((r) => (
        <div key={r.label} className="flex items-baseline gap-3">
          <dt className="w-24 shrink-0 text-small text-faint">{r.label}</dt>
          <dd className="min-w-0 flex-1 truncate text-text-soft">{r.value}</dd>
        </div>
      ))}
    </dl>
  );
}

function Busy({ children }: { children: ReactNode }) {
  return (
    <p className="flex items-center gap-2 text-ui text-muted-foreground">
      <LoaderCircle className="size-4 animate-spin motion-reduce:animate-none" /> {children}
    </p>
  );
}

/** Desktop: PKCE challenge in the URL; approval answers with nota://auth. */
function DesktopApproval({
  auth,
  request,
}: {
  auth: AuthApi;
  request: Extract<ConnectRequest, { kind: "desktop" }>;
}) {
  const [busy, setBusy] = useState(false);
  const [redirect, setRedirect] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Handing off happens from the click, which browsers allow; the link
  // under it is the fallback when the app is not installed.
  useEffect(() => {
    if (!redirect) return;
    const t = setTimeout(() => {
      window.location.href = redirect;
    }, 250);
    return () => clearTimeout(t);
  }, [redirect]);

  const approve = async () => {
    setBusy(true);
    setError(null);
    try {
      const out = await auth.approveDesktop({
        challenge: request.challenge,
        device: request.device,
        redirect: request.redirect,
        state: request.state,
      });
      setRedirect(out.redirect);
    } catch (e) {
      setError(e instanceof NotaError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  if (redirect) {
    return (
      <AuthShell
        title="Approved"
        description={
          <>
            Nota on <b>{request.device}</b> is signing in. This tab can close once the app shows
            your vault.
          </>
        }
      >
        <a
          href={redirect}
          data-testid="back-to-nota"
          className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-ink px-4 text-[14px] font-medium text-ink-foreground hover:bg-ink/90"
        >
          <ExternalLink className="size-4" /> Open Nota
        </a>
        <p className="mt-3 text-ui text-muted-foreground">
          Nothing happened? The app may not be installed, or the browser asked first. The button
          above sends the same one-time code.
        </p>
      </AuthShell>
    );
  }

  return (
    <AuthShell
      title="Approve this app?"
      description="A Nota app wants to sync your vault with your account. It gets its own device token, which you can revoke under Devices."
      footer={
        <a
          href="/app/"
          className="text-ui text-muted-foreground hover:text-foreground hover:underline"
        >
          Not now, back to my notes
        </a>
      }
    >
      {details([
        {
          label: "App",
          value: (
            <span className="inline-flex items-center gap-1.5">
              <Laptop className="size-3.5" /> Nota for desktop
            </span>
          ),
        },
        { label: "Device", value: request.device },
        { label: "Account", value: "You, signed in here" },
      ])}
      <div className="mt-4 flex flex-col gap-2">
        <Button size="lg" className="w-full" disabled={busy} onClick={() => void approve()}>
          {busy ? <LoaderCircle className="animate-spin motion-reduce:animate-none" /> : <Check />}
          Approve and open Nota
        </Button>
        <p className="flex items-start gap-1.5 text-small text-faint">
          <ShieldCheck className="mt-0.5 size-3.5 shrink-0" />
          The code is good for two minutes and works once. If you didn't start this, close the tab.
        </p>
      </div>
      {error ? (
        <p role="alert" className="mt-3 text-ui text-danger">
          {error}
        </p>
      ) : null}
    </AuthShell>
  );
}

/** CLI: the short code from `nota login`; approval lets the terminal poll a token. */
function CliApproval({
  auth,
  request,
}: {
  auth: AuthApi;
  request: Extract<ConnectRequest, { kind: "cli" }>;
}) {
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<"approved" | "denied" | null>(null);
  const pending = useQuery({
    queryKey: ["device-code", request.code],
    queryFn: () => auth.lookupCode(request.code),
    retry: false,
  });

  const decide = async (approve: boolean) => {
    setBusy(true);
    try {
      await auth.approveCode(request.code, approve);
      setDone(approve ? "approved" : "denied");
      if (approve) toast.success("The terminal can finish signing in");
    } catch (e) {
      fail("Couldn't answer the sign-in", e);
    } finally {
      setBusy(false);
    }
  };

  if (done) {
    return (
      <AuthShell
        title={done === "approved" ? "Signed in" : "Denied"}
        description={
          done === "approved"
            ? "The terminal has its device token now. You can close this tab."
            : "The terminal was told no. Nothing was signed in."
        }
      >
        <a
          href="/app/"
          className="inline-flex h-10 w-full items-center justify-center rounded-lg bg-ink px-4 text-[14px] font-medium text-ink-foreground hover:bg-ink/90"
        >
          Go to my notes
        </a>
      </AuthShell>
    );
  }

  if (pending.isPending) {
    return (
      <AuthShell title="Checking the code" description={`Looking up ${request.code}.`}>
        <Busy>Asking the server…</Busy>
      </AuthShell>
    );
  }

  if (pending.isError) {
    const notFound = pending.error instanceof NotaError && pending.error.status === 404;
    return (
      <AuthShell
        title={notFound ? "That code has expired" : "Couldn't check the code"}
        description={
          notFound
            ? "Sign-in codes are good for ten minutes. Run `nota login` again for a fresh one."
            : pending.error instanceof Error
              ? pending.error.message
              : String(pending.error)
        }
      >
        <code className="block rounded-md bg-code px-2.5 py-2 font-mono text-[12.5px] text-text-soft">
          nota login
        </code>
        <a
          href="/app/"
          className="mt-4 inline-flex h-10 w-full items-center justify-center rounded-lg bg-surface px-4 text-[14px] font-medium text-foreground shadow-card hover:bg-hover"
        >
          Back to my notes
        </a>
      </AuthShell>
    );
  }

  return (
    <AuthShell
      title="Approve this terminal?"
      description="The Nota CLI on the machine below wants to sign in. It gets a device token you can revoke under Devices."
      footer={
        <a
          href="/app/"
          className="text-ui text-muted-foreground hover:text-foreground hover:underline"
        >
          Not now, back to my notes
        </a>
      }
    >
      {details([
        {
          label: "Client",
          value: (
            <span className="inline-flex items-center gap-1.5">
              <Terminal className="size-3.5" /> nota CLI
            </span>
          ),
        },
        { label: "Device", value: pending.data.device },
        {
          label: "Expires",
          value: new Date(pending.data.expiresAt).toLocaleTimeString(undefined, {
            hour: "2-digit",
            minute: "2-digit",
          }),
        },
      ])}
      <div className="mt-4 flex gap-2">
        <Button size="lg" className="flex-1" disabled={busy} onClick={() => void decide(true)}>
          {busy ? <LoaderCircle className="animate-spin motion-reduce:animate-none" /> : <Check />}
          Approve
        </Button>
        <Button size="lg" variant="outline" disabled={busy} onClick={() => void decide(false)}>
          <Ban /> Deny
        </Button>
      </div>
    </AuthShell>
  );
}

/** /app/connect: sign in if needed (the gate does that), then approve. */
export function ConnectPage({ auth, request }: { auth: AuthApi; request: ConnectRequest | null }) {
  if (!request)
    return (
      <AuthShell
        title="This link is incomplete"
        description="Start the sign-in again from the Nota app (Settings → Sign in to sync) or from `nota login` in a terminal."
      >
        <a
          href="/app/"
          className="inline-flex h-10 w-full items-center justify-center rounded-lg bg-ink px-4 text-[14px] font-medium text-ink-foreground hover:bg-ink/90"
        >
          Go to my notes
        </a>
      </AuthShell>
    );
  return request.kind === "desktop" ? (
    <DesktopApproval auth={auth} request={request} />
  ) : (
    <CliApproval auth={auth} request={request} />
  );
}
