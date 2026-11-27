import {
  ICON_COLORS,
  ICON_SETS,
  type IconColor,
  type IconLook,
  type IconMatch,
  type IconSet,
} from "@nota/client";
import { Button } from "@nota/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@nota/ui/dialog";
import { Input } from "@nota/ui/input";
import { cn } from "@nota/ui/utils";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { Folder, Layers } from "lucide-react";
import { type KeyboardEvent, useRef, useState } from "react";
import { create } from "zustand";
import { setIcon } from "../actions.ts";
import { useClient } from "../context.ts";
import { useDebounced } from "../lib/debounce.ts";
import { gridMove, iconColorVar, type IconTarget, lookOf } from "../lib/icons.ts";
import { useTreeValue, useViews } from "../queries.ts";
import { IconGlyph } from "./IconGlyph.tsx";

// The icon of a folder or a saved view: search the icon sets, pick a
// colour from the theme's. The folder's and the view's menus and ⌘K open it.

export const useIconPicker = create<{ target: IconTarget | null }>(() => ({ target: null }));
export const openIconPicker = (target: IconTarget) => useIconPicker.setState({ target });
const close = () => useIconPicker.setState({ target: null });

const coarse = () => typeof matchMedia !== "undefined" && matchMedia("(pointer: coarse)").matches;

const SETS: { id: IconSet | undefined; name: string }[] = [
  { id: undefined, name: "All" },
  ...ICON_SETS.map((s) => ({
    id: s.prefix,
    name: s.prefix === "simple-icons" ? "Brands" : s.name,
  })),
];

const COLOR_NAMES: Record<IconColor, string> = {
  accent: "Accent",
  red: "Red",
  orange: "Orange",
  yellow: "Yellow",
  green: "Green",
  blue: "Blue",
  purple: "Purple",
  pink: "Pink",
  gray: "Gray",
};

const knownColor = (c: string | undefined): IconColor | undefined =>
  (ICON_COLORS as readonly string[]).includes(c ?? "") ? (c as IconColor) : undefined;

export function IconPickerDialog() {
  const target = useIconPicker((s) => s.target);
  // Keep the content while the dialog closes.
  const [last, setLast] = useState(target);
  if (target && target !== last) setLast(target);
  return (
    <Dialog open={!!target} onOpenChange={(o) => !o && close()}>
      <DialogContent
        showClose
        aria-describedby={undefined}
        className="max-w-[440px]"
        // A phone's keyboard would cover the icons.
        onOpenAutoFocus={(e) => coarse() && e.preventDefault()}
      >
        {last ? (
          <PickerBody
            key={last.kind === "folder" ? `f:${last.path}` : `v:${last.id}`}
            target={last}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

/** What the folder or view looks like now. */
function useLook(target: IconTarget): IconLook {
  const folder = useTreeValue((t) =>
    target.kind === "folder" ? t.folders.find((f) => f.path === target.path) : undefined,
  );
  const views = useViews();
  const view = target.kind === "view" ? views.data?.find((v) => v.id === target.id) : undefined;
  const item = folder ?? view;
  return item ? lookOf(item) : {};
}

function PickerBody({ target }: { target: IconTarget }) {
  const client = useClient();
  const qc = useQueryClient();
  const look = useLook(target);
  const [query, setQuery] = useState("");
  const [set, setSet] = useState<IconSet | undefined>(undefined);
  const [color, setColor] = useState(knownColor(look.color));
  const [active, setActive] = useState(0);
  const grid = useRef<HTMLDivElement>(null);
  const q = useDebounced(query.trim(), 120);
  const icons = useQuery({
    queryKey: ["icons", q, set ?? ""],
    queryFn: () => client.searchIcons!(q, set),
    enabled: !!client.searchIcons,
    placeholderData: keepPreviousData,
    staleTime: Infinity,
  });
  const results = icons.data ?? [];
  const at = Math.min(active, Math.max(0, results.length - 1));

  const apply = async (icon: string | undefined, c: IconColor | undefined, done: boolean) => {
    const saved = await setIcon(client, qc, target, icon, c);
    if (saved && done) close();
  };
  const pick = (m: IconMatch) => void apply(m.id, color, true);
  const focusCell = (i: number) =>
    grid.current?.querySelectorAll<HTMLButtonElement>("[data-icon]")[i]?.focus();

  const onGridKey = (e: KeyboardEvent) => {
    const el = grid.current;
    if (!el) return;
    const cols = getComputedStyle(el).gridTemplateColumns.split(" ").filter(Boolean).length;
    const next = gridMove(at, e.key, results.length, cols);
    if (next === null) return;
    e.preventDefault();
    setActive(next);
    focusCell(next);
  };

  const fallback =
    target.kind === "folder" ? <Folder strokeWidth={1.75} /> : <Layers strokeWidth={1.75} />;

  return (
    <>
      <DialogHeader className="pr-12">
        <DialogTitle className="flex items-center gap-2">
          <IconGlyph
            look={look}
            fallback={fallback}
            className="text-faint [&_svg]:size-4"
            emojiClassName="size-4 text-[14px]"
          />
          <span className="truncate">Icon for {target.name}</span>
        </DialogTitle>
      </DialogHeader>
      <div className="flex flex-col gap-3 px-5 pt-4 pb-5">
        <Input
          value={query}
          placeholder="Search icons"
          aria-label="Search icons"
          spellCheck={false}
          autoComplete="off"
          onChange={(e) => {
            setQuery(e.target.value);
            setActive(0);
          }}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown" && results.length) {
              e.preventDefault();
              focusCell(at);
            } else if (e.key === "Enter" && !e.nativeEvent.isComposing && results[at]) {
              e.preventDefault();
              pick(results[at]);
            }
          }}
        />
        <div role="group" aria-label="Icon set" className="flex rounded-lg bg-hover p-0.5">
          {SETS.map((s) => {
            const on = s.id === set;
            return (
              <button
                key={s.name}
                type="button"
                aria-pressed={on}
                onClick={() => {
                  setSet(s.id);
                  setActive(0);
                }}
                className={cn(
                  "h-7 flex-1 rounded-md text-small outline-none select-none focus-visible:ring-2 focus-visible:ring-ink/40 pointer-coarse:h-9 pointer-coarse:text-[14px]",
                  on
                    ? "bg-background text-foreground shadow-xs"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {s.name}
              </button>
            );
          })}
        </div>
        <div className="h-[244px] overflow-y-auto overscroll-contain max-[720px]:h-[min(244px,36dvh)]">
          {icons.isError ? (
            <p className="px-1 py-2 text-small text-danger">
              {icons.error instanceof Error ? icons.error.message : String(icons.error)}
            </p>
          ) : icons.data && !results.length ? (
            <p className="px-1 py-2 text-small text-faint">No icons match “{q}”</p>
          ) : (
            <div
              ref={grid}
              role="group"
              aria-label="Icons"
              aria-busy={icons.isFetching || undefined}
              onKeyDown={onGridKey}
              className="grid grid-cols-[repeat(auto-fill,minmax(36px,1fr))] gap-0.5 pointer-coarse:grid-cols-[repeat(auto-fill,minmax(44px,1fr))]"
            >
              {results.map((m, i) => (
                <button
                  key={m.id}
                  type="button"
                  data-icon
                  title={m.name}
                  aria-label={m.name}
                  aria-pressed={m.id === look.icon}
                  tabIndex={i === at ? 0 : -1}
                  onFocus={() => setActive(i)}
                  onClick={() => pick(m)}
                  className={cn(
                    "grid aspect-square place-items-center rounded-md text-text-soft outline-none hover:bg-hover focus-visible:bg-hover focus-visible:ring-2 focus-visible:ring-ink/40 active:bg-hover [&_svg]:size-5",
                    m.id === look.icon && "bg-selected",
                  )}
                >
                  <IconGlyph
                    look={{ icon: m.id, svg: m.svg, color }}
                    emojiClassName="size-5 text-[18px]"
                  />
                </button>
              ))}
            </div>
          )}
        </div>
        <div className="flex items-center gap-3">
          <div role="radiogroup" aria-label="Colour" className="flex flex-1 flex-wrap gap-1.5">
            {[undefined, ...ICON_COLORS].map((c) => {
              const on = c === color;
              return (
                <button
                  key={c ?? "none"}
                  type="button"
                  role="radio"
                  aria-checked={on}
                  aria-label={c ? COLOR_NAMES[c] : "No colour"}
                  title={c ? COLOR_NAMES[c] : "No colour"}
                  onClick={() => {
                    setColor(c);
                    void apply(look.icon, c, false);
                  }}
                  className={cn(
                    "relative size-5 shrink-0 overflow-hidden rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ink/40 focus-visible:ring-offset-2 focus-visible:ring-offset-surface pointer-coarse:size-8",
                    !c && "shadow-[inset_0_0_0_1.5px_var(--hairline)]",
                    on && "shadow-[0_0_0_2px_var(--surface),0_0_0_3.5px_var(--foreground)]",
                  )}
                  style={c ? { background: iconColorVar(c) } : undefined}
                >
                  {c ? null : (
                    // A slash through the empty swatch: no colour.
                    <span
                      aria-hidden
                      className="absolute top-1/2 left-1/2 h-px w-[140%] -translate-x-1/2 -translate-y-1/2 -rotate-45 bg-faint"
                    />
                  )}
                </button>
              );
            })}
          </div>
          {look.icon ? (
            <Button variant="ghost" size="sm" onClick={() => void apply(undefined, color, true)}>
              Remove icon
            </Button>
          ) : null}
        </div>
      </div>
    </>
  );
}
