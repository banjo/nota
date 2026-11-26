import type { Share, ShareRequest } from "@nota/client";
import { Button } from "@nota/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@nota/ui/dialog";
import { Input } from "@nota/ui/input";
import { toast } from "@nota/ui/sonner";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, ExternalLink, Globe, LoaderCircle, Lock, Palette } from "lucide-react";
import { type FormEvent, type ReactNode, useState } from "react";
import { create } from "zustand";
import { fail, openExternal } from "../actions.ts";
import { useClient } from "../context.ts";
import { currentShareTheme, sameTheme } from "../lib/share.ts";
import { keys, useNote } from "../queries.ts";
import { useUI } from "../state.ts";

// Sharing a note: a link to a web page of the note on the sync server,
// live, optionally behind a password, in the sharer's theme. ⌘K "Share
// note…" and the note's "…" menu open it.

export const useShareDialog = create<{ path: string | null }>(() => ({ path: null }));
export const openShare = (path: string) => useShareDialog.setState({ path });
const close = () => useShareDialog.setState({ path: null });

export function ShareDialog() {
  const path = useShareDialog((s) => s.path);
  // Keep the content while the dialog closes.
  const [last, setLast] = useState(path);
  if (path && path !== last) setLast(path);
  return (
    <Dialog open={!!path} onOpenChange={(o) => !o && close()}>
      <DialogContent showClose className="max-w-[460px]">
        {last ? <ShareBody key={last} path={last} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function ShareBody({ path }: { path: string }) {
  const client = useClient();
  const qc = useQueryClient();
  const note = useNote(path);
  const shares = useQuery({
    queryKey: keys.shares,
    queryFn: () => client.shares!.list(),
    enabled: !!client.shares,
    retry: false,
  });
  const share = shares.data?.find((s) => s.path === path);
  const title = note.data?.title || path.split("/").pop()?.replace(/\.md$/, "") || path;

  const put = useMutation({
    mutationFn: (req: ShareRequest) => client.shares!.put(path, req),
    onSuccess: (s) => {
      qc.setQueryData<Share[]>(keys.shares, (list = []) => [
        ...list.filter((x) => x.path !== s.path),
        s,
      ]);
    },
    onError: (e) => fail("Couldn't share the note", e),
  });
  const remove = useMutation({
    mutationFn: () => client.shares!.remove(path),
    onSuccess: () => {
      qc.setQueryData<Share[]>(keys.shares, (list = []) => list.filter((x) => x.path !== path));
      toast("Stopped sharing", { description: "The link no longer opens the note." });
      close();
    },
    onError: (e) => fail("Couldn't stop sharing", e),
  });

  const header = (
    <DialogHeader className="pr-12">
      <DialogTitle className="flex items-center gap-2">
        <Globe className="size-4 text-faint" strokeWidth={1.75} aria-hidden />
        <span className="truncate">Share {title}</span>
      </DialogTitle>
      <DialogDescription>
        {share
          ? `${share.password ? "Anyone with the link and the password" : "Anyone with the link"} can read this note. The page shows your latest edits.`
          : "Publish this note as a web page that anyone with the link can read. It stays up to date as you edit."}
      </DialogDescription>
    </DialogHeader>
  );

  if (!client.shares || shares.isError) {
    const err = shares.error;
    const noSync = !client.shares || /needs sync/i.test(String(err));
    return (
      <>
        {header}
        <div className="px-5 pt-4 pb-5">
          <p className="text-ui text-foreground">
            {noSync ? "Sharing comes with sync" : "Couldn't load your shared notes"}
          </p>
          <p className="mt-1 text-small text-muted-foreground">
            {noSync
              ? "Shared notes are pages of your sync server. Sign in to sync to share notes."
              : err instanceof Error
                ? err.message
                : String(err)}
          </p>
          {noSync && client.account ? (
            <Button
              variant="secondary"
              className="mt-4"
              onClick={() => {
                close();
                useUI.getState().navigate({ kind: "settings" });
              }}
            >
              Open Settings
            </Button>
          ) : null}
        </div>
      </>
    );
  }

  if (shares.isPending) {
    return (
      <>
        {header}
        <div className="grid h-32 place-items-center text-faint">
          <LoaderCircle className="size-5 animate-spin" aria-label="Loading" />
        </div>
      </>
    );
  }

  if (!share) return <NotShared header={header} pending={put.isPending} onShare={put.mutate} />;

  return (
    <>
      {header}
      <div className="flex flex-col gap-4 px-5 pt-4 pb-5">
        <LinkRow url={share.url} />
        <PasswordRow
          share={share}
          pending={put.isPending}
          onSave={(password) =>
            put.mutate(
              { password },
              {
                onSuccess: () =>
                  toast(password ? "Password set" : "Password removed", {
                    description: password
                      ? "Readers need it to open the note."
                      : "Anyone with the link can read the note.",
                  }),
              },
            )
          }
        />
        <ThemeRow
          share={share}
          pending={put.isPending}
          onUpdate={(theme) => put.mutate({ theme })}
        />
        <div className="flex items-center justify-between gap-2 border-t border-divider pt-4">
          <Button
            variant="ghost"
            className="text-danger hover:text-danger"
            disabled={remove.isPending}
            onClick={() => remove.mutate()}
          >
            Stop sharing
          </Button>
          <Button variant="outline" onClick={() => openExternal(share.url)}>
            <ExternalLink /> Open page
          </Button>
        </div>
      </div>
    </>
  );
}

function NotShared({
  header,
  pending,
  onShare,
}: {
  header: ReactNode;
  pending: boolean;
  onShare: (req: ShareRequest) => void;
}) {
  const [password, setPassword] = useState("");
  const submit = (e: FormEvent) => {
    e.preventDefault();
    onShare({ password: password || undefined, theme: currentShareTheme() });
  };
  return (
    <>
      {header}
      <form onSubmit={submit} className="flex flex-col gap-4 px-5 pt-4 pb-5">
        <label className="flex flex-col gap-1.5">
          <span className="text-small font-medium text-foreground">
            Password <span className="font-normal text-faint">(optional)</span>
          </span>
          <Input
            type="password"
            autoComplete="new-password"
            placeholder="Anyone with the link can read it"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        <p className="text-small text-muted-foreground">
          Images in the note are shown. Links to other notes are plain text unless those are shared
          too. The page uses your current theme.
        </p>
        <div className="flex justify-end">
          <Button type="submit" disabled={pending}>
            {pending ? <LoaderCircle className="animate-spin" /> : <Globe />} Create link
          </Button>
        </div>
      </form>
    </>
  );
}

function LinkRow({ url }: { url: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch (e) {
      fail("Couldn't copy the link", e);
    }
  };
  return (
    <div className="flex gap-2">
      <Input
        readOnly
        aria-label="Link"
        value={url}
        onFocus={(e) => e.currentTarget.select()}
        className="font-mono text-[12.5px]"
      />
      <Button onClick={() => void copy()} className="w-[92px]">
        {copied ? <Check /> : <Copy />} {copied ? "Copied" : "Copy"}
      </Button>
    </div>
  );
}

function PasswordRow({
  share,
  pending,
  onSave,
}: {
  share: Share;
  pending: boolean;
  onSave: (password: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState("");
  if (editing)
    return (
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (!value) return;
          onSave(value);
          setEditing(false);
          setValue("");
        }}
      >
        <Input
          type="password"
          autoFocus
          autoComplete="new-password"
          aria-label="New password"
          placeholder="New password"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.stopPropagation();
              setEditing(false);
            }
          }}
        />
        <Button type="submit" variant="secondary" disabled={!value || pending}>
          Save
        </Button>
      </form>
    );
  return (
    <Row
      icon={<Lock className="size-4" strokeWidth={1.75} />}
      title={share.password ? "Password protected" : "No password"}
      detail={
        share.password ? "Readers enter it to open the note." : "The link is enough to read it."
      }
    >
      {share.password ? (
        <>
          <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
            Change
          </Button>
          <Button size="sm" variant="ghost" disabled={pending} onClick={() => onSave("")}>
            Remove
          </Button>
        </>
      ) : (
        <Button size="sm" variant="secondary" onClick={() => setEditing(true)}>
          Add password
        </Button>
      )}
    </Row>
  );
}

function ThemeRow({
  share,
  pending,
  onUpdate,
}: {
  share: Share;
  pending: boolean;
  onUpdate: (theme: ReturnType<typeof currentShareTheme>) => void;
}) {
  const now = currentShareTheme();
  const same = sameTheme(share.theme, now);
  const mode = share.theme?.mode === "dark" ? "dark" : share.theme?.mode === "light" ? "light" : "";
  return (
    <Row
      icon={<Palette className="size-4" strokeWidth={1.75} />}
      title={same ? "Your current theme" : "An earlier theme"}
      detail={
        same
          ? `Readers see the note the way you do${mode ? `, in ${mode} mode` : ""}.`
          : "You have changed theme since sharing."
      }
    >
      {same ? null : (
        <Button size="sm" variant="secondary" disabled={pending} onClick={() => onUpdate(now)}>
          Use current
        </Button>
      )}
    </Row>
  );
}

function Row({
  icon,
  title,
  detail,
  children,
}: {
  icon: ReactNode;
  title: string;
  detail: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex items-center gap-3">
      <span className="grid size-8 shrink-0 place-items-center rounded-md bg-chip text-muted-foreground">
        {icon}
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="text-ui text-foreground">{title}</span>
        <span className="text-small text-muted-foreground">{detail}</span>
      </span>
      {children ? <span className="flex shrink-0 gap-1">{children}</span> : null}
    </div>
  );
}
