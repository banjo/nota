import type { SyncStatus } from "@nota/client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { useClient } from "../context.ts";
import { keys } from "../queries.ts";
import { useUI } from "../state.ts";

const time = new Intl.DateTimeFormat("en-GB", { hour: "2-digit", minute: "2-digit" });

function describe(
  s: SyncStatus | undefined,
  web: boolean,
): { text: string; color: string; title?: string } {
  if (web) return { text: "Saved to the server", color: "var(--success)" };
  if (!s || s.state === "off") return { text: "On this device only", color: "var(--faint)" };
  if (s.state === "starting") return { text: "Syncing…", color: "var(--warning)" };
  if (s.state === "error") return { text: "Sync paused", color: "var(--danger)", title: s.error };
  const conflicts = s.conflicts.length
    ? ` · ${s.conflicts.length} conflict${s.conflicts.length > 1 ? "s" : ""}`
    : "";
  return {
    text: `Synced ${time.format(new Date(s.time))}${conflicts}`,
    color: "var(--success)",
    title: s.remote,
  };
}

/** The sync state at the foot of the sidebar; opens Settings. */
export function SyncStatusRow() {
  const client = useClient();
  const qc = useQueryClient();
  const navigate = useUI((s) => s.navigate);
  const status = useQuery({
    queryKey: keys.sync,
    queryFn: () => client.sync!.status(),
    enabled: !!client.sync,
  });
  useEffect(() => client.sync?.subscribe((s) => qc.setQueryData(keys.sync, s)), [client, qc]);
  const d = describe(status.data, !client.sync);
  return (
    <button
      type="button"
      title={d.title}
      onClick={() => navigate({ kind: "settings" })}
      className="flex h-7 min-w-0 flex-1 items-center gap-[7px] rounded-sm px-2 text-left text-small text-faint outline-none hover:bg-hover hover:text-muted-foreground focus-visible:bg-hover pointer-coarse:h-11"
    >
      <span className="size-[7px] shrink-0 rounded-full" style={{ background: d.color }} />
      <span className="truncate">{d.text}</span>
    </button>
  );
}
