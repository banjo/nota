import { cn } from "@nota/ui/utils";
import { ChevronRight } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

// A sidebar row: 28px on a desk, 44px under a finger (decision 10).

export function Row({
  icon,
  label,
  count,
  active,
  depth = 0,
  chevron,
  chevronEnd,
  trailing,
  className,
  muted,
  ...props
}: Omit<ComponentProps<"button">, "children"> & {
  icon?: ReactNode;
  label: ReactNode;
  count?: ReactNode;
  active?: boolean;
  depth?: number;
  /** undefined: no chevron; true/false: expanded or not. */
  chevron?: boolean;
  /** Put the chevron after the label (rows that line up with chevron-less ones). */
  chevronEnd?: boolean;
  trailing?: ReactNode;
  muted?: boolean;
}) {
  return (
    <button
      type="button"
      aria-current={active ? "page" : undefined}
      aria-expanded={chevron}
      style={{ paddingLeft: 8 + depth * 16 }}
      className={cn(
        "group/row flex h-7 w-full min-w-0 items-center gap-[7px] rounded-sm pr-2 text-left text-ui text-text-soft outline-none select-none",
        "hover:bg-hover focus-visible:bg-hover focus-visible:outline-none pointer-coarse:h-11 pointer-coarse:text-[15px]",
        active && "bg-selected font-medium text-foreground hover:bg-selected",
        muted && !active && "text-file",
        className,
      )}
      {...props}
    >
      {chevron !== undefined && !chevronEnd ? (
        <span className="grid size-4 shrink-0 place-items-center text-faint">
          <ChevronRight
            className={cn(
              "size-3 transition-transform duration-150 ease-out",
              chevron && "rotate-90",
            )}
            strokeWidth={2}
          />
        </span>
      ) : null}
      {icon ? (
        <span className="grid shrink-0 place-items-center text-muted-foreground [&_svg]:size-[15px]">
          {icon}
        </span>
      ) : null}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {trailing}
      {chevron !== undefined && chevronEnd ? (
        <ChevronRight
          className={cn(
            "size-3 shrink-0 text-faint transition-transform duration-150 ease-out",
            chevron && "rotate-90",
          )}
          strokeWidth={2}
        />
      ) : null}
      {count !== undefined && count !== null && count !== 0 ? (
        <span className="shrink-0 text-small text-faint tabular-nums">{count}</span>
      ) : null}
    </button>
  );
}

export function SectionHeader({
  children,
  onClick,
  open,
}: {
  children: ReactNode;
  onClick?: () => void;
  open?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-expanded={open}
      className="group/sec flex w-full items-center gap-1 px-2 pt-4 pb-1 text-left text-micro font-semibold text-faint outline-none hover:text-muted-foreground focus-visible:text-foreground pointer-coarse:pt-5 pointer-coarse:pb-2"
    >
      {children}
      {open !== undefined ? (
        <ChevronRight
          className={cn(
            "size-3 opacity-0 transition-[opacity,transform] duration-150 ease-out group-hover/sec:opacity-100 group-focus-visible/sec:opacity-100 pointer-coarse:opacity-100",
            open && "rotate-90",
          )}
          strokeWidth={2}
        />
      ) : null}
    </button>
  );
}
