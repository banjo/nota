import { Kbd } from "@nota/ui/kbd";
import { MarkTile } from "@nota/ui/logo";
import { ScrollArea } from "@nota/ui/scroll-area";
import { useQueryClient } from "@tanstack/react-query";
import { FolderPlus, Plus, Search, Settings } from "lucide-react";
import { useState } from "react";
import { createNote, newNoteFolder } from "../actions.ts";
import { useClient } from "../context.ts";
import { label } from "../lib/shortcuts.ts";
import { useInboxName, useVaultInfo } from "../queries.ts";
import { useContributions } from "../registry.ts";
import { useUI } from "../state.ts";
import { SidebarScroll } from "./FolderTree.tsx";
import { SidebarContextMenu } from "./RowMenu.tsx";
import { IconButton, startNewFolder } from "./NewFolder.tsx";
import { SyncStatusRow } from "./SyncStatus.tsx";
import { VaultSwitcher } from "../vaults/VaultSwitcher.tsx";

/** The Träd sidebar: workspace, search, the registered sections, new note, sync. */
export function Sidebar() {
  const info = useVaultInfo();
  const sections = useContributions("sidebar");
  const openPalette = useUI((s) => s.openPalette);
  const navigate = useUI((s) => s.navigate);
  const client = useClient();
  const qc = useQueryClient();
  const inbox = useInboxName();
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  const name = info.data?.name ?? "Notes";

  return (
    <aside
      className="flex h-full min-h-0 flex-col bg-sidebar pt-[calc(var(--mygo-titlebar-controls-bottom,0px)+10px+var(--safe-top))]"
      aria-label="Sidebar"
    >
      <div className="flex items-center gap-[9px] px-4 pt-1.5 pb-2.5 pointer-coarse:pt-3">
        <MarkTile size={22} />
        <span className="truncate text-[13.5px] font-semibold text-foreground">{name}</span>
        <VaultSwitcher />
      </div>
      <div className="px-3 pb-3">
        <button
          type="button"
          onClick={() => openPalette()}
          className="flex h-[30px] w-full items-center gap-2 rounded-md bg-surface px-[9px] text-faint shadow-card outline-none hover:text-muted-foreground focus-visible:shadow-[0_0_0_1px_var(--ink),0_0_0_4px_var(--ink-soft)] pointer-coarse:h-11"
        >
          <Search className="size-3.5" strokeWidth={1.75} />
          <span className="text-ui pointer-coarse:text-[15px]">Search</span>
          <Kbd className="ml-auto pointer-coarse:hidden">{label("mod+k")}</Kbd>
        </button>
      </div>
      <SidebarContextMenu>
        <ScrollArea
          className="min-h-0 flex-1"
          viewportRef={setScrollEl}
          viewportClassName="overscroll-contain"
        >
          <SidebarScroll.Provider value={scrollEl}>
            <nav className="flex flex-col px-2 pb-3">
              {sections.map((s) => (
                <s.component key={s.id} />
              ))}
            </nav>
          </SidebarScroll.Provider>
        </ScrollArea>
      </SidebarContextMenu>
      <div className="flex flex-col gap-0.5 border-t border-divider px-2 pt-1.5 pb-[max(8px,var(--safe-bottom))]">
        <div className="flex items-center gap-0.5">
          <button
            type="button"
            onClick={() =>
              void createNote(client, qc, newNoteFolder(useUI.getState().route, inbox))
            }
            className="flex h-8 min-w-0 flex-1 items-center gap-2 rounded-sm px-2 text-ui text-muted-foreground outline-none hover:bg-hover hover:text-foreground focus-visible:bg-hover active:bg-hover pointer-coarse:h-11 pointer-coarse:text-[15px]"
          >
            <Plus className="size-[15px]" strokeWidth={1.75} />
            New note
            <Kbd className="ml-auto pointer-coarse:hidden">{label("mod+n")}</Kbd>
          </button>
          <IconButton
            label="New folder"
            onClick={() => startNewFolder("")}
            className="size-8 pointer-coarse:size-11"
          >
            <FolderPlus strokeWidth={1.75} />
          </IconButton>
        </div>
        <div className="flex items-center">
          <SyncStatusRow />
          <button
            type="button"
            aria-label="Settings"
            onClick={() => navigate({ kind: "settings" })}
            className="grid size-7 shrink-0 place-items-center rounded-sm text-faint outline-none hover:bg-hover hover:text-foreground focus-visible:bg-hover pointer-coarse:size-11"
          >
            <Settings className="size-[15px]" strokeWidth={1.75} />
          </button>
        </div>
      </div>
    </aside>
  );
}
