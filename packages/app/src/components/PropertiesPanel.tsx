import type { Note, PropertyPatch } from "@nota/client";
import { Button } from "@nota/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@nota/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@nota/ui/popover";
import { Sheet, SheetContent, SheetTrigger } from "@nota/ui/sheet";
import { toast } from "@nota/ui/sonner";
import { cn } from "@nota/ui/utils";
import { useQueryClient } from "@tanstack/react-query";
import {
  Calendar,
  CalendarClock,
  CheckSquare,
  Circle,
  Hash,
  List,
  type LucideIcon,
  Plus,
  Text,
  Trash2,
  X,
} from "lucide-react";
import { type ReactNode, useState } from "react";
import { fail } from "../actions.ts";
import { useClient } from "../context.ts";
import { formatDate, relativeTime } from "../lib/format.ts";
import { useNarrow } from "../lib/viewport.ts";
import {
  display,
  emptyValue,
  parseValue,
  type PropType,
  propType,
  SPECIAL_KEYS,
  STATUSES,
  statusColor,
  statusLabel,
  validKey,
} from "../lib/properties.ts";
import { keys, noteBase, noteWritten } from "../queries.ts";
import { useUI } from "../state.ts";

// Frontmatter as a properties panel: tags, status, dates, and every
// other key with an editor for its type. Edits change only their own
// lines in the file (internal/edit), checked against the note's version.

const TYPE_ICON: Record<PropType, LucideIcon> = {
  text: Text,
  number: Hash,
  bool: CheckSquare,
  date: Calendar,
  list: List,
};
const TYPE_LABEL: Record<PropType, string> = {
  text: "Text",
  number: "Number",
  bool: "Checkbox",
  date: "Date",
  list: "List",
};

function usePatch(note: Note) {
  const client = useClient();
  const qc = useQueryClient();
  return async (patch: PropertyPatch) => {
    try {
      const r = await client.patch(note.path, patch, noteBase(note.path));
      if (r.conflict) {
        toast.warning("The note changed somewhere else", {
          description: "It was reloaded; make the change again.",
        });
        void qc.invalidateQueries({ queryKey: keys.note(note.path) });
        return;
      }
      noteWritten(qc, r.note);
      void qc.invalidateQueries({ queryKey: keys.tree });
      void qc.invalidateQueries({ queryKey: keys.tags });
    } catch (e) {
      fail("Couldn't change the property", e);
    }
  };
}

/** A property's name. `still` rows (dates) hold nothing to tap: no 44px row for them. */
function Label({
  icon: Icon,
  children,
  still,
}: {
  icon: LucideIcon;
  children: ReactNode;
  still?: boolean;
}) {
  return (
    <dt
      className={cn(
        "flex h-7 min-w-0 items-center gap-1.5 text-faint",
        still ? "pointer-coarse:h-9" : "pointer-coarse:h-11",
      )}
    >
      <Icon className="size-3.5 shrink-0" strokeWidth={1.75} />
      <span className="truncate">{children}</span>
    </dt>
  );
}

const fieldClass =
  "h-7 w-full min-w-0 rounded-xs bg-transparent px-1.5 -mx-1.5 text-ui text-text-soft outline-none placeholder:text-faint hover:bg-hover focus:bg-surface focus:shadow-[0_0_0_1px_var(--ink),0_0_0_3px_var(--ink-soft)] pointer-coarse:h-11";

function Chip({
  children,
  onRemove,
  onClick,
  label,
}: {
  children: ReactNode;
  onRemove?: () => void;
  onClick?: () => void;
  label: string;
}) {
  return (
    <span className="inline-flex h-[22px] items-center rounded-xs bg-chip text-small text-chip-foreground pointer-coarse:h-9 pointer-coarse:text-[14px]">
      <button
        type="button"
        onClick={onClick}
        // Under a finger the hit area reaches past the chip to 44px.
        className="relative h-full rounded-xs px-2 outline-none hover:text-foreground focus-visible:text-foreground active:text-foreground pointer-coarse:px-2 pointer-coarse:after:absolute pointer-coarse:after:inset-x-0 pointer-coarse:after:-inset-y-1 pointer-coarse:after:content-['']"
      >
        {children}
      </button>
      {onRemove ? (
        <button
          type="button"
          aria-label={`Remove ${label}`}
          onClick={onRemove}
          className="-ml-1.5 grid h-full w-0 place-items-center overflow-hidden rounded-xs text-faint opacity-0 outline-none transition-[opacity,width] duration-150 ease-out group-hover/chip:w-5 group-hover/chip:opacity-100 hover:text-foreground focus-visible:w-5 focus-visible:text-foreground focus-visible:opacity-100 pointer-coarse:relative pointer-coarse:w-8 pointer-coarse:opacity-100 pointer-coarse:after:absolute pointer-coarse:after:inset-x-0 pointer-coarse:after:-inset-y-1 pointer-coarse:after:content-['']"
        >
          <X className="size-3" strokeWidth={2} />
        </button>
      ) : null}
    </span>
  );
}

function AddInline({
  placeholder,
  onAdd,
}: {
  placeholder: string;
  onAdd: (value: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState("");
  if (!editing)
    return (
      <button
        type="button"
        onClick={() => setEditing(true)}
        className="inline-flex h-[22px] items-center gap-1 rounded-xs px-1.5 text-small text-faint outline-none hover:bg-hover hover:text-muted-foreground focus-visible:bg-hover active:bg-hover pointer-coarse:h-11 pointer-coarse:w-11 pointer-coarse:justify-center pointer-coarse:px-0"
      >
        {/* Under a finger just the "+", so it stays on the chips' line. */}
        <Plus className="size-3 pointer-coarse:size-4" strokeWidth={2} />
        <span className="pointer-coarse:sr-only">Add</span>
      </button>
    );
  const commit = () => {
    const v = value.trim().replace(/^#/, "");
    if (v) onAdd(v);
    setValue("");
    setEditing(false);
  };
  return (
    <input
      autoFocus
      value={value}
      placeholder={placeholder}
      onChange={(e) => setValue(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") commit();
        if (e.key === "Escape") {
          setValue("");
          setEditing(false);
        }
      }}
      enterKeyHint="done"
      autoCapitalize="none"
      className="h-[22px] w-28 rounded-xs bg-surface px-1.5 text-small shadow-[0_0_0_1px_var(--ink),0_0_0_3px_var(--ink-soft)] outline-none pointer-coarse:h-9 pointer-coarse:w-36"
    />
  );
}

function ValueEditor({
  name,
  value,
  onSet,
}: {
  name: string;
  value: unknown;
  onSet: (v: unknown) => void;
}) {
  const type = propType(value);
  const [draft, setDraft] = useState<string | null>(null);
  switch (type) {
    case "bool":
      return (
        <button
          type="button"
          role="checkbox"
          aria-checked={value === true}
          aria-label={name}
          onClick={() => onSet(!value)}
          className={cn(
            "grid size-[15px] place-items-center rounded-[4px] border-[1.5px] outline-none transition-colors duration-150 ease-out focus-visible:outline-2 focus-visible:outline-ink pointer-coarse:size-6",
            value ? "border-ink bg-ink text-ink-foreground" : "border-faint bg-background",
          )}
        >
          {value ? (
            <svg
              viewBox="0 0 16 16"
              className="size-[11px]"
              fill="none"
              stroke="currentColor"
              strokeWidth={2.4}
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M3.5 8.5l3 3 6-7" />
            </svg>
          ) : null}
        </button>
      );
    case "list": {
      const items = (value as unknown[]).map(String);
      return (
        <div className="flex flex-wrap items-center gap-1.5 py-[3px]">
          {items.map((it, i) => (
            <Chip
              key={`${it}:${i}`}
              label={it}
              onRemove={() => onSet(items.filter((_, j) => j !== i))}
            >
              {it}
            </Chip>
          ))}
          <AddInline placeholder="Add item" onAdd={(v) => onSet([...items, v])} />
        </div>
      );
    }
    case "date": {
      const s = String(value);
      return (
        <input
          type="date"
          aria-label={name}
          value={s.slice(0, 10)}
          onChange={(e) =>
            e.target.value && onSet(s.length > 10 ? e.target.value + s.slice(10) : e.target.value)
          }
          className={cn(fieldClass, "w-auto [color-scheme:light_dark]")}
        />
      );
    }
    default: {
      const commit = () => {
        if (draft === null) return;
        const v = parseValue(type, draft);
        setDraft(null);
        if (v !== undefined && v !== value) onSet(v);
      };
      return (
        <input
          aria-label={name}
          inputMode={type === "number" ? "decimal" : undefined}
          value={draft ?? display(value)}
          placeholder="Empty"
          onChange={(e) => setDraft(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") e.currentTarget.blur();
            if (e.key === "Escape") {
              setDraft(null);
              e.currentTarget.blur();
            }
          }}
          className={cn(fieldClass, type === "number" && "tabular-nums")}
        />
      );
    }
  }
}

function AddProperty({
  existing,
  onAdd,
}: {
  existing: string[];
  onAdd: (key: string, type: PropType) => void;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [type, setType] = useState<PropType>("text");
  const narrow = useNarrow();
  const key = name.trim();
  const valid = validKey(key) && !existing.includes(key);
  const submit = () => {
    if (!valid) return;
    onAdd(key, type);
    setName("");
    setType("text");
    setOpen(false);
  };
  const trigger = (
    <button
      type="button"
      className="col-span-2 -mx-1.5 flex h-7 w-fit items-center gap-1.5 rounded-xs px-1.5 text-faint outline-none hover:bg-hover hover:text-muted-foreground focus-visible:bg-hover active:bg-hover pointer-coarse:h-11"
    >
      <Plus className="size-3.5" strokeWidth={1.75} /> Add property
    </button>
  );
  const form = (
    <form
      className={cn("flex flex-col gap-2.5", narrow && "gap-3 px-2 pb-2")}
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <input
        autoFocus
        aria-label="Property name"
        placeholder="Name, e.g. priority"
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        enterKeyHint="done"
        value={name}
        onChange={(e) => setName(e.target.value)}
        className="h-8 rounded-md bg-surface px-2.5 text-ui shadow-card outline-none focus:shadow-[0_0_0_1px_var(--ink),0_0_0_4px_var(--ink-soft)] pointer-coarse:h-11"
      />
      <div role="radiogroup" aria-label="Type" className="grid grid-cols-5 gap-0.5">
        {(Object.keys(TYPE_LABEL) as PropType[]).map((t) => {
          const Icon = TYPE_ICON[t];
          return (
            <button
              key={t}
              type="button"
              role="radio"
              aria-checked={type === t}
              title={TYPE_LABEL[t]}
              onClick={() => setType(t)}
              className={cn(
                "flex h-12 flex-col items-center justify-center gap-1 rounded-md text-[10.5px] text-muted-foreground outline-none hover:bg-hover focus-visible:bg-hover pointer-coarse:h-14 pointer-coarse:text-[12px]",
                type === t && "bg-ink-soft text-ink-strong hover:bg-ink-soft",
              )}
            >
              <Icon className="size-4" strokeWidth={1.75} />
              {TYPE_LABEL[t]}
            </button>
          );
        })}
      </div>
      <Button type="submit" size="sm" disabled={!valid} className={narrow ? "w-full" : "self-end"}>
        Add property
      </Button>
    </form>
  );
  // A phone: the form rises from the bottom, above the keyboard, instead
  // of a popover the keyboard would cover.
  if (narrow)
    return (
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetTrigger asChild>{trigger}</SheetTrigger>
        <SheetContent title="Add property">{form}</SheetContent>
      </Sheet>
    );
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{trigger}</PopoverTrigger>
      <PopoverContent className="w-80 p-3">{form}</PopoverContent>
    </Popover>
  );
}

export function PropertiesPanel({ note }: { note: Note }) {
  const patch = usePatch(note);
  const navigate = useUI((s) => s.navigate);
  const props = note.properties;
  const status = typeof props.status === "string" ? props.status : "";
  const custom = Object.keys(props).filter((k) => !SPECIAL_KEYS.has(k));
  const fmTags = Array.isArray(props.tags)
    ? props.tags.map(String)
    : typeof props.tags === "string"
      ? [props.tags]
      : [];
  // Tags written in the body (#tag) show too, but only frontmatter ones can be removed here.
  const tags = note.tags;

  return (
    <dl className="mb-7 grid grid-cols-[110px_1fr] gap-y-1 border-b border-divider pb-5 text-ui max-[520px]:grid-cols-[92px_1fr]">
      <Label icon={Hash}>Tags</Label>
      <dd className="flex min-h-7 flex-wrap items-center gap-1.5 py-[3px]">
        {tags.map((t) => (
          <Chip
            key={t}
            label={`#${t}`}
            onClick={() => navigate({ kind: "list", list: { type: "tag", tag: t } })}
            onRemove={fmTags.includes(t) ? () => void patch({ tags: { remove: [t] } }) : undefined}
          >
            #{t}
          </Chip>
        ))}
        <AddInline placeholder="tag" onAdd={(t) => void patch({ tags: { add: [t] } })} />
      </dd>

      <Label icon={Circle}>Status</Label>
      <dd className="flex h-7 items-center pointer-coarse:h-11">
        <DropdownMenu>
          <DropdownMenuTrigger className="-mx-1.5 inline-flex h-7 items-center gap-1.5 rounded-xs px-1.5 text-text-soft outline-none hover:bg-hover focus-visible:bg-hover data-[state=open]:bg-hover pointer-coarse:h-11">
            {status ? (
              <>
                <span className="size-2 rounded-full" style={{ background: statusColor(status) }} />
                {statusLabel(status)}
              </>
            ) : (
              <span className="text-faint">Empty</span>
            )}
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {STATUSES.map((s) => (
              <DropdownMenuItem key={s} onSelect={() => void patch({ set: { status: s } })}>
                <span className="size-2 rounded-full" style={{ background: statusColor(s) }} />
                {statusLabel(s)}
              </DropdownMenuItem>
            ))}
            {status ? (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => void patch({ unset: ["status"] })}>
                  <X /> Clear status
                </DropdownMenuItem>
              </>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      </dd>

      <Label icon={Calendar} still>
        Created
      </Label>
      <dd className="flex h-7 items-center text-text-soft pointer-coarse:h-9">
        {note.created ? formatDate(note.created) : "—"}
      </dd>
      {note.updated && note.updated !== note.created ? (
        <>
          <Label icon={CalendarClock} still>
            Updated
          </Label>
          <dd className="flex h-7 items-center text-text-soft pointer-coarse:h-9">
            {relativeTime(note.updated)}
          </dd>
        </>
      ) : null}

      {custom.map((k) => {
        const value = props[k];
        const Icon = TYPE_ICON[propType(value)];
        return (
          <div key={k} className="contents">
            <dt className="flex h-7 min-w-0 items-center pointer-coarse:h-11">
              <DropdownMenu>
                <DropdownMenuTrigger className="-mx-1.5 flex h-7 max-w-full min-w-0 items-center gap-1.5 rounded-xs px-1.5 text-faint outline-none hover:bg-hover hover:text-muted-foreground focus-visible:bg-hover data-[state=open]:bg-hover pointer-coarse:h-11">
                  <Icon className="size-3.5 shrink-0" strokeWidth={1.75} />
                  <span className="truncate">{k}</span>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start">
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() => void patch({ unset: [k] })}
                  >
                    <Trash2 /> Remove property
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </dt>
            <dd className="flex min-h-7 min-w-0 items-center pointer-coarse:min-h-11">
              <ValueEditor name={k} value={value} onSet={(v) => void patch({ set: { [k]: v } })} />
            </dd>
          </div>
        );
      })}

      <AddProperty
        existing={Object.keys(props)}
        onAdd={(k, t) => void patch({ set: { [k]: emptyValue(t) } })}
      />
    </dl>
  );
}
