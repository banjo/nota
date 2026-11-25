import type { ViewInfo } from "@nota/client";
import {
  Bookmark,
  CheckCircle2,
  Clock,
  Flag,
  Hash,
  Inbox,
  Layers,
  type LucideIcon,
  PenLine,
  Star,
} from "lucide-react";
import { FolderTree } from "../components/FolderTree.tsx";
import { FoldersHeaderActions } from "../components/FoldersHeader.tsx";
import { NoteRow } from "../components/NoteRow.tsx";
import { Row, SectionHeader } from "../components/Row.tsx";
import { useInbox, useList, useTags, useViews } from "../queries.ts";
import { type Route, useUI } from "../state.ts";

// The built-in sidebar sections, registered like any plugin's.

/** The nav row a route lights up: "inbox", "list:recent", "list:tag:<tag>", or "". */
export function navKey(route: Route): string {
  if (route.kind === "inbox") return "inbox";
  if (route.kind !== "list") return "";
  const l = route.list;
  return l.type === "tag" ? `list:tag:${l.tag}` : `list:${l.type}`;
}

/** Whether the nav key lights the list row of `type` (and `key`, for a tag). */
export const navActive = (current: string, type: string, key?: string) =>
  key === undefined
    ? current === `list:${type}` || current.startsWith(`list:${type}:`)
    : current === `list:${type}:${key}`;

/** Inbox, Recent and Tags. */
export function NavSection() {
  const inbox = useInbox();
  const tags = useTags();
  // Which of these rows is the page on screen, as a string: opening a
  // note does not re-render the section.
  const current = useUI((s) => navKey(s.route));
  const navigate = useUI((s) => s.navigate);
  const open = useUI((s) => !!s.expanded["section:tags"]);
  const toggle = useUI((s) => s.toggle);
  const listActive = (type: string, key?: string) => navActive(current, type, key);
  return (
    <div className="flex flex-col">
      <Row
        icon={<Inbox strokeWidth={1.75} />}
        label="Inbox"
        count={inbox.data?.length}
        active={current === "inbox"}
        onClick={() => navigate({ kind: "inbox" })}
      />
      <Row
        icon={<Clock strokeWidth={1.75} />}
        label="Recent"
        active={listActive("recent")}
        onClick={() => navigate({ kind: "list", list: { type: "recent" } })}
      />
      <Row
        icon={<Hash strokeWidth={1.75} />}
        label="Tags"
        chevron={open}
        onClick={() => toggle("section:tags")}
      />
      {open
        ? (tags.data ?? []).map((t) => (
            <Row
              key={t.tag}
              depth={1.4}
              icon={<Hash strokeWidth={1.75} className="text-faint" />}
              label={t.tag}
              count={t.count}
              muted
              active={listActive("tag", t.tag)}
              onClick={() => navigate({ kind: "list", list: { type: "tag", tag: t.tag } })}
            />
          ))
        : null}
      {open && tags.data?.length === 0 ? (
        <p className="py-1 pl-10 text-small text-faint">No tags yet</p>
      ) : null}
    </div>
  );
}

const VIEW_ICONS: Record<string, LucideIcon> = {
  pen: PenLine,
  star: Star,
  flag: Flag,
  clock: Clock,
  check: CheckCircle2,
  bookmark: Bookmark,
  hash: Hash,
};

function ViewRow({ view }: { view: ViewInfo }) {
  const key = `view:${view.id}`;
  const open = useUI((s) => !!s.expanded[key]);
  const toggle = useUI((s) => s.toggle);
  const active = useUI(
    (s) => s.route.kind === "list" && s.route.list.type === "view" && s.route.list.id === view.id,
  );
  const navigate = useUI((s) => s.navigate);
  const notes = useList({ view: view.id }, open);
  const Icon = VIEW_ICONS[view.icon ?? ""] ?? Layers;
  return (
    <>
      <Row
        chevron={open}
        icon={<Icon strokeWidth={1.75} />}
        label={view.name}
        count={view.count}
        active={active}
        onClick={() => {
          toggle(key);
          navigate({ kind: "list", list: { type: "view", id: view.id, name: view.name } });
        }}
      />
      {open ? (notes.data ?? []).map((n) => <NoteRow key={n.path} note={n} depth={2} />) : null}
    </>
  );
}

/** Saved views (.nota/views/*.yaml) as virtual folders. */
export function ViewsSection() {
  const views = useViews();
  const open = useUI((s) => s.expanded["section:views"] !== false);
  const toggle = useUI((s) => s.toggle);
  if (!views.data?.length) return null;
  return (
    <div className="flex flex-col">
      <SectionHeader open={open} onClick={() => toggle("section:views", !open)}>
        Views
      </SectionHeader>
      {open ? views.data.map((v) => <ViewRow key={v.id} view={v} />) : null}
    </div>
  );
}

/** The folder tree with notes nested. */
export function FoldersSection() {
  const open = useUI((s) => s.expanded["section:folders"] !== false);
  const toggle = useUI((s) => s.toggle);
  return (
    <div className="flex flex-col">
      <div className="relative">
        <SectionHeader open={open} onClick={() => toggle("section:folders", !open)}>
          Folders
        </SectionHeader>
        <FoldersHeaderActions />
      </div>
      {open ? <FolderTree /> : null}
    </div>
  );
}
