import type { AccountState, Device } from "@nota/client";
import { Button } from "@nota/ui/button";
import { Input } from "@nota/ui/input";
import { toast } from "@nota/ui/sonner";
import { cn } from "@nota/ui/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Cloud, ExternalLink, FolderOpen, Laptop, LoaderCircle } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { fail } from "../actions.ts";
import { useClient } from "../context.ts";
import { relativeTime } from "../lib/format.ts";
import { keys, useVaultInfo } from "../queries.ts";
import { TopBar } from "./TopBar.tsx";

function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="py-5 [section+&]:border-t [section+&]:border-divider">
      <h2 className="text-[14px] font-semibold text-foreground">{title}</h2>
      {description ? <p className="mt-1 text-ui text-muted-foreground">{description}</p> : null}
      <div className="mt-4">{children}</div>
    </section>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[120px_1fr] items-center gap-3 py-1.5 text-ui max-[520px]:grid-cols-1 max-[520px]:gap-1">
      <span className="text-muted-foreground">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function useAccount() {
  const client = useClient();
  const qc = useQueryClient();
  const account = client.account;
  const q = useQuery({
    queryKey: keys.account,
    queryFn: () => account!.state(),
    enabled: !!account,
  });
  // State changes (the deep link coming back, sync starting) arrive as events.
  useEffect(() => account?.subscribe((st) => qc.setQueryData(keys.account, st)), [account, qc]);
  return q.data;
}

function Devices() {
  const client = useClient();
  const qc = useQueryClient();
  const devices = useQuery({
    queryKey: [...keys.account, "devices"],
    queryFn: () => client.account!.devices(),
  });
  const revoke = useMutation({
    mutationFn: (d: Device) => client.account!.revokeDevice(d.id),
    onSuccess: (_, d) => {
      toast.success(`Signed out ${d.device}`);
      void qc.invalidateQueries({ queryKey: [...keys.account, "devices"] });
    },
    onError: (e) => fail("Couldn't sign the device out", e),
  });
  if (devices.isError)
    return (
      <p className="text-ui text-muted-foreground">
        Couldn't load devices: {String(devices.error)}
      </p>
    );
  return (
    <ul className="overflow-hidden rounded-lg border border-hairline">
      {(devices.data ?? []).map((d) => (
        <li
          key={d.id}
          className="flex items-center gap-3 border-t border-divider px-3.5 py-2.5 text-ui first:border-t-0 pointer-coarse:py-3"
        >
          <Laptop className="size-4 text-faint" strokeWidth={1.75} />
          <div className="min-w-0 flex-1">
            <div className="truncate text-foreground">
              {d.device}
              {d.current ? <span className="ml-2 text-small text-ink">This device</span> : null}
            </div>
            <div className="text-small text-faint">
              {d.lastUsedAt
                ? `Last seen ${relativeTime(d.lastUsedAt).toLowerCase()}`
                : `Added ${relativeTime(d.createdAt).toLowerCase()}`}
            </div>
          </div>
          {!d.current ? (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => revoke.mutate(d)}
              disabled={revoke.isPending}
            >
              Sign out
            </Button>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

function ChooseVault({ st }: { st: AccountState }) {
  const client = useClient();
  const [name, setName] = useState(st.suggested ?? "");
  const choose = useMutation({
    mutationFn: ({ name, create }: { name: string; create: boolean }) =>
      client.account!.chooseVault(name, create),
    onError: (e) => fail("Couldn't link the vault", e),
  });
  return (
    <div className="flex flex-col gap-3">
      <p className="text-ui text-text-soft">
        Signed in{st.email ? ` as ${st.email}` : ""}. Pick the server vault to sync this folder
        with. Nothing is overwritten: the first sync merges both sides.
      </p>
      {st.vaults.length ? (
        <ul className="overflow-hidden rounded-lg border border-hairline">
          {st.vaults.map((v) => (
            <li
              key={v.name}
              className="flex items-center gap-3 border-t border-divider px-3.5 py-2 text-ui first:border-t-0"
            >
              <Cloud className="size-4 text-faint" strokeWidth={1.75} />
              <span className="flex-1 truncate">{v.name}</span>
              <Button
                size="sm"
                variant="secondary"
                disabled={choose.isPending}
                onClick={() => choose.mutate({ name: v.name, create: false })}
              >
                Sync with this
              </Button>
            </li>
          ))}
        </ul>
      ) : null}
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (name.trim()) choose.mutate({ name: name.trim(), create: true });
        }}
      >
        <Input
          aria-label="New vault name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="New vault name"
          className="max-w-60"
        />
        <Button type="submit" disabled={!name.trim() || choose.isPending}>
          Create and sync
        </Button>
      </form>
    </div>
  );
}

function SyncAccount() {
  const client = useClient();
  const st = useAccount();
  const [busy, setBusy] = useState(false);
  if (!client.account) {
    return (
      <Section
        title="Account"
        description="This vault lives on the server; every change is saved there."
      >
        <span />
      </Section>
    );
  }
  if (!st) return null;
  const act = async (fn: () => Promise<unknown>, what: string) => {
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      fail(what, e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Section
      title="Sync"
      description={
        st.state === "signed-in"
          ? "This vault syncs in the background. The files on this device stay the source of truth."
          : "Sign in to keep this vault in sync with your other devices and the web. The notes stay plain files here."
      }
    >
      {st.state === "signed-out" || st.state === "error" ? (
        <div className="flex flex-col items-start gap-3">
          {st.error ? <p className="text-ui text-danger">{st.error}</p> : null}
          <Button
            onClick={() => void act(() => client.account!.signIn(), "Couldn't start signing in")}
            disabled={busy}
          >
            <Cloud /> Sign in to sync
          </Button>
          <p className="text-small text-faint">
            Opens {new URL(st.connectUrl ?? "https://nota.banjoanton.com").host} in your browser;
            you come back here when done.
          </p>
        </div>
      ) : null}
      {st.state === "pending" ? (
        <div className="flex flex-col items-start gap-3">
          <p className="flex items-center gap-2 text-ui text-text-soft">
            <LoaderCircle
              className="size-4 animate-spin text-ink motion-reduce:animate-none"
              strokeWidth={2}
            />
            Finish signing in in your browser…
          </p>
          {st.error ? <p className="text-ui text-muted-foreground">{st.error}</p> : null}
          <div className="flex gap-2">
            {st.connectUrl ? (
              <Button variant="secondary" asChild>
                <a href={st.connectUrl} target="_blank" rel="noreferrer">
                  <ExternalLink /> Open the page again
                </a>
              </Button>
            ) : null}
            <Button
              variant="ghost"
              onClick={() => void act(() => client.account!.cancelSignIn(), "Couldn't cancel")}
            >
              Cancel
            </Button>
          </div>
        </div>
      ) : null}
      {st.state === "choose-vault" ? <ChooseVault st={st} /> : null}
      {st.state === "signed-in" ? (
        <div className="flex flex-col gap-4">
          <div>
            <Field label="Account">
              <span className="flex items-center gap-1.5 text-foreground">
                <Check className="size-3.5 text-success" strokeWidth={2.5} />
                {st.email ?? "Signed in"}
              </span>
            </Field>
            <Field label="Server vault">{st.vault}</Field>
            <Field label="Server">
              <span className="truncate text-muted-foreground">{st.server}</span>
            </Field>
            <Field label="This device">{st.device}</Field>
          </div>
          <div>
            <h3 className="mb-2 text-small font-medium text-muted-foreground">Devices</h3>
            <Devices />
          </div>
          <div>
            <Button
              variant="outline"
              onClick={() => void act(() => client.account!.signOut(), "Couldn't sign out")}
              disabled={busy}
            >
              Sign out
            </Button>
            <p className="mt-2 text-small text-faint">
              Signing out stops syncing. The notes on this device stay as they are.
            </p>
          </div>
        </div>
      ) : null}
    </Section>
  );
}

export function Settings() {
  const client = useClient();
  const info = useVaultInfo();
  return (
    <div className="h-full overflow-y-auto">
      <TopBar crumbs={[{ label: "Settings" }]} />
      <div className="mx-auto w-full max-w-[640px] px-14 pt-10 pb-24 max-[720px]:px-5 max-[720px]:pt-5">
        <h1 className="mb-5 text-[26px] font-bold tracking-[-0.015em] text-foreground">Settings</h1>
        <Section
          title="Vault"
          description="A folder of Markdown files. Any editor can open it; Nota keeps its index and settings in .nota."
        >
          <Field label="Name">{info.data?.name}</Field>
          <Field label={client.kind === "desktop" ? "Folder" : "Server vault"}>
            <div className="flex items-center gap-2">
              <code
                className={cn(
                  "min-w-0 truncate rounded-xs bg-code px-1.5 py-0.5 font-mono text-[12px] text-text-soft",
                )}
              >
                {info.data?.dir}
              </code>
              {client.shell ? (
                <Button variant="ghost" size="sm" onClick={() => void client.shell!.revealVault()}>
                  <FolderOpen /> Show
                </Button>
              ) : null}
            </div>
          </Field>
          <Field label="Inbox folder">{info.data?.inbox}</Field>
        </Section>
        <SyncAccount />
      </div>
    </div>
  );
}
