import type { AccountState, FolderInfo, SetupApi } from "@nota/client";
import { Button } from "@nota/ui/button";
import { Input } from "@nota/ui/input";
import { cn } from "@nota/ui/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, CircleAlert, Cloud, Folder, Info, LoaderCircle } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { Mark } from "../auth/chrome.tsx";
import { useClient } from "../context.ts";
import { keys } from "../queries.ts";
import {
  baseName,
  canAdvance,
  checkName,
  type Draft,
  deriveVault,
  type Flow,
  initialDraft,
  inboxOf,
  nextStep,
  notesLine,
  plural,
  prevStep,
  type Step,
  STEP_LABEL,
  stepsFor,
  type VaultMode,
} from "./setup.ts";

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/** The sign-in state of the account, kept current by the app's events. */
function useAccountState() {
  const client = useClient();
  const qc = useQueryClient();
  const account = client.account;
  const q = useQuery({
    queryKey: keys.account,
    queryFn: () => account!.state(),
    enabled: !!account,
  });
  useEffect(() => account?.subscribe((st) => qc.setQueryData(keys.account, st)), [account, qc]);
  return q.data;
}

function Msg({ tone, children }: { tone?: "err" | "ok"; children: ReactNode }) {
  if (!children) return null;
  const Icon = tone === "err" ? CircleAlert : Check;
  return (
    <p
      className={cn(
        "mt-1.5 flex items-start gap-1.5 text-[12px] leading-[1.45]",
        tone === "err" ? "text-danger" : "text-muted-foreground",
      )}
    >
      {tone ? (
        <Icon
          aria-hidden
          className={cn("mt-[2px] size-3.5 shrink-0", tone === "ok" && "text-ink")}
          strokeWidth={1.75}
        />
      ) : null}
      <span>{children}</span>
    </p>
  );
}

function Label({ htmlFor, children }: { htmlFor: string; children: ReactNode }) {
  return (
    <label htmlFor={htmlFor} className="mb-1.5 block text-[12px] font-medium text-text-soft">
      {children}
    </label>
  );
}

/** A path on a code-coloured line, with an optional tag on the right. */
function PathBox({
  path,
  tag,
  tone = "ok",
}: {
  path: string;
  tag?: string;
  tone?: "ok" | "bad" | "dim";
}) {
  return (
    <div
      className={cn(
        "flex min-h-[34px] items-center gap-2 rounded-md bg-code px-2.5 py-2 font-mono text-[12px] text-text-soft",
        tone === "bad" &&
          "shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--danger)_55%,transparent)]",
        tone === "dim" && "text-faint",
      )}
    >
      {tone === "bad" ? (
        <CircleAlert aria-hidden className="size-3.5 shrink-0 text-danger" strokeWidth={1.75} />
      ) : (
        <Folder
          aria-hidden
          className={cn("size-3.5 shrink-0", tone === "dim" ? "text-faint" : "text-ink")}
          strokeWidth={1.75}
        />
      )}
      <span data-testid="vault-path" className="min-w-0 flex-1 break-all select-text">
        {path}
      </span>
      {tag ? (
        <span className="rounded-full bg-ink-soft px-[7px] py-[2px] font-sans text-[11px] whitespace-nowrap text-ink">
          {tag}
        </span>
      ) : null}
    </div>
  );
}

function RadioCard({
  checked,
  onChange,
  title,
  hint,
}: {
  checked: boolean;
  onChange: () => void;
  title: string;
  hint: string;
}) {
  return (
    <label
      className={cn(
        "flex cursor-default flex-col gap-1.5 rounded-[10px] border px-3 py-2.5 transition-[border-color,background-color] duration-150",
        "has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-ink",
        checked ? "border-ink bg-ink-soft" : "border-hairline hover:bg-hover",
      )}
    >
      <input
        type="radio"
        name="vault-mode"
        checked={checked}
        onChange={onChange}
        className="m-0 size-3.5 accent-[var(--ink)]"
      />
      <span>
        <b className="block text-[13px] font-semibold">{title}</b>
        <small className="mt-px block text-[12px] text-muted-foreground">{hint}</small>
      </span>
    </label>
  );
}

interface WizardProps {
  flow: Flow;
  setup: SetupApi;
  defaultLocation: string;
  initialMode?: VaultMode;
  /** The inbox to start from: the open vault's when switching. */
  initialInbox?: string;
  onCancel?: () => void;
}

function Wizard({
  flow,
  setup,
  defaultLocation,
  initialMode = "create",
  initialInbox,
  onCancel,
}: WizardProps) {
  const client = useClient();
  const platform = client.shell?.platform ?? "linux";
  const steps = stepsFor(flow);
  const [step, setStep] = useState<Step>(steps[0]!);
  const [draft, setDraft] = useState<Draft>(() =>
    initialDraft(defaultLocation, initialMode, initialInbox),
  );
  const [touched, setTouched] = useState<Record<string, boolean>>({});
  const [typing, setTyping] = useState(false);
  const [pickError, setPickError] = useState("");
  const [opening, setOpening] = useState(false);
  const [slow, setSlow] = useState(false);
  const [error, setError] = useState("");
  const account = useAccountState();
  const signingIn = account?.state === "pending";

  const touch = (k: string) => setTouched((t) => ({ ...t, [k]: true }));
  const set = (patch: Partial<Draft>) => setDraft((d) => ({ ...d, ...patch }));

  // What the folder says, looked up a moment after the typing stops.
  const probe = useDebounced(
    draft.mode === "create" ? `${draft.location}\n${draft.name}` : draft.existing,
    150,
  );
  const locationProblem = useQuery({
    queryKey: ["setup", "location", draft.location],
    queryFn: () => setup.checkLocation(draft.location),
    enabled: draft.mode === "create" && draft.location.trim() !== "",
    staleTime: 0,
  });
  const locInfo = useQuery({
    queryKey: ["setup", "inspect", draft.location],
    queryFn: () => setup.inspectFolder(draft.location),
    enabled: draft.mode === "create" && draft.location.trim() !== "" && !locationProblem.data,
    staleTime: 0,
    gcTime: 0,
  });
  const pre = deriveVault(draft, platform, null, locationProblem.data ?? "");
  const inspected = useQuery({
    queryKey: ["setup", "inspect", pre.target],
    queryFn: () => setup.inspectFolder(pre.target),
    enabled: pre.target !== "" && probe !== "" && !pre.locationError,
    staleTime: 0,
    gcTime: 0,
  });
  const info: FolderInfo | null = inspected.data ?? null;
  const view = deriveVault(draft, platform, info, locationProblem.data ?? "");
  const inboxCheck = checkName(draft.inbox, "inbox");
  const welcomeNow = view.canWelcome && draft.welcome;

  const go = (s: Step) => {
    setError("");
    setStep(s);
  };
  const advance = () => {
    if (!canAdvance(step, draft, view, signingIn)) {
      setTouched((t) => ({ ...t, all: true }));
      return;
    }
    go(nextStep(flow, step));
  };
  const back = () => {
    const p = prevStep(flow, step);
    if (p) go(p);
    else onCancel?.();
  };

  const finish = async () => {
    if (opening) return;
    setOpening(true);
    setError("");
    try {
      const inbox = inboxOf(draft);
      if (draft.mode === "create")
        await setup.createVault(draft.location, draft.name, inbox, welcomeNow);
      else await setup.useVault(view.path, inbox, welcomeNow);
      // The app relaunches to open the vault; if it doesn't, say so.
      setTimeout(() => setSlow(true), 10_000);
    } catch (e) {
      setError(message(e));
      setOpening(false);
    }
  };

  const pick = async (what: "location" | "existing") => {
    setPickError("");
    try {
      const start = what === "location" ? draft.location : draft.existing || draft.location;
      const p = await setup.pickFolder(
        what === "location" ? "Choose a location" : "Choose a folder to open",
        start,
      );
      if (!p) return;
      if (what === "location") set({ location: p });
      else set({ existing: p });
      touch(what);
    } catch {
      setPickError("The folder chooser isn’t available here. Type the path instead.");
      setTyping(true);
    }
  };

  // Enter continues, Esc goes back.
  const act = useRef({ advance, back, finish, step, opening });
  act.current = { advance, back, finish, step, opening };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.isComposing || e.metaKey || e.ctrlKey || e.altKey) return;
      const a = act.current;
      if (a.opening) return;
      if (e.key === "Enter") {
        const t = e.target as HTMLElement | null;
        if (
          t &&
          (t.tagName === "BUTTON" || t.tagName === "A" || t.getAttribute("role") === "radio")
        )
          return;
        e.preventDefault();
        if (a.step === "done") void a.finish();
        else a.advance();
      } else if (e.key === "Escape") {
        e.preventDefault();
        a.back();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  // Each step puts the cursor where the next thing to do is.
  useEffect(() => {
    const t = setTimeout(() => {
      const el = document.querySelector<HTMLElement>("[data-autofocus]");
      el?.focus();
    }, 30);
    return () => clearTimeout(t);
  }, [step, draft.mode]);

  // Once the browser hands the sign-in back, Enter should mean "continue".
  const accountState = account?.state;
  useEffect(() => {
    if (step === "sync") document.querySelector<HTMLElement>("[data-autofocus]")?.focus();
  }, [step, accountState]);

  const idx = steps.indexOf(step);
  const dots = (
    <>
      <div className="mt-4 flex items-center justify-center gap-1.5" aria-hidden>
        {steps.map((s, i) => (
          <span
            key={s}
            className={cn(
              "h-1.5 rounded-full transition-[width,background-color] duration-200 ease-out",
              i === idx
                ? "w-[18px] bg-ink"
                : i < idx
                  ? "w-1.5 bg-ink opacity-45"
                  : "w-1.5 bg-hairline",
            )}
          />
        ))}
      </div>
      <p className="mt-2 text-center text-[12px] text-faint">
        Step {idx + 1} of {steps.length}
        {step !== "welcome" ? ` · ${STEP_LABEL[step]}` : ""}
      </p>
    </>
  );

  const nav = (extra?: ReactNode, label = "Continue", quiet = false) => (
    <div className="mt-[22px] flex items-center gap-2">
      <Button variant="ghost" onClick={back}>
        {prevStep(flow, step) ? "Back" : "Cancel"}
      </Button>
      <span className="flex-1" />
      {extra}
      <Button
        onClick={advance}
        variant={quiet ? "outline" : "default"}
        data-autofocus={step === "sync" ? "" : undefined}
        aria-disabled={step !== "done" && !canAdvance(step, draft, view, signingIn)}
        className={cn(
          step !== "done" &&
            !canAdvance(step, draft, view, signingIn) &&
            "opacity-45 active:scale-100",
        )}
      >
        {label}
      </Button>
    </div>
  );

  const all = !!touched.all;
  const nameErr = (touched.name || all) && view.nameError ? view.nameError : "";
  const locErr =
    view.locationError && (touched.location || touched.existing || all) ? view.locationError : "";

  let body: ReactNode;
  if (step === "welcome") {
    return (
      <Frame>
        <section className="animate-rise motion-reduce:animate-none rounded-xl bg-surface px-8 pt-9 pb-[30px] text-center shadow-card">
          <div className="mx-auto mb-[18px] w-fit">
            <Mark size={84} />
          </div>
          <h1 className="text-[24px] font-semibold tracking-[-0.015em]">Welcome to Nota</h1>
          <p className="mx-auto mt-2.5 max-w-[340px] text-[14px] leading-[1.5] text-muted-foreground">
            Your notes are plain Markdown files in a folder you choose. No lock-in, no database to
            look after.
          </p>
          <Button size="lg" className="mt-6" onClick={advance} data-autofocus>
            Get started
          </Button>
          <p className="mt-3.5 text-[12px] text-faint">
            Takes about a minute. Nothing leaves this computer unless you turn on sync.
          </p>
        </section>
        {dots}
      </Frame>
    );
  }

  if (step === "vault") {
    const create = draft.mode === "create";
    body = (
      <>
        <Heading
          title="Where should your notes live?"
          sub="Create a fresh vault, or point Nota at a folder you use already."
        />
        <div className="mt-4 grid gap-3">
          <div role="radiogroup" aria-label="Vault" className="grid grid-cols-2 gap-2">
            <RadioCard
              checked={create}
              onChange={() => set({ mode: "create" })}
              title="Create a new vault"
              hint="A new folder, named and placed by you"
            />
            <RadioCard
              checked={!create}
              onChange={() => set({ mode: "open" })}
              title="Open an existing folder"
              hint="Use the Markdown files you have"
            />
          </div>
          {create ? (
            <>
              <div>
                <Label htmlFor="setup-name">Name</Label>
                <Input
                  id="setup-name"
                  data-autofocus
                  value={draft.name}
                  aria-invalid={!!nameErr}
                  aria-describedby="setup-name-msg"
                  autoComplete="off"
                  spellCheck={false}
                  onChange={(e) => {
                    set({ name: e.target.value });
                    touch("name");
                  }}
                  className="h-[34px]"
                />
                <div id="setup-name-msg" aria-live="polite">
                  <Msg tone="err">{nameErr}</Msg>
                </div>
              </div>
              <div>
                <Label htmlFor="setup-location">Location</Label>
                <div className="flex gap-2">
                  <Input
                    id="setup-location"
                    value={draft.location}
                    aria-invalid={!!locErr}
                    aria-describedby="setup-location-msg"
                    autoComplete="off"
                    spellCheck={false}
                    onChange={(e) => {
                      set({ location: e.target.value });
                      touch("location");
                    }}
                    className="h-[34px] font-mono text-[12.5px]"
                  />
                  <Button
                    variant="outline"
                    className="h-[34px]"
                    onClick={() => void pick("location")}
                  >
                    Choose…
                  </Button>
                </div>
                <div id="setup-location-msg" aria-live="polite">
                  <Msg tone="err">{locErr}</Msg>
                  <Msg tone="err">{pickError}</Msg>
                </div>
              </div>
              <div aria-live="polite">
                {view.kind === "conflict" ? (
                  <>
                    <PathBox path={view.path} tone="bad" />
                    <Msg tone="err">
                      A folder named “{baseName(view.path)}” already exists here and isn’t empty
                      {view.notes ? ` (${plural(view.notes, "note")})` : ""}.
                    </Msg>
                    <Button
                      size="sm"
                      variant="outline"
                      className="mt-2"
                      onClick={() => {
                        set({ mode: "open", existing: view.path });
                        touch("existing");
                      }}
                    >
                      Use existing folder instead
                    </Button>
                  </>
                ) : view.ok ? (
                  <>
                    <PathBox
                      path={view.path}
                      tag={view.kind === "empty" ? "Empty folder" : "New folder"}
                    />
                    {locInfo.data && !locInfo.data.exists && !locErr ? (
                      <Msg>This folder doesn’t exist yet, so Nota will create it too.</Msg>
                    ) : null}
                  </>
                ) : view.unwritable ? (
                  <>
                    <PathBox path={view.path} tone="bad" />
                    <Msg tone="err">Nota can’t write to “{view.path}”. Pick another location.</Msg>
                  </>
                ) : (
                  <PathBox
                    path={`${draft.location.trim() || "…"}/${draft.name.trim() || "…"}`}
                    tone="dim"
                  />
                )}
              </div>
            </>
          ) : (
            <div className="mt-1">
              {draft.existing === "" ? (
                <>
                  <button
                    type="button"
                    data-autofocus
                    onClick={() => void pick("existing")}
                    className="flex h-16 w-full items-center justify-center gap-2 rounded-lg border border-dashed border-hairline text-ui transition-colors duration-150 outline-none hover:border-faint hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
                  >
                    <Folder aria-hidden className="size-3.5" strokeWidth={1.75} />
                    Choose a folder…
                  </button>
                  {typing ? null : (
                    <button
                      type="button"
                      className="mt-2 text-[12px] text-muted-foreground underline-offset-2 hover:underline"
                      onClick={() => {
                        setTyping(true);
                        requestAnimationFrame(() =>
                          document.getElementById("setup-existing")?.focus(),
                        );
                      }}
                    >
                      Type a path instead
                    </button>
                  )}
                  {touched.all ? <Msg tone="err">Choose a folder to continue.</Msg> : null}
                </>
              ) : null}
              {typing || draft.existing !== "" ? (
                <div className={cn(draft.existing === "" && "mt-2")}>
                  <Label htmlFor="setup-existing">Folder</Label>
                  <div className="flex gap-2">
                    <Input
                      id="setup-existing"
                      value={draft.existing}
                      aria-invalid={!!locErr}
                      autoComplete="off"
                      spellCheck={false}
                      onChange={(e) => {
                        set({ existing: e.target.value });
                        touch("existing");
                      }}
                      className="h-[34px] font-mono text-[12.5px]"
                    />
                    <Button
                      variant="outline"
                      className="h-[34px]"
                      onClick={() => void pick("existing")}
                    >
                      Choose…
                    </Button>
                  </div>
                </div>
              ) : null}
              <div aria-live="polite" className="mt-2.5">
                <Msg tone="err">{locErr}</Msg>
                <Msg tone="err">{pickError}</Msg>
                {view.kind && !view.locationError ? (
                  <>
                    <PathBox path={view.path} tone={view.unwritable ? "bad" : "ok"} />
                    {view.unwritable ? (
                      <Msg tone="err">Nota can’t write to “{view.path}”.</Msg>
                    ) : (
                      <Msg tone="ok">{notesLine(view)}</Msg>
                    )}
                  </>
                ) : null}
              </div>
            </div>
          )}
        </div>
        {nav()}
      </>
    );
  } else if (step === "inbox") {
    const err =
      (touched.inbox || all) && inboxCheck.error && draft.inbox.trim() !== ""
        ? inboxCheck.error
        : "";
    body = (
      <>
        <Heading
          title="Where should quick captures go?"
          sub="Notes you jot down quickly land in one folder to sort later. It’s called Inbox unless you rename it. You can skip this."
        />
        <div className="mt-4">
          <Label htmlFor="setup-inbox">Inbox folder</Label>
          <Input
            id="setup-inbox"
            data-autofocus
            value={draft.inbox}
            aria-invalid={!!err}
            aria-describedby="setup-inbox-msg"
            autoComplete="off"
            spellCheck={false}
            onChange={(e) => {
              set({ inbox: e.target.value });
              touch("inbox");
            }}
            className="h-[34px]"
          />
          <div id="setup-inbox-msg" aria-live="polite">
            {err ? (
              <Msg tone="err">{err}</Msg>
            ) : (
              <p className="mt-1.5 text-[12px] leading-[1.45] text-muted-foreground">
                {view.path
                  ? `Captures are saved in ${view.path}/${draft.inbox.trim() || "…"}/`
                  : "A folder inside your vault where quick captures land."}
              </p>
            )}
          </div>
        </div>
        {nav(
          <Button
            variant="ghost"
            onClick={() => {
              set({ inbox: "Inbox" });
              go(nextStep(flow, step));
            }}
          >
            Skip
          </Button>,
        )}
      </>
    );
  } else if (step === "sync") {
    body = (
      <>
        <Heading
          title="Sync across devices?"
          sub="Optional. Sign in to keep this vault in step with your other devices. Nota works fully offline without an account."
        />
        <SyncBox account={account} />
        {nav(
          undefined,
          account?.state === "choose-vault" ? "Continue" : "Skip for now",
          account?.state !== "choose-vault",
        )}
      </>
    );
  } else {
    const signed = account?.state === "choose-vault";
    body = (
      <>
        <Heading
          title="You’re all set"
          sub={
            flow === "setup"
              ? "Nota will open this vault now."
              : "Nota will restart to open this vault."
          }
        />
        <dl className="mt-[18px] grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 rounded-[10px] bg-sidebar px-3.5 py-3 text-[12.5px]">
          <dt className="text-muted-foreground">Vault</dt>
          <dd className="min-w-0 font-mono break-all">{view.path}</dd>
          <dt className="text-muted-foreground">Inbox</dt>
          <dd>{draft.inbox.trim() || "Inbox"}</dd>
          {flow === "setup" ? (
            <>
              <dt className="text-muted-foreground">Sync</dt>
              <dd>
                {signed
                  ? `Signed in${account?.email ? ` as ${account.email}` : ""}`
                  : "Off. You can turn it on later."}
              </dd>
            </>
          ) : null}
        </dl>
        <div className="mt-4">
          {view.canWelcome ? (
            <label className="flex items-start gap-2.5">
              <input
                type="checkbox"
                checked={draft.welcome}
                onChange={(e) => set({ welcome: e.target.checked })}
                className="mt-0.5 size-[15px] accent-[var(--ink)]"
              />
              <span>
                <b className="block font-medium">Add a welcome note</b>
                <small className="mt-px block text-[12px] text-muted-foreground">
                  A short starter note that explains the basics.
                </small>
              </span>
            </label>
          ) : (
            <p className="flex items-start gap-1.5 text-[12px] text-muted-foreground">
              <Info aria-hidden className="mt-[2px] size-3.5 shrink-0" strokeWidth={1.75} />
              <span>Your existing files stay exactly as they are.</span>
            </p>
          )}
        </div>
        <div aria-live="polite">
          <Msg tone="err">{error}</Msg>
          {opening ? (
            <p className="mt-3 flex items-center gap-2 text-ui text-muted-foreground">
              <LoaderCircle
                aria-hidden
                className="size-4 animate-spin text-ink motion-reduce:animate-none"
                strokeWidth={2}
              />
              {slow
                ? "Nota should restart on its own. If it doesn’t, quit and open it again."
                : "Opening your vault…"}
            </p>
          ) : null}
        </div>
        <div className="mt-[22px] flex items-center gap-2">
          <Button variant="ghost" onClick={back} disabled={opening}>
            Back
          </Button>
          <span className="flex-1" />
          <Button data-autofocus onClick={() => void finish()} disabled={opening}>
            Open vault
          </Button>
        </div>
      </>
    );
  }

  return (
    <Frame>
      <section
        key={step}
        className="animate-rise motion-reduce:animate-none rounded-xl bg-surface p-6 shadow-card"
      >
        {body}
      </section>
      {dots}
    </Frame>
  );
}

function Frame({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-full flex-col items-center justify-center bg-background px-5 py-8">
      <div className="w-full max-w-[460px]">{children}</div>
    </div>
  );
}

function Heading({ title, sub }: { title: string; sub: string }) {
  return (
    <>
      <h1 className="text-[20px] font-semibold tracking-[-0.015em] text-foreground">{title}</h1>
      <p className="mt-1.5 text-ui leading-[1.5] text-muted-foreground">{sub}</p>
    </>
  );
}

function SyncBox({ account }: { account?: AccountState }) {
  const client = useClient();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setErr("");
    try {
      await fn();
    } catch (e) {
      setErr(message(e));
    } finally {
      setBusy(false);
    }
  };
  const st = account?.state ?? "signed-out";
  const shown = st === "error" ? account?.error : err;
  return (
    <div aria-live="polite">
      <div className="mt-[18px] flex items-center gap-3 rounded-[10px] bg-sidebar p-3.5">
        {st === "pending" ? (
          <>
            <span className="flex flex-1 items-center gap-2 text-muted-foreground">
              <LoaderCircle
                aria-hidden
                className="size-3.5 animate-spin text-ink motion-reduce:animate-none"
                strokeWidth={2}
              />
              Finish signing in in your browser…
            </span>
            <Button
              variant="outline"
              onClick={() => void run(() => client.account!.cancelSignIn())}
            >
              Cancel
            </Button>
          </>
        ) : st === "choose-vault" ? (
          <>
            <span className="flex flex-1 items-center gap-2 text-foreground">
              <Check aria-hidden className="size-3.5 text-ink" strokeWidth={2} />
              Signed in{account?.email ? ` as ${account.email}` : ""}
            </span>
            <Button variant="ghost" onClick={() => void run(() => client.account!.signOut())}>
              Sign out
            </Button>
          </>
        ) : (
          <>
            <span className="flex flex-1 items-center gap-2 text-muted-foreground">
              <Cloud aria-hidden className="size-3.5 text-ink" strokeWidth={1.75} />
              Not signed in
            </span>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => void run(() => client.account!.signIn())}
            >
              Sign in to sync
            </Button>
          </>
        )}
      </div>
      {shown ? <Msg tone="err">{shown}</Msg> : null}
      {st === "pending" && account?.connectUrl ? (
        <p className="mt-2 text-[12px] text-faint">
          Nothing opened?{" "}
          <a
            className="underline underline-offset-2"
            href={account.connectUrl}
            target="_blank"
            rel="noreferrer"
          >
            Open the page again
          </a>
          .
        </p>
      ) : null}
    </div>
  );
}

/**
 * The setup wizard: first run (welcome, vault, inbox, optional sign-in,
 * done) and, from Settings or ⌘K, switching to or creating another vault.
 * Enter continues, Esc goes back; everything is reachable by keyboard.
 * Finishing writes the config and the app relaunches into the vault.
 */
export function Onboarding({
  flow,
  initialMode,
  onCancel,
}: {
  flow: Flow;
  initialMode?: VaultMode;
  onCancel?: () => void;
}) {
  const client = useClient();
  const setup = client.shell?.setup;
  const loc = useQuery({
    queryKey: ["setup", "defaultLocation"],
    queryFn: () => setup!.defaultLocation(),
    enabled: !!setup,
    staleTime: Infinity,
  });
  // Switching starts from the inbox the open vault uses.
  const info = useQuery({
    queryKey: keys.info,
    queryFn: () => client.info(),
    enabled: !!setup && flow === "switch",
    staleTime: Infinity,
  });
  if (!setup || loc.data === undefined || (flow === "switch" && info.isPending)) return null;
  return (
    <Wizard
      flow={flow}
      setup={setup}
      defaultLocation={loc.data}
      initialMode={initialMode}
      initialInbox={info.data?.inbox || undefined}
      onCancel={onCancel}
    />
  );
}
