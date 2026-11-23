import type { VaultSummary } from "@nota/client";
import { Button } from "@nota/ui/button";
import { cn } from "@nota/ui/utils";
import { Check, Cloud, LoaderCircle, PenLine, Plus, Trash2 } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useClient } from "../context.ts";
import { switchVault, vaultNameProblem } from "./online.ts";
import { DeleteConfirm, VaultNameInput } from "./VaultDialogs.tsx";
import { useVaults } from "./useVaults.ts";

type Editing = { kind: "rename" | "delete"; name: string } | null;

function RenameRow({
  vault,
  taken,
  busy,
  onSave,
  onCancel,
}: {
  vault: VaultSummary;
  taken: string[];
  busy: boolean;
  onSave: (to: string) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(vault.name);
  const trimmed = name.trim();
  const problem = vaultNameProblem(trimmed, taken, vault.name);
  return (
    <form
      className="flex items-start gap-2 px-3.5 py-2.5 max-[520px]:flex-col max-[520px]:items-stretch"
      onSubmit={(e) => {
        e.preventDefault();
        if (!problem && trimmed !== vault.name) onSave(trimmed);
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") onCancel();
      }}
    >
      <VaultNameInput
        label={`New name for ${vault.name}`}
        value={name}
        onChange={setName}
        problem={problem}
        autoFocus
        className="flex-1"
      />
      <div className="flex gap-2 max-[520px]:justify-end">
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={!!problem || trimmed === vault.name || busy}>
          {busy ? <LoaderCircle className="animate-spin motion-reduce:animate-none" /> : null}
          Save
        </Button>
      </div>
    </form>
  );
}

function IconAction({
  label,
  onClick,
  children,
  danger,
}: {
  label: string;
  onClick: () => void;
  children: ReactNode;
  danger?: boolean;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className={cn(
        "grid size-7 shrink-0 place-items-center rounded-sm text-faint outline-none hover:bg-hover focus-visible:bg-hover pointer-coarse:size-11 [&_svg]:size-[15px]",
        danger ? "hover:text-danger" : "hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

/**
 * Settings › Vaults on the web: the account's vaults on the server,
 * the open one marked, with rename (inline), switch, delete (typed
 * confirmation) and a new vault. The desktop keeps its folder Vault
 * section and setup wizard.
 */
export function OnlineVaults() {
  const client = useClient();
  const { current, list, create, rename, remove } = useVaults();
  const [editing, setEditing] = useState<Editing>(null);
  const [name, setName] = useState("");
  const vaults = list.data ?? [];
  const taken = vaults.map((v) => v.name);
  const trimmed = name.trim();
  const problem = vaultNameProblem(trimmed, taken);

  return (
    <section className="py-5 [section+&]:border-t [section+&]:border-divider">
      <h2 className="text-[14px] font-semibold text-foreground">Vaults</h2>
      <p className="mt-1 text-ui text-muted-foreground">
        Your vaults on the server. This browser has one open at a time; the desktop app and the CLI
        can sync with any of them.
      </p>
      <div className="mt-4 flex flex-col gap-3">
        {list.isPending ? (
          <p className="flex items-center gap-2 text-ui text-muted-foreground">
            <LoaderCircle className="size-4 animate-spin motion-reduce:animate-none" /> Loading your
            vaults…
          </p>
        ) : list.isError ? (
          <p className="text-ui text-muted-foreground">
            Couldn't list your vaults: {String(list.error)}{" "}
            <button
              type="button"
              className="text-ink underline-offset-2 hover:underline"
              onClick={() => void list.refetch()}
            >
              Try again
            </button>
          </p>
        ) : (
          <ul
            aria-label="Your vaults"
            className="overflow-hidden rounded-lg border border-hairline"
          >
            {vaults.map((v) => {
              const on = v.name === current;
              const edit = editing?.name === v.name ? editing.kind : null;
              return (
                <li
                  key={v.name}
                  aria-current={on ? "true" : undefined}
                  className="border-t border-divider first:border-t-0"
                >
                  {edit === "rename" ? (
                    <RenameRow
                      vault={v}
                      taken={taken}
                      busy={rename.isPending}
                      onCancel={() => setEditing(null)}
                      onSave={(to) =>
                        rename.mutate({ name: v.name, to }, { onSuccess: () => setEditing(null) })
                      }
                    />
                  ) : (
                    <div className="flex items-center gap-2.5 py-1.5 pr-2 pl-3.5 text-ui pointer-coarse:py-1">
                      <Cloud className="size-4 shrink-0 text-faint" strokeWidth={1.75} />
                      <span className="min-w-0 flex-1 truncate text-foreground">{v.name}</span>
                      {on ? (
                        <span className="flex items-center gap-1 px-1 text-small text-muted-foreground">
                          <Check className="size-3.5 text-ink" strokeWidth={2.25} /> Open
                        </span>
                      ) : (
                        <Button
                          size="sm"
                          variant="secondary"
                          className="pointer-coarse:h-9"
                          onClick={() => switchVault(client, v.name)}
                        >
                          Open
                        </Button>
                      )}
                      <IconAction
                        label={`Rename ${v.name}`}
                        onClick={() => setEditing({ kind: "rename", name: v.name })}
                      >
                        <PenLine strokeWidth={1.75} />
                      </IconAction>
                      <IconAction
                        label={`Delete ${v.name}`}
                        danger
                        onClick={() => setEditing({ kind: "delete", name: v.name })}
                      >
                        <Trash2 strokeWidth={1.75} />
                      </IconAction>
                    </div>
                  )}
                  {edit === "delete" ? (
                    <div className="border-t border-divider bg-sidebar px-3.5 py-3">
                      <DeleteConfirm
                        name={v.name}
                        busy={remove.isPending}
                        autoFocus
                        onCancel={() => setEditing(null)}
                        onDelete={(confirm) =>
                          remove.mutate(
                            { name: v.name, confirm },
                            { onSuccess: () => setEditing(null) },
                          )
                        }
                      />
                    </div>
                  ) : null}
                </li>
              );
            })}
          </ul>
        )}
        <form
          className="flex items-start gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (!problem) create.mutate({ name: trimmed }, { onSuccess: () => setName("") });
          }}
        >
          <VaultNameInput
            label="New vault name"
            placeholder="New vault name"
            value={name}
            onChange={setName}
            problem={problem}
            className="max-w-60 flex-1"
          />
          <Button
            type="submit"
            variant="outline"
            className="pointer-coarse:h-11"
            disabled={!!problem || create.isPending}
          >
            <Plus /> New vault
          </Button>
        </form>
        <p className="text-small text-faint">
          Renaming keeps a vault's notes, history and synced devices: the old name still leads to
          it.
        </p>
      </div>
    </section>
  );
}
