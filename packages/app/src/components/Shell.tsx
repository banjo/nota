import { Button } from "@nota/ui/button";
import { Kbd } from "@nota/ui/kbd";
import { cn } from "@nota/ui/utils";
import { useQueryClient } from "@tanstack/react-query";
import { NotebookPen } from "lucide-react";
import { type ReactNode, useEffect, useRef } from "react";
import { useClient } from "../context.ts";
import { label, matches } from "../lib/shortcuts.ts";
import { type Command, type CommandContext, useRegistry } from "../registry.ts";
import { currentPath, type Route, useUI } from "../state.ts";
import { useList } from "../queries.ts";
import { EmptyState } from "./EmptyState.tsx";
import { ConfirmDialog, ExternalPage } from "./ExternalPage.tsx";
import { InboxView } from "./Inbox.tsx";
import { NoteList } from "./NoteList.tsx";
import { NotePage } from "./NotePage.tsx";
import { OpenedFiles } from "./OpenedFiles.tsx";
import { Onboarding, preloadScreens, Settings } from "./lazy.tsx";
import { Palette } from "./Palette.tsx";
import { VaultDialogs } from "../vaults/VaultDialogs.tsx";
import { HistoryDialog } from "./History.tsx";
import { RowSheet } from "./RowSheet.tsx";
import { Sidebar } from "./Sidebar.tsx";
import { ReplaceView } from "./Replace.tsx";
import { TrashView } from "./Trash.tsx";
import { TopBar } from "./TopBar.tsx";

const enabled = (cmd: Command, ctx: CommandContext) =>
  !cmd.when || cmd.id === "palette.toggle" || cmd.when(ctx);

/** Runs registered commands from shortcuts and the app menu. */
export function useCommandRunner() {
  const registry = useRegistry();
  const client = useClient();
  const qc = useQueryClient();
  const last = useRef<{ id: string; at: number }>({ id: "", at: 0 });

  const run = (id: string) => {
    // The menu accelerator and the page may both see one key press.
    const now = performance.now();
    if (last.current.id === id && now - last.current.at < 300) return;
    last.current = { id, at: now };
    const cmd = registry.command(id);
    if (!cmd) return;
    const ctx = context();
    if (!enabled(cmd, ctx)) return;
    void cmd.run(ctx);
  };
  const context = (): CommandContext => {
    const ui = useUI.getState();
    return {
      client,
      queryClient: qc,
      route: ui.route,
      notePath: currentPath(ui.route),
      navigate: ui.navigate,
    };
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.isComposing) return;
      const cmds = registry.get("command").filter((c) => c.shortcut && matches(e, c.shortcut));
      if (!cmds.length) return;
      e.preventDefault();
      // Commands may share a shortcut (Pin note / Unpin note): the one that applies runs.
      const ctx = context();
      const cmd = cmds.find((c) => enabled(c, ctx));
      if (cmd) run(cmd.id);
    };
    window.addEventListener("keydown", onKey);
    const offMenu = client.shell?.onCommand(run);
    return () => {
      window.removeEventListener("keydown", onKey);
      offMenu?.();
    };
  });
  return run;
}

function Home({ run }: { run: (id: string) => void }) {
  return (
    <>
      <TopBar crumbs={[]} />
      <EmptyState
        icon={NotebookPen}
        title="Pick a note, or start a new one"
        body={
          <>
            <span className="pointer-coarse:hidden">
              Search everything with{" "}
              <Kbd className="text-ui text-muted-foreground">{label("mod+k")}</Kbd>.
            </span>
            <span className="hidden pointer-coarse:inline">
              Search everything from the top bar.
            </span>
          </>
        }
        action={
          <Button variant="secondary" onClick={() => run("note.new")}>
            New note{" "}
            <Kbd className="text-muted-foreground pointer-coarse:hidden">{label("mod+n")}</Kbd>
          </Button>
        }
      />
    </>
  );
}

function Page({ route, run }: { route: Route; run: (id: string) => void }) {
  switch (route.kind) {
    case "note":
      return (
        <div className="h-full overflow-y-auto overscroll-contain">
          <NotePage path={route.path} />
        </div>
      );
    case "external":
      return (
        <div className="h-full overflow-y-auto overscroll-contain">
          <ExternalPage key={route.id} id={route.id} />
        </div>
      );
    case "list":
      return <NoteList key={JSON.stringify(route.list)} list={route.list} />;
    case "inbox":
      return <InboxView />;
    case "trash":
      return <TrashView />;
    case "replace":
      return <ReplaceView />;
    case "settings":
      return <Settings />;
    default:
      return <Home run={run} />;
  }
}

/**
 * The Träd layout: sidebar and page. Below 720px it is a navigation
 * stack: the sidebar, then the page with a back button (decision 10).
 */

/**
 * A fresh window opens the most recently edited note rather than an
 * empty page, unless a file opened with the app takes the page: it waits
 * until those have been looked at.
 */
function OpenRecent() {
  const route = useUI((s) => s.route);
  const navigate = useUI((s) => s.navigate);
  const checked = useUI((s) => s.openChecked);
  const { data } = useList({ sort: "updated desc", limit: 1 }, route.kind === "home");
  const done = useRef(false);
  useEffect(() => {
    if (!checked) return;
    if (done.current || route.kind !== "home" || !data?.length) return;
    done.current = true;
    const newest = [...data].sort((a, b) => (b.updated ?? "").localeCompare(a.updated ?? ""))[0];
    if (newest) navigate({ kind: "note", path: newest.path });
  }, [data, route, navigate, checked]);
  return null;
}

export function Shell({ children }: { children?: ReactNode }) {
  useEffect(preloadScreens, []);
  const route = useUI((s) => s.route);
  const sidebarOpen = useUI((s) => s.sidebarOpen);
  const pane = useUI((s) => s.pane);
  const run = useCommandRunner();
  const vaultFlow = useUI((s) => s.vaultFlow);
  const closeVaultFlow = useUI((s) => s.closeVaultFlow);
  return (
    <div
      data-pane={pane}
      className={cn(
        "grid h-full grid-cols-[268px_minmax(0,1fr)] bg-background",
        !sidebarOpen && "grid-cols-[0_minmax(0,1fr)]",
        "max-[720px]:grid-cols-1",
      )}
    >
      <div
        className={cn(
          "min-h-0 overflow-hidden border-r border-hairline bg-sidebar pl-(--safe-left)",
          !sidebarOpen && "invisible border-r-0",
          "max-[720px]:visible max-[720px]:border-r-0 max-[720px]:pr-(--safe-right)",
          pane === "main" && "max-[720px]:hidden",
          // Back to the list: it slides in from where it went, the left.
          pane === "nav" && "max-[720px]:animate-slide-back",
        )}
      >
        <Sidebar />
      </div>
      <main
        className={cn(
          // The status bar's strip and landscape's notch side stay page-coloured.
          "flex min-h-0 min-w-0 flex-col overflow-hidden pt-(--safe-top) pr-(--safe-right)",
          "max-[720px]:pl-(--safe-left)",
          pane === "nav" && "max-[720px]:hidden",
          pane === "main" && "max-[720px]:animate-slide-in",
        )}
      >
        <Page route={route} run={run} />
      </main>
      <OpenedFiles />
      <ConfirmDialog />
      <OpenRecent />
      <Palette />
      <VaultDialogs />
      <RowSheet />
      <HistoryDialog />
      {vaultFlow ? (
        <div className="fixed inset-0 z-50 overflow-y-auto bg-background">
          <Onboarding flow="switch" initialMode={vaultFlow.mode} onCancel={closeVaultFlow} />
        </div>
      ) : null}
      {children}
    </div>
  );
}
