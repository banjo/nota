import { cn } from "@nota/ui/utils";
import { ChevronLeft, PanelLeft, Search } from "lucide-react";
import type { ReactNode } from "react";
import { useUI } from "../state.ts";

/**
 * The bar above the page: breadcrumbs on the left, quiet details on the
 * right. On a narrow screen it leads with a back button to the sidebar
 * and ends with search (a phone has no ⌘K); with the sidebar hidden, it
 * leads with a button to bring it back.
 */
export function TopBar({
  crumbs,
  right,
}: {
  crumbs: { label: string; onClick?: () => void }[];
  right?: ReactNode;
}) {
  const sidebarOpen = useUI((s) => s.sidebarOpen);
  const toggleSidebar = useUI((s) => s.toggleSidebar);
  const back = useUI((s) => s.back);
  const openPalette = useUI((s) => s.openPalette);
  return (
    <header
      className="sticky top-0 z-10 flex h-11 shrink-0 items-center gap-1.5 bg-background/95 pr-[18px] pl-[18px] text-[12.5px] text-muted-foreground backdrop-blur-md [--app-region:drag] pointer-coarse:h-12 max-[720px]:pr-1 max-[720px]:pl-1 max-[720px]:text-[15px]"
      style={{ paddingTop: "var(--mygo-titlebar-inset-top, 0px)" }}
    >
      <button
        type="button"
        aria-label="Back to notes"
        onClick={back}
        className="hidden size-11 shrink-0 place-items-center rounded-sm text-ink outline-none [--app-region:no-drag] active:bg-hover max-[720px]:grid"
      >
        <ChevronLeft className="size-6" strokeWidth={2} />
      </button>
      {!sidebarOpen ? (
        <button
          type="button"
          aria-label="Show sidebar"
          onClick={toggleSidebar}
          className="-ml-1.5 mr-1 grid size-7 shrink-0 place-items-center rounded-sm text-faint outline-none [--app-region:no-drag] hover:bg-hover hover:text-foreground focus-visible:bg-hover max-[720px]:hidden"
        >
          <PanelLeft className="size-4" strokeWidth={1.75} />
        </button>
      ) : null}
      <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1.5">
        {crumbs.map((c, i) => {
          const last = i === crumbs.length - 1;
          return (
            <span
              key={`${c.label}:${i}`}
              className={cn(
                "flex min-w-0 items-center gap-1.5",
                !last && "shrink-[2] max-[720px]:hidden",
              )}
            >
              {c.onClick && !last ? (
                <button
                  type="button"
                  onClick={c.onClick}
                  className="truncate rounded-xs outline-none [--app-region:no-drag] hover:text-foreground focus-visible:text-foreground"
                >
                  {c.label}
                </button>
              ) : (
                <span
                  className={cn("truncate", last && "text-foreground")}
                  aria-current={last ? "page" : undefined}
                >
                  {c.label}
                </span>
              )}
              {!last ? <span className="text-[#c9cbd0] dark:text-faint">/</span> : null}
            </span>
          );
        })}
      </nav>
      <div className="ml-auto flex shrink-0 items-center gap-1 text-faint [--app-region:no-drag] max-[720px]:gap-0">
        {right}
        {/* Before the page's "…" (which takes order-last), after everything else. */}
        <button
          type="button"
          aria-label="Search"
          onClick={() => openPalette()}
          className="hidden size-11 shrink-0 place-items-center rounded-sm text-muted-foreground outline-none active:bg-hover max-[720px]:grid"
        >
          <Search className="size-5" strokeWidth={1.75} />
        </button>
      </div>
    </header>
  );
}
