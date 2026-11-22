import { type AuthApi, type AuthConfig, type AuthSession, NotaError } from "@nota/client";
import { Button } from "@nota/ui/button";
import { Input } from "@nota/ui/input";
import { Check, Copy, KeyRound, LifeBuoy, LoaderCircle } from "lucide-react";
import { type ComponentProps, type FormEvent, type ReactNode, useState } from "react";
import { AuthShell } from "./chrome.tsx";

export type AuthMode = "signin" | "signup" | "recovery";

/** A passkey name for this browser's device, offered at sign-up. */
export function suggestPasskeyName(): string {
  if (typeof navigator === "undefined") return "This device";
  const ua = navigator.userAgent;
  if (/iPhone/.test(ua)) return "iPhone";
  if (/iPad/.test(ua)) return "iPad";
  if (/Macintosh|Mac OS X/.test(ua)) return "Mac";
  if (/Windows/.test(ua)) return "Windows PC";
  if (/Android/.test(ua)) return "Android phone";
  if (/Linux/.test(ua)) return "Linux computer";
  return "This device";
}

function message(e: unknown): string {
  if (e instanceof NotaError) {
    switch (e.code) {
      case "webauthn_failed":
        return "The passkey didn't match. Try again, or use the passkey you created on this site.";
      case "invalid_invite":
        return "That invite code is not valid for this email, or it has expired.";
      case "invite_required":
        return "Sign-up needs an invite code. Ask for one, then try again.";
      case "signup_closed":
        return "Sign-up is closed on this server.";
      case "exists":
        return "There is already an account with that email. Sign in with its passkey instead.";
      case "invalid_credentials":
        return "That recovery code is not right, or it has been used already.";
      case "rate_limited":
        return e.message;
      case "unreachable":
        return "Can't reach the server. Check your connection and try again.";
      default:
        return e.message;
    }
  }
  if (e instanceof DOMException && (e.name === "NotAllowedError" || e.name === "AbortError"))
    return "The passkey prompt was closed. Nothing changed.";
  if (e instanceof Error && e.message) return e.message;
  return "Something went wrong.";
}

function ErrorText({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="mt-3 text-ui leading-[1.45] text-danger">
      {children}
    </p>
  );
}

function Label({ children }: { children: ReactNode }) {
  return (
    <span className="mb-1.5 block text-small font-medium text-muted-foreground">{children}</span>
  );
}

function Or() {
  return (
    <div className="my-4 flex items-center gap-3 text-small text-faint">
      <span className="h-px flex-1 bg-divider" />
      or
      <span className="h-px flex-1 bg-divider" />
    </div>
  );
}

/** A full-width primary action with a busy spinner. */
function Action({ children, busy, ...props }: ComponentProps<typeof Button> & { busy?: boolean }) {
  return (
    <Button size="lg" className="w-full" disabled={busy} {...props}>
      {busy ? <LoaderCircle className="animate-spin motion-reduce:animate-none" /> : null}
      {children}
    </Button>
  );
}

export function SignInFlow({
  auth,
  config,
  initialMode,
  invite,
  onSignedIn,
  onRegistered,
}: {
  auth: AuthApi;
  config: AuthConfig | undefined;
  initialMode: AuthMode;
  invite?: string;
  onSignedIn: (session: AuthSession) => void;
  /** Signed up: the cookie is set, the codes were shown. Refetch the session. */
  onRegistered: () => void;
}) {
  const [mode, setMode] = useState<AuthMode>(initialMode);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [inviteCode, setInviteCode] = useState(invite ?? "");
  const [passkeyName, setPasskeyName] = useState(suggestPasskeyName());
  const [codes, setCodes] = useState<{ email: string; codes: string[] } | null>(null);

  const signUpOpen = config ? config.signup !== "closed" : true;
  const needsInvite = config?.signup !== "open";

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };

  const signIn = () =>
    run(async () => {
      const session = await auth.login();
      onSignedIn(session);
    });

  const signUp = (e: FormEvent) => {
    e.preventDefault();
    return run(async () => {
      const addr = email.trim();
      if (!addr.includes("@")) return setError("Enter the email address for the account.");
      if (!passkeyName.trim()) return setError("Name the passkey so you can recognise it later.");
      if (needsInvite && !inviteCode.trim())
        return setError("This server needs an invite code to sign up.");
      const out = await auth.register({
        email: addr,
        ...(inviteCode.trim() ? { invite: inviteCode.trim() } : {}),
        name: passkeyName.trim(),
      });
      setCodes({ email: out.user.email, codes: out.recoveryCodes });
    });
  };

  const recover = (e: FormEvent) => {
    e.preventDefault();
    return run(async () => {
      if (!email.trim() || !code.trim()) return setError("Enter the email and one recovery code.");
      const session = await auth.recovery(email.trim(), code.trim().toLowerCase());
      onSignedIn(session);
    });
  };

  // The 10 recovery codes are shown exactly once: make saving them the step.
  if (codes) return <RecoveryCodes {...codes} onDone={onRegistered} />;

  if (mode === "signup") {
    if (!signUpOpen) {
      return (
        <AuthShell
          title="Sign-up is closed"
          description="This server creates accounts by invitation only. Ask the owner for one."
        >
          <Button variant="outline" className="w-full" onClick={() => setMode("signin")}>
            Back to sign in
          </Button>
        </AuthShell>
      );
    }
    return (
      <AuthShell
        title="Create your account"
        description="An account is an email and a passkey. No password to forget; your device holds the key."
        footer={
          <>
            Already have an account?{" "}
            <button
              type="button"
              className="font-medium text-ink underline-offset-2 hover:underline"
              onClick={() => {
                setError(null);
                setMode("signin");
              }}
            >
              Sign in
            </button>
          </>
        }
      >
        <form onSubmit={(e) => void signUp(e)} className="flex flex-col gap-3.5">
          <label className="block">
            <Label>Email</Label>
            <Input
              type="email"
              autoComplete="username"
              placeholder="you@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoFocus
            />
          </label>
          {needsInvite ? (
            <label className="block">
              <Label>Invite code</Label>
              <Input
                placeholder="From your invitation link"
                value={inviteCode}
                onChange={(e) => setInviteCode(e.target.value)}
                spellCheck={false}
                className="font-mono text-[12.5px]"
              />
            </label>
          ) : null}
          <label className="block">
            <Label>Name this passkey</Label>
            <Input
              placeholder="MacBook Air"
              value={passkeyName}
              onChange={(e) => setPasskeyName(e.target.value)}
            />
            <span className="mt-1.5 block text-small text-faint">
              Shown in Settings so you can tell your devices apart.
            </span>
          </label>
          {error ? <ErrorText>{error}</ErrorText> : null}
          <Action type="submit" busy={busy}>
            <KeyRound /> Create account with a passkey
          </Action>
        </form>
      </AuthShell>
    );
  }

  if (mode === "recovery") {
    return (
      <AuthShell
        title="Use a recovery code"
        description="Each code works once. You'll add a new passkey right after signing in."
        footer={
          <button
            type="button"
            className="font-medium text-ink underline-offset-2 hover:underline"
            onClick={() => {
              setError(null);
              setMode("signin");
            }}
          >
            Back to passkey sign-in
          </button>
        }
      >
        <form onSubmit={(e) => void recover(e)} className="flex flex-col gap-3.5">
          <label className="block">
            <Label>Email</Label>
            <Input
              type="email"
              autoComplete="username"
              placeholder="you@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoFocus
            />
          </label>
          <label className="block">
            <Label>Recovery code</Label>
            <Input
              placeholder="xxxx-xxxx"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              className="font-mono text-[12.5px]"
              spellCheck={false}
            />
          </label>
          {error ? <ErrorText>{error}</ErrorText> : null}
          <Action type="submit" busy={busy}>
            <LifeBuoy /> Sign in
          </Action>
        </form>
      </AuthShell>
    );
  }

  return (
    <AuthShell
      title="Sign in to Nota"
      description="Your vault, in the browser. The passkey on this device finds your account — no email or password."
      footer={
        signUpOpen ? (
          <>
            New to Nota?{" "}
            <a
              href="/app/signup"
              className="font-medium text-ink underline-offset-2 hover:underline"
              onClick={(e) => {
                e.preventDefault();
                setError(null);
                setMode("signup");
              }}
            >
              Create an account
            </a>
          </>
        ) : null
      }
    >
      <div className="flex flex-col">
        <Action busy={busy} onClick={() => void signIn()}>
          <KeyRound /> Sign in with a passkey
        </Action>
        <Or />
        <Button variant="outline" size="lg" className="w-full" onClick={() => setMode("recovery")}>
          Use a recovery code
        </Button>
        {error ? <ErrorText>{error}</ErrorText> : null}
      </div>
    </AuthShell>
  );
}

/** The one moment the recovery codes exist: ten codes, copy, saved. */
function RecoveryCodes({
  email,
  codes,
  onDone,
}: {
  email: string;
  codes: string[];
  onDone: () => void;
}) {
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState(false);
  return (
    <AuthShell
      width={440}
      title="Save your recovery codes"
      description={
        <>
          If you lose your passkey, one of these codes signs you in to <b>{email}</b>. Each works
          once, and this is the only time they are shown.
        </>
      }
      footer={
        <button
          type="button"
          className="text-ui text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
          onClick={onDone}
        >
          I saved them — continue
        </button>
      }
    >
      <ol className="grid grid-cols-2 gap-1.5">
        {codes.map((c, i) => (
          <li
            key={c}
            className="flex items-baseline gap-2 rounded-md bg-code px-2 py-1.5 font-mono text-[12.5px] text-text-soft"
          >
            <span className="w-4 shrink-0 text-right text-[10px] text-faint">{i + 1}</span>
            <span className="tabular-nums">{c}</span>
          </li>
        ))}
      </ol>
      <div className="mt-4 flex flex-col gap-3">
        <Button
          variant="outline"
          className="w-full"
          onClick={() => {
            void navigator.clipboard?.writeText(codes.join("\n")).then(
              () => setCopied(true),
              () => setCopied(false),
            );
          }}
        >
          {copied ? <Check /> : <Copy />} {copied ? "Copied" : "Copy all codes"}
        </Button>
        <label className="flex cursor-pointer items-start gap-2.5 text-ui text-text-soft">
          <input
            type="checkbox"
            checked={saved}
            onChange={(e) => setSaved(e.target.checked)}
            className="mt-0.5 size-4 accent-[var(--ink)]"
          />
          <span>
            I've put them somewhere safe — a password manager, or a printout. I understand they
            can't be shown again.
          </span>
        </label>
        <Action disabled={!saved} onClick={onDone}>
          Continue to my notes
        </Action>
      </div>
    </AuthShell>
  );
}
