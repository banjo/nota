import type { VaultSummary } from "@nota/client";
import { Button } from "@nota/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@nota/ui/dialog";
import { Input } from "@nota/ui/input";
import { cn } from "@nota/ui/utils";
import { Check, LoaderCircle, Plus } from "lucide-react";
import { type FormEvent, type ReactNode, useState } from "react";
import { useClient } from "../context.ts";
import {
  deleteConfirmed,
  onlineVaults,
  switchVault,
  useVaultDialog,
  type VaultDialog,
  vaultNameProblem,
} from "./online.ts";
import { useVaults } from "./useVaults.ts";

/** A vault name input with its rule shown once something is typed. */
export function VaultNameInput({
  value,
  onChange,
  problem,
  label,
  placeholder,
  autoFocus,
  className,
}: {
  value: string;
  onChange: (v: string) => void;
  problem: string | null;
  label: string;
  placeholder?: string;
  autoFocus?: boolean;
  className?: string;
}) {
  const show = value !== "" && problem;
  return (
    <div className={cn("min-w-0", className)}>
      <Input
        aria-label={label}
        aria-invalid={show ? true : undefined}
        value={value}
        placeholder={placeholder}
        autoFocus={autoFocus}
        spellCheck={false}
        autoCapitalize="off"
        autoComplete="off"
        onChange={(e) => onChange(e.target.value)}
        className="pointer-coarse:h-11"
      />
      {show ? <p className="mt-1.5 text-small text-danger">{problem}</p> : null}
    </div>
  );
}

/** The typed confirmation of a delete, with what goes. */
export function DeleteConfirm({
  name,
  busy,
  onDelete,
  onCancel,
  autoFocus,
}: {
  name: string;
  busy: boolean;
  onDelete: (typed: string) => void;
  onCancel?: () => void;
  autoFocus?: boolean;
}) {
  const [typed, setTyped] = useState("");
  const ok = deleteConfirmed(typed, name);
  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        if (ok) onDelete(typed);
      }}
    >
      <p className="text-ui text-text-soft">
        This deletes <b className="font-semibold text-foreground">{name}</b> from the server: every
        note, its whole history and the vault's settings. Devices that sync with it keep their own
        files, but stop syncing. This can't be undone.
      </p>
      <label className="block">
        <span className="mb-1.5 block text-small text-muted-foreground">
          Type <b className="font-mono font-medium text-foreground">{name}</b> to confirm
        </span>
        <Input
          aria-label={`Type ${name} to confirm`}
          value={typed}
          autoFocus={autoFocus}
          spellCheck={false}
          autoCapitalize="off"
          autoComplete="off"
          onChange={(e) => setTyped(e.target.value)}
          className="pointer-coarse:h-11"
        />
      </label>
      <div className="flex justify-end gap-2">
        {onCancel ? (
          <Button type="button" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        ) : null}
        <Button type="submit" variant="destructive" disabled={!ok || busy}>
          {busy ? <LoaderCircle className="animate-spin motion-reduce:animate-none" /> : null}
          Delete vault
        </Button>
      </div>
    </form>
  );
}

function Frame({
  title,
  description,
  children,
}: {
  title: string;
  description?: ReactNode;
  children: ReactNode;
}) {
  return (
    <>
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        {description ? (
          <DialogDescription>{description}</DialogDescription>
        ) : (
          <DialogDescription className="sr-only">{title}</DialogDescription>
        )}
      </DialogHeader>
      <div className="px-5 pt-4 pb-5">{children}</div>
    </>
  );
}

function NameForm({
  initial,
  taken,
  self,
  submit,
  busy,
  onSubmit,
  onCancel,
}: {
  initial: string;
  taken: string[];
  self?: string;
  submit: string;
  busy: boolean;
  onSubmit: (name: string) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(initial);
  const trimmed = name.trim();
  const problem = vaultNameProblem(trimmed, taken, self);
  const send = (e: FormEvent) => {
    e.preventDefault();
    if (!problem && trimmed !== self) onSubmit(trimmed);
  };
  return (
    <form className="flex flex-col gap-4" onSubmit={send}>
      <VaultNameInput
        label="Vault name"
        value={name}
        onChange={setName}
        problem={problem}
        autoFocus
      />
      <DialogFooter className="px-0 pb-0">
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={!!problem || trimmed === self || busy}>
          {busy ? <LoaderCircle className="animate-spin motion-reduce:animate-none" /> : null}
          {submit}
        </Button>
      </DialogFooter>
    </form>
  );
}

function SwitchList({
  vaults,
  current,
  onNew,
}: {
  vaults: VaultSummary[];
  current: string;
  onNew: () => void;
}) {
  const client = useClient();
  return (
    <div className="flex flex-col gap-3">
      <ul className="overflow-hidden rounded-lg border border-hairline">
        {vaults.map((v) => {
          const on = v.name === current;
          return (
            <li key={v.name} className="border-t border-divider first:border-t-0">
              <button
                type="button"
                aria-current={on ? "true" : undefined}
                onClick={() => switchVault(client, v.name)}
                className="flex h-10 w-full items-center gap-2.5 px-3.5 text-left text-ui text-foreground outline-none hover:bg-hover focus-visible:bg-hover pointer-coarse:h-12"
              >
                <span className="min-w-0 flex-1 truncate">{v.name}</span>
                {on ? (
                  <span className="flex items-center gap-1 text-small text-muted-foreground">
                    <Check className="size-3.5 text-ink" strokeWidth={2.25} /> Open
                  </span>
                ) : null}
              </button>
            </li>
          );
        })}
      </ul>
      <Button variant="outline" className="self-start" onClick={onNew}>
        <Plus /> New vault…
      </Button>
    </div>
  );
}

function Body({ dialog, close }: { dialog: VaultDialog; close: () => void }) {
  const { current, list, create, rename, remove } = useVaults();
  const open = useVaultDialog((s) => s.open);
  const vaults = list.data ?? [];
  const taken = vaults.map((v) => v.name);
  switch (dialog.kind) {
    case "switch":
      return (
        <Frame title="Switch vault" description="The page reloads into the vault you pick.">
          {list.isPending ? (
            <p className="flex items-center gap-2 text-ui text-muted-foreground">
              <LoaderCircle className="size-4 animate-spin motion-reduce:animate-none" /> Loading
              your vaults…
            </p>
          ) : (
            <SwitchList vaults={vaults} current={current} onNew={() => open({ kind: "create" })} />
          )}
        </Frame>
      );
    case "create":
      return (
        <Frame
          title="New vault"
          description="An empty vault on the server, next to your others. It opens when it's made."
        >
          <NameForm
            initial=""
            taken={taken}
            submit="Create and open"
            busy={create.isPending}
            onSubmit={(name) => create.mutate({ name, open: true })}
            onCancel={close}
          />
        </Frame>
      );
    case "rename":
      return (
        <Frame
          title={`Rename ${dialog.name}`}
          description="Devices that sync with this vault keep working: the old name still leads to it."
        >
          <NameForm
            initial={dialog.name}
            taken={taken}
            self={dialog.name}
            submit="Rename"
            busy={rename.isPending}
            onSubmit={(to) => rename.mutate({ name: dialog.name, to }, { onSuccess: close })}
            onCancel={close}
          />
        </Frame>
      );
    case "delete":
      return (
        <Frame title={`Delete ${dialog.name}?`}>
          <DeleteConfirm
            name={dialog.name}
            busy={remove.isPending}
            autoFocus
            onDelete={(confirm) =>
              remove.mutate({ name: dialog.name, confirm }, { onSuccess: close })
            }
            onCancel={close}
          />
        </Frame>
      );
  }
}

/** The vault dialogs, mounted once in the shell; nothing on the desktop. */
export function VaultDialogs() {
  const client = useClient();
  const dialog = useVaultDialog((s) => s.dialog);
  const close = useVaultDialog((s) => s.close);
  if (!onlineVaults(client)) return null;
  return (
    <Dialog open={!!dialog} onOpenChange={(o) => (o ? null : close())}>
      <DialogContent className="max-w-[420px]" showClose>
        {dialog ? <Body key={dialog.kind} dialog={dialog} close={close} /> : null}
      </DialogContent>
    </Dialog>
  );
}
