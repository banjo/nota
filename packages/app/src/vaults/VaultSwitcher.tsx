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
import { ChevronsUpDown, PenLine, Plus, Settings2 } from "lucide-react";
import { useClient } from "../context.ts";
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
