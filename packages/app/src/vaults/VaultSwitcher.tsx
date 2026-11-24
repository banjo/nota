import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@nota/ui/dropdown-menu";
import { Sheet, SheetContent, SheetItem, SheetSeparator, SheetTrigger } from "@nota/ui/sheet";
import { Check, ChevronsUpDown, PenLine, Plus, Settings2 } from "lucide-react";
import { useState } from "react";
import { useClient } from "../context.ts";
import { useNarrow } from "../lib/viewport.ts";
import { useUI } from "../state.ts";
import { onlineVaults, switchVault, useVaultDialog } from "./online.ts";
import { useVaults } from "./useVaults.ts";

/**
 * The web app's vault switcher: a quiet chevron in the sidebar header
 * (a full-size touch target on phones, which have no ⌘K). Nothing on the
 * desktop, whose vault is a folder.
 */
export function VaultSwitcher() {
  const client = useClient();
  if (!onlineVaults(client)) return null;
  return <Switcher />;
}

function Switcher() {
  const client = useClient();
  const { current, list } = useVaults();
  const open = useVaultDialog((s) => s.open);
  const navigate = useUI((s) => s.navigate);
  const narrow = useNarrow();
  if (narrow) return <SwitcherSheet />;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label="Switch vault"
        className="-mr-1.5 ml-auto grid size-7 shrink-0 place-items-center rounded-sm text-faint outline-none hover:bg-hover hover:text-foreground focus-visible:bg-hover data-[state=open]:bg-hover data-[state=open]:text-foreground pointer-coarse:size-11"
      >
        <ChevronsUpDown className="size-[15px]" strokeWidth={1.75} />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-60">
        <DropdownMenuLabel>Vaults</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={current} onValueChange={(v) => switchVault(client, v)}>
          {(list.data ?? [{ name: current }]).map((v) => (
            <DropdownMenuRadioItem key={v.name} value={v.name} className="pointer-coarse:h-11">
              <span className="truncate">{v.name}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem className="pointer-coarse:h-11" onSelect={() => open({ kind: "create" })}>
          <Plus /> New vault…
        </DropdownMenuItem>
        <DropdownMenuItem
          className="pointer-coarse:h-11"
          onSelect={() => open({ kind: "rename", name: current })}
        >
          <PenLine /> <span className="truncate">Rename {current}…</span>
        </DropdownMenuItem>
        <DropdownMenuItem
          className="pointer-coarse:h-11"
          onSelect={() => navigate({ kind: "settings" })}
        >
          <Settings2 /> Manage vaults…
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

const trigger =
  "-mr-1.5 ml-auto grid size-11 shrink-0 place-items-center rounded-sm text-faint outline-none active:bg-hover data-[state=open]:bg-hover data-[state=open]:text-foreground";

/** The same switcher on a phone: a sheet with the vaults and what to do with them. */
function SwitcherSheet() {
  const client = useClient();
  const { current, list } = useVaults();
  const open = useVaultDialog((s) => s.open);
  const navigate = useUI((s) => s.navigate);
  const [shown, setShown] = useState(false);
  const then = (fn: () => void) => () => {
    setShown(false);
    fn();
  };
  return (
    <Sheet open={shown} onOpenChange={setShown}>
      <SheetTrigger aria-label="Switch vault" className={trigger}>
        <ChevronsUpDown className="size-[18px]" strokeWidth={1.75} />
      </SheetTrigger>
      <SheetContent
        title="Vaults"
        onOpenAutoFocus={(e) => e.preventDefault()}
        onCloseAutoFocus={(e) => e.preventDefault()}
      >
        <div role="radiogroup" aria-label="Vaults">
          {(list.data ?? [{ name: current }]).map((v) => {
            const on = v.name === current;
            return (
              <SheetItem
                key={v.name}
                role="radio"
                aria-checked={on}
                onClick={then(() => (on ? undefined : switchVault(client, v.name)))}
              >
                <span className="min-w-0 flex-1 truncate">{v.name}</span>
                {on ? <Check className="!text-ink" strokeWidth={2} /> : null}
              </SheetItem>
            );
          })}
        </div>
        <SheetSeparator />
        <SheetItem onClick={then(() => open({ kind: "create" }))}>
          <Plus strokeWidth={1.75} /> New vault…
        </SheetItem>
        <SheetItem onClick={then(() => open({ kind: "rename", name: current }))}>
          <PenLine strokeWidth={1.75} /> <span className="truncate">Rename {current}…</span>
        </SheetItem>
        <SheetItem onClick={then(() => navigate({ kind: "settings" }))}>
          <Settings2 strokeWidth={1.75} /> Manage vaults…
        </SheetItem>
      </SheetContent>
    </Sheet>
  );
}
